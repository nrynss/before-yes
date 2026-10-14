package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/keel/flag"
	flagstore "github.com/nrynss/keel/flag/sqlitestore"
	"github.com/nrynss/keel/gate"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/pipeline"
)

// testClock is a settable clock for the gate's buckets.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type gatedEnv struct {
	*testServerEnv
	flags *flagstore.Store
	live  flag.Bool
	clock *testClock
	h     http.Handler
	url   string
}

func newGatedEnv(t *testing.T, cfg GateConfig) *gatedEnv {
	t.Helper()
	env := newTestServerEnv(t, 4)
	fs, err := flagstore.Open(context.Background(), flagstore.Config{DB: env.db})
	if err != nil {
		t.Fatalf("flagstore.Open: %v", err)
	}
	clock := &testClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cfg.Flags = fs
	if cfg.Live.Name == "" {
		cfg.Live = LiveFlag(true)
	}
	cfg.Now = clock.Now
	h, err := env.server.GatedHandler(cfg)
	if err != nil {
		t.Fatalf("GatedHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return &gatedEnv{testServerEnv: env, flags: fs, live: cfg.Live, clock: clock, h: h, url: ts.URL}
}

func postCheck(t *testing.T, url, body string, hdr map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+"/api/checks", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/checks: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func errCode(t *testing.T, b []byte) (string, map[string]any) {
	t.Helper()
	var env struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Detail  map[string]any `json:"detail"`
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatalf("decode error envelope %s: %v", b, err)
	}
	return env.Error.Code, env.Error.Detail
}

// Past the per-client bucket, POST /api/checks answers 429 rate_limited
// with a Retry-After header, and other routes stay ungated.
func TestGateRateLimitedWithRetryAfter(t *testing.T) {
	e := newGatedEnv(t, GateConfig{Rule: gate.Rule{
		Name:      "checks",
		PerClient: gate.Limit{Burst: 2, Every: time.Minute},
		Global:    gate.Limit{Burst: 100, Every: time.Second},
	}})
	// Invalid bodies still draw a token: the gate runs before validation.
	for i := range 2 {
		resp, b := postCheck(t, e.url, `{"title":"","age":9}`, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("request %d: status %d body %s, want 400", i, resp.StatusCode, b)
		}
	}
	resp, b := postCheck(t, e.url, `{"title":"Roblox","age":9}`, nil)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status %d body %s, want 429", resp.StatusCode, b)
	}
	if code, detail := errCode(t, b); code != "rate_limited" || detail["retry_after_seconds"] == nil {
		t.Errorf("code %q detail %v, want rate_limited with retry_after_seconds", code, detail)
	}
	if ra := resp.Header.Get("Retry-After"); ra == "" {
		t.Error("429 without Retry-After")
	}
	// Ungated routes still answer.
	for range 5 {
		r, err := http.Get(e.url + "/api/cards")
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/cards status %d, want 200 (ungated)", r.StatusCode)
		}
	}
	// The bucket refills on the gate's clock.
	e.clock.Add(time.Minute)
	if resp, b := postCheck(t, e.url, `{"title":"","age":9}`, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("after refill: status %d body %s, want 400 (past the gate)", resp.StatusCode, b)
	}
}

// With a passcode set, POST /api/checks without it is 403
// passcode_required; with it the check starts. Other routes need none.
func TestGatePasscodeRequired(t *testing.T) {
	e := newGatedEnv(t, GateConfig{Passcode: "open-sesame"})
	resp, b := postCheck(t, e.url, `{"title":"Roblox","age":9}`, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("no passcode: status %d body %s, want 403", resp.StatusCode, b)
	}
	if code, _ := errCode(t, b); code != "passcode_required" {
		t.Errorf("code %q, want passcode_required", code)
	}
	resp, b = postCheck(t, e.url, `{"title":"Roblox","age":9}`, map[string]string{"X-Passcode": "wrong"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong passcode: status %d body %s, want 403", resp.StatusCode, b)
	}
	resp, b = postCheck(t, e.url, `{"title":"Roblox","age":9}`, map[string]string{"X-Passcode": "open-sesame"})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("with passcode: status %d body %s, want 202", resp.StatusCode, b)
	}
	r, err := http.Get(e.url + "/api/cards")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Errorf("GET /api/cards status %d, want 200 without passcode", r.StatusCode)
	}
}

// With the live switch off, an uncached title gets 503 live_checks_off
// and starts nothing, while a title with a fresh card still gets 200
// {card_id}. Flipping the flag back on starts checks again, no restart.
func TestLiveSwitchOffRefusesUncachedServesCached(t *testing.T) {
	e := newGatedEnv(t, GateConfig{Rule: gate.Rule{
		Name:      "checks",
		PerClient: gate.Limit{Burst: 100, Every: time.Second},
		Global:    gate.Limit{Burst: 100, Every: time.Second},
	}})
	cid, err := e.cardStore.Put(context.Background(), pipeline.Card{
		Title:            "Minecraft",
		Age:              9,
		ChecklistVersion: checklist.Version,
		CheckedAt:        time.Now().UTC(),
		Verdict:          checklist.YesWithSettings,
	})
	if err != nil {
		t.Fatalf("put card: %v", err)
	}
	if _, err := e.flags.SetBool(context.Background(), e.live, false); err != nil {
		t.Fatalf("SetBool: %v", err)
	}

	resp, b := postCheck(t, e.url, `{"title":"Fortnite","age":11}`, nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("uncached: status %d body %s, want 503", resp.StatusCode, b)
	}
	if code, _ := errCode(t, b); code != CodeLiveChecksOff {
		t.Errorf("code %q, want %s", code, CodeLiveChecksOff)
	}
	if !strings.Contains(string(b), liveOffMessage) {
		t.Errorf("body %s, want the contract message", b)
	}
	e.searcher.mu.Lock()
	calls := e.searcher.searchCalls
	e.searcher.mu.Unlock()
	if calls != 0 {
		t.Errorf("search calls %d with live off, want 0", calls)
	}

	resp, b = postCheck(t, e.url, `{"title":"minecraft ","age":9}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cached: status %d body %s, want 200", resp.StatusCode, b)
	}
	var got struct {
		CardID string `json:"card_id"`
	}
	_ = json.Unmarshal(b, &got)
	if strings.TrimPrefix(got.CardID, "c_") != strings.TrimPrefix(cid, "c_") {
		t.Errorf("card_id %q, want %q", got.CardID, cid)
	}

	// Validation still answers first.
	if resp, b := postCheck(t, e.url, `{"title":"Fortnite","age":99}`, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid with live off: status %d body %s, want 400", resp.StatusCode, b)
	}

	if _, err := e.flags.SetBool(context.Background(), e.live, true); err != nil {
		t.Fatalf("SetBool: %v", err)
	}
	if resp, b := postCheck(t, e.url, `{"title":"Fortnite","age":11}`, nil); resp.StatusCode != http.StatusAccepted {
		t.Errorf("live back on: status %d body %s, want 202", resp.StatusCode, b)
	}
}

// The config's live value is the flag's default: live = false refuses
// until the flag is set.
func TestLiveSwitchDefaultFromConfig(t *testing.T) {
	e := newGatedEnv(t, GateConfig{Live: LiveFlag(false)})
	resp, b := postCheck(t, e.url, `{"title":"Fortnite","age":11}`, nil)
	if code, _ := errCode(t, b); resp.StatusCode != http.StatusServiceUnavailable || code != CodeLiveChecksOff {
		t.Errorf("status %d code %q, want 503 live_checks_off", resp.StatusCode, code)
	}
}

// Forwarded headers count only from a trusted proxy: from the Docker
// bridge each CF-Connecting-IP gets its own bucket; from a public peer the
// headers are ignored and every request shares the peer's bucket.
func TestGateTrustedProxies(t *testing.T) {
	e := newGatedEnv(t, GateConfig{Rule: gate.Rule{
		Name:      "checks",
		PerClient: gate.Limit{Burst: 1, Every: time.Hour},
		Global:    gate.Limit{Burst: 100, Every: time.Second},
	}})
	send := func(peer, client string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/checks", strings.NewReader(`{"title":"","age":9}`))
		req.RemoteAddr = peer
		req.Header.Set("CF-Connecting-IP", client)
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		return rec.Code
	}
	// Traefik on the Docker bridge, two different clients.
	if c := send("172.18.0.2:5000", "198.51.100.1"); c != http.StatusBadRequest {
		t.Fatalf("trusted client 1: %d, want 400", c)
	}
	if c := send("172.18.0.2:5000", "198.51.100.2"); c != http.StatusBadRequest {
		t.Fatalf("trusted client 2: %d, want 400 (own bucket)", c)
	}
	// An untrusted peer cannot mint buckets with the header.
	if c := send("203.0.113.9:5000", "198.51.100.3"); c != http.StatusBadRequest {
		t.Fatalf("untrusted first: %d, want 400", c)
	}
	if c := send("203.0.113.9:5000", "198.51.100.4"); c != http.StatusTooManyRequests {
		t.Fatalf("untrusted second: %d, want 429 (header ignored)", c)
	}
	// A private peer outside the trusted set is not believed either.
	if c := send("10.1.2.3:5000", "198.51.100.5"); c != http.StatusBadRequest {
		t.Fatalf("10/8 first: %d, want 400", c)
	}
	if c := send("10.1.2.3:5000", "198.51.100.6"); c != http.StatusTooManyRequests {
		t.Fatalf("10/8 second: %d, want 429 (not a trusted proxy)", c)
	}
	for _, p := range TrustedProxies {
		if p == netip.MustParsePrefix("10.0.0.0/8") {
			t.Error("10.0.0.0/8 must not be trusted")
		}
	}
}

// GatedHandler refuses to build without a flag store.
func TestGatedHandlerNeedsFlags(t *testing.T) {
	env := newTestServerEnv(t, 4)
	if _, err := env.server.GatedHandler(GateConfig{}); err == nil {
		t.Fatal("GatedHandler with no flag store: want an error")
	}
}
