package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/job"
	"github.com/nrynss/keel/stream"

	"github.com/nrynss/before-yes/internal/appstore"
	"github.com/nrynss/before-yes/internal/cards"
	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/model"
	"github.com/nrynss/before-yes/internal/pipeline"
	"github.com/nrynss/before-yes/internal/spend"
	"github.com/nrynss/before-yes/internal/tavily"
)

// Test-only key strings. They are not real keys; the tests look for them
// byte for byte wherever a key must never be.
const (
	ourTFKey     = "tf-ours-0000000000000000"
	ourTavilyKey = "tvly-ours-000000000000000"
	visitorTF    = "tf-visitor/SECRET+7f3a9c1e5b"
	visitorTV    = "tvly-visitor/SECRET+2d8e4b6a0c"
)

// syncBuffer is a log sink safe for the runner's goroutines.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// upstream is a fake Token Factory or Tavily host. It records the bearer
// keys it was sent. With echo set it refuses every call with a 401 whose
// body repeats the key, as a careless host might.
type upstream struct {
	mu    sync.Mutex
	auths map[string]int
	echo  bool
	// mode shapes the echo: "" raw, "truncated" puts the key across the
	// clients' 300-byte cut, "json" escapes "/" as "\/", "url" query-encodes.
	mode string
}

func (u *upstream) echoBody(key string) string {
	switch u.mode {
	case "truncated":
		return `{"error":"` + strings.Repeat("x", 280) + key + `"}`
	case "json":
		return `{"error":"invalid api key ` + strings.ReplaceAll(key, "/", `\/`) + `"}`
	case "url":
		return `{"error":"see https://host.example/keys?k=` + url.QueryEscape(key) + `"}`
	}
	return `{"error":"invalid api key ` + key + `"}`
}

func (u *upstream) saw(key string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.auths[key]
}

func (u *upstream) note(r *http.Request) (string, bool) {
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.auths == nil {
		u.auths = map[string]int{}
	}
	u.auths[key]++
	return key, u.echo
}

func (u *upstream) tokenFactory(w http.ResponseWriter, r *http.Request) {
	key, echo := u.note(r)
	if echo {
		http.Error(w, u.echoBody(key), http.StatusUnauthorized)
		return
	}
	var body struct {
		Model string `json:"model"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	text := "[]"
	if body.Model == "judge-model" {
		text = okGradesJSON()
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"content": text}}},
		"usage":   map[string]any{"prompt_tokens": 100, "completion_tokens": 10},
	})
}

func (u *upstream) tavily(w http.ResponseWriter, r *http.Request) {
	key, echo := u.note(r)
	if echo {
		http.Error(w, u.echoBody(key), http.StatusUnauthorized)
		return
	}
	one := 1.0
	switch r.URL.Path {
	case "/search":
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []any{map[string]any{
				"url": "https://example.com/privacy", "title": "Privacy Policy",
				"content": "We collect basic info. Chat is filtered for safety.",
			}},
			"usage": map[string]any{"credits": one},
		})
	case "/extract":
		var body struct {
			URLs []string `json:"urls"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		var pages []any
		for _, u := range body.URLs {
			pages = append(pages, map[string]any{"url": u, "raw_content": "Full policy document: chat is moderated."})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": pages, "failed_results": []any{}, "usage": map[string]any{"credits": one}})
	default:
		http.NotFound(w, r)
	}
}

type byokEnv struct {
	dbPath  string
	meter   *spend.Meter
	runner  *job.Runner
	tf, tv  *upstream
	logs    *syncBuffer
	srv     *Server
	url     string
	searchr *configurableSearcher
}

// newBYOKEnv builds a server whose pipeline uses the real model and Tavily
// clients against fake hosts, a durable spend.Meter with a daily cap of
// limit fresh checks on the same database file as the cards and the jobs,
// and Server.Checks and Pipeline.Checks set to that one meter.
func newBYOKEnv(t *testing.T, limit int) *byokEnv {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "before-yes.db")
	db := openTestDB(t, dbPath)
	cardStore, err := cards.Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	evStore := openEvidenceStore(t, db, "evidence")
	logs := &syncBuffer{}
	log := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	broker := stream.New(stream.Config{})
	runner, err := NewRunner(db, broker, 4, nil, log)
	if err != nil {
		t.Fatal(err)
	}
	evCache, err := NewEvidenceCache(EvidenceCacheConfig{Store: evStore})
	if err != nil {
		t.Fatal(err)
	}
	meter, err := spend.Open(context.Background(), spend.Config{
		Path:  dbPath,
		Pools: map[spend.Pool]spend.PoolConfig{spend.PoolChecksDaily: {Limit: cost.Price(limit)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meter.Close() })

	tf, tv := &upstream{}, &upstream{}
	tfSrv := httptest.NewServer(http.HandlerFunc(tf.tokenFactory))
	t.Cleanup(tfSrv.Close)
	tvSrv := httptest.NewServer(http.HandlerFunc(tv.tavily))
	t.Cleanup(tvSrv.Close)

	ours := func(k string) func() (string, error) { return func() (string, error) { return k, nil } }
	reader, err := model.New(model.Settings{Provider: "test", BaseURL: tfSrv.URL, Model: "reader-model", InputPerM: 0.06, OutputPerM: 0.24}, ours(ourTFKey))
	if err != nil {
		t.Fatal(err)
	}
	judge, err := model.New(model.Settings{Provider: "test", BaseURL: tfSrv.URL, Model: "judge-model", InputPerM: 0.1, OutputPerM: 0.4}, ours(ourTFKey))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []*model.Client{reader, judge} {
		c.Retry.Attempts, c.Retry.TransientAttempts, c.Retry.Backoff = 1, 1, time.Millisecond
	}
	search := tavily.New(tvSrv.URL, ours(ourTavilyKey))
	search.Retry.PerMinute, search.Retry.Attempts, search.Retry.TransientAttempts, search.Retry.Backoff = 0, 1, 1, time.Millisecond

	p := &pipeline.Pipeline{
		Gatherer: &pipeline.Gatherer{Store: &configurableStore{}, Search: search, Meter: fakeMeter{}},
		Reader:   &pipeline.Reader{Model: reader, Meter: fakeMeter{}},
		Judge:    &pipeline.Judge{Model: judge, Meter: fakeMeter{}},
		Cache:    evCache,
		Checks:   meter,
	}
	srv := &Server{
		Pipeline: p, Runner: runner, Broker: broker, Cards: cardStore,
		EvidenceCache: evCache, Log: log, Checks: meter,
	}
	h, err := srv.Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return &byokEnv{dbPath: dbPath, meter: meter, runner: runner, tf: tf, tv: tv, logs: logs, srv: srv, url: ts.URL}
}

func visitorHeaders() map[string]string {
	return map[string]string{HeaderTokenFactoryKey: visitorTF, HeaderTavilyKey: visitorTV}
}

// waitCheck polls GET /api/checks/{id} until a terminal status and returns
// the raw body.
func waitCheck(t *testing.T, url, checkID string) (string, []byte) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get(url + "/api/checks/" + checkID)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		var st struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(b, &st)
		if st.Status != "running" && st.Status != "queued" {
			return st.Status, b
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("check did not finish")
	return "", nil
}

func startID(t *testing.T, b []byte) string {
	t.Helper()
	var s struct {
		CheckID string `json:"check_id"`
	}
	if err := json.Unmarshal(b, &s); err != nil || s.CheckID == "" {
		t.Fatalf("start body %s: %v", b, err)
	}
	return s.CheckID
}

func checksSpentToday(t *testing.T, m *spend.Meter) cost.Price {
	t.Helper()
	st, err := m.Status(context.Background(), spend.PoolChecksDaily)
	if err != nil {
		t.Fatal(err)
	}
	return st.Spent
}

// scanForKeys fails if a visitor key appears in the database file or its
// WAL and shared-memory files, in the job store's record, or in the logs.
func scanForKeys(t *testing.T, e *byokEnv, checkID string, extra ...[]byte) {
	t.Helper()
	keys := keyWindows()
	var scanned int
	for _, suffix := range []string{"", "-wal", "-shm"} {
		b, err := os.ReadFile(e.dbPath + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		scanned += len(b)
		for _, k := range keys {
			if bytes.Contains(b, []byte(k)) {
				t.Errorf("visitor key found in %s", filepath.Base(e.dbPath+suffix))
			}
		}
	}
	if scanned == 0 {
		t.Fatal("scanned no database bytes")
	}
	res, err := e.runner.Result(context.Background(), strings.TrimPrefix(checkID, "j_"))
	if err != nil {
		t.Fatal(err)
	}
	rec := string(res.Data) + string(res.Detail) + res.Code
	if res.Err != nil {
		rec += res.Err.Error()
	}
	logs := e.logs.String()
	for _, k := range keys {
		if strings.Contains(rec, k) {
			t.Errorf("visitor key found in the job record")
		}
		if strings.Contains(logs, k) {
			t.Errorf("visitor key found in the logs")
		}
		for _, x := range extra {
			if bytes.Contains(x, []byte(k)) {
				t.Errorf("visitor key found in a response")
			}
		}
	}
}

// A check on visitor keys runs on clients that send the visitor's keys,
// draws nothing on the daily cap, stores its card like any other, and
// leaves no key in the database file, the job store or the logs.
func TestVisitorKeysLeaveNoTrace(t *testing.T) {
	e := newBYOKEnv(t, 5)
	resp, b := byokPost(t, e.url, `{"title":"Roblox","age":9}`, visitorHeaders())
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d body %s, want 202", resp.StatusCode, b)
	}
	id := startID(t, b)
	status, body := waitCheck(t, e.url, id)
	if status != "done" {
		t.Fatalf("status %s body %s, want done", status, body)
	}
	if e.tf.saw(visitorTF) == 0 || e.tv.saw(visitorTV) == 0 {
		t.Errorf("hosts did not see the visitor keys: tf %d tavily %d", e.tf.saw(visitorTF), e.tv.saw(visitorTV))
	}
	if e.tf.saw(ourTFKey) != 0 || e.tv.saw(ourTavilyKey) != 0 {
		t.Errorf("a visitor check used our keys: tf %d tavily %d", e.tf.saw(ourTFKey), e.tv.saw(ourTavilyKey))
	}
	if got := checksSpentToday(t, e.meter); got != 0 {
		t.Errorf("checks booked %d, want 0 (a visitor check does not count)", got)
	}
	var done struct {
		CardID string `json:"card_id"`
	}
	_ = json.Unmarshal(body, &done)
	cr, err := http.Get(e.url + "/api/cards/" + done.CardID)
	if err != nil {
		t.Fatal(err)
	}
	card, _ := io.ReadAll(cr.Body)
	cr.Body.Close()
	if cr.StatusCode != http.StatusOK {
		t.Fatalf("card status %d", cr.StatusCode)
	}
	// The card is stored like any other: a repeat on our keys is a cache hit.
	resp, b2 := byokPost(t, e.url, `{"title":"Roblox","age":9}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("repeat status %d body %s, want 200 from the card cache", resp.StatusCode, b2)
	}
	scanForKeys(t, e, id, b, body, card, b2)
}

// The daily cap refuses a fresh check with 402 budget_exhausted
// (pool daily_checks) once spent, before any paid call. A cache hit still
// answers, a visitor-key check still runs, and a failed check is released
// rather than counted.
func TestDailyCapRefusesOnceSpent(t *testing.T) {
	e := newBYOKEnv(t, 1)

	resp, b := byokPost(t, e.url, `{"title":"Roblox","age":9}`, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("first: status %d body %s", resp.StatusCode, b)
	}
	if st, body := waitCheck(t, e.url, startID(t, b)); st != "done" {
		t.Fatalf("first: %s %s", st, body)
	}
	if got := checksSpentToday(t, e.meter); got != 1 {
		t.Fatalf("checks booked %d, want 1 (the main check only)", got)
	}
	calls := e.tv.saw(ourTavilyKey)

	resp, b = byokPost(t, e.url, `{"title":"Fortnite","age":11}`, nil)
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("over cap: status %d body %s, want 402", resp.StatusCode, b)
	}
	code, detail := byokErrCode(t, b)
	if code != CodeBudgetExhausted || detail["pool"] != "daily_checks" {
		t.Errorf("code %q detail %v, want budget_exhausted pool daily_checks", code, detail)
	}
	if !strings.Contains(string(b), "today's budget for fresh checks is spent") {
		t.Errorf("body %s, want the contract message", b)
	}
	if e.tv.saw(ourTavilyKey) != calls {
		t.Error("a refused check made a paid call")
	}

	if resp, b := byokPost(t, e.url, `{"title":"Roblox","age":9}`, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("cached over cap: status %d body %s, want 200", resp.StatusCode, b)
	}
	resp, b = byokPost(t, e.url, `{"title":"Fortnite","age":11}`, visitorHeaders())
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("visitor over cap: status %d body %s, want 202", resp.StatusCode, b)
	}
	if st, body := waitCheck(t, e.url, startID(t, b)); st != "done" {
		t.Fatalf("visitor over cap: %s %s", st, body)
	}
	if got := checksSpentToday(t, e.meter); got != 1 {
		t.Errorf("checks booked %d after a visitor check, want 1", got)
	}
}

// A check that fails releases its hold: it does not count against the day.
func TestFailedCheckReleasesHold(t *testing.T) {
	e := newBYOKEnv(t, 1)
	e.tv.echo = true // every Tavily call fails
	resp, b := byokPost(t, e.url, `{"title":"Roblox","age":9}`, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d body %s", resp.StatusCode, b)
	}
	if st, body := waitCheck(t, e.url, startID(t, b)); st != "error" {
		t.Fatalf("status %s %s, want error", st, body)
	}
	st, err := e.meter.Status(context.Background(), spend.PoolChecksDaily)
	if err != nil {
		t.Fatal(err)
	}
	if st.Spent != 0 || st.Remaining != 1 {
		t.Errorf("after a failed check: spent %d remaining %d, want 0 and 1", st.Spent, st.Remaining)
	}
}

// Alternatives reserve through Pipeline.Checks (T3.5) and the main check
// through Server.Checks, so the two must be one *spend.Meter. A server
// with Pipeline.Checks unset (alternatives uncapped) or set to another
// meter (a second ledger) refuses to build.
func TestCapWiringSharesOneMeter(t *testing.T) {
	e := newBYOKEnv(t, 5)
	other, err := spend.Open(context.Background(), spend.Config{
		Path:  filepath.Join(t.TempDir(), "other.db"),
		Pools: map[spend.Pool]spend.PoolConfig{spend.PoolChecksDaily: {Limit: 5}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })

	cases := []struct {
		name     string
		server   Checks
		pipeline pipeline.Checks
		ok       bool
	}{
		{"same meter", e.meter, e.meter, true},
		{"neither", nil, nil, true},
		{"pipeline unset", e.meter, nil, false},
		{"server unset", nil, e.meter, false},
		{"different meters", e.meter, other, false},
	}
	for _, c := range cases {
		p := *e.srv.Pipeline
		p.Checks = c.pipeline
		s := *e.srv
		s.Pipeline, s.Checks = &p, c.server
		_, err := s.Handler()
		if c.ok && err != nil {
			t.Errorf("%s: Handler error %v, want none", c.name, err)
		}
		if !c.ok && !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: Handler error %v, want ErrInvalidConfig", c.name, err)
		}
	}
}

// Half a key pair, or a key with a space, is refused with a fixed message
// that does not quote the key.
func TestVisitorKeysBothRequired(t *testing.T) {
	e := newBYOKEnv(t, 5)
	for _, h := range []map[string]string{
		{HeaderTokenFactoryKey: visitorTF},
		{HeaderTavilyKey: visitorTV},
		{HeaderTokenFactoryKey: visitorTF + " x", HeaderTavilyKey: visitorTV},
	} {
		resp, b := byokPost(t, e.url, `{"title":"Fortnite","age":11}`, h)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("headers %d: status %d, want 400", len(h), resp.StatusCode)
		}
		if bytes.Contains(b, []byte(visitorTF)) || bytes.Contains(b, []byte(visitorTV)) {
			t.Error("refusal quotes a key")
		}
	}
	if got := checksSpentToday(t, e.meter); got != 0 {
		t.Errorf("checks booked %d, want 0", got)
	}
}

// Printing a VisitorKeys never writes a key.
func TestVisitorKeysRedactWhenPrinted(t *testing.T) {
	k := NewVisitorKeys(visitorTF, visitorTV)
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("keys", "k", k)
	j, _ := json.Marshal(k)
	for _, s := range []string{buf.String(), string(j), k.String(), k.GoString()} {
		if strings.Contains(s, visitorTF) || strings.Contains(s, visitorTV) {
			t.Errorf("printed a key: %s", s)
		}
	}
}

func byokPost(t *testing.T, url, body string, hdr map[string]string) (*http.Response, []byte) {
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

func byokErrCode(t *testing.T, b []byte) (string, map[string]any) {
	t.Helper()
	var env struct {
		Error struct {
			Code   string         `json:"code"`
			Detail map[string]any `json:"detail"`
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatalf("decode error envelope %s: %v", b, err)
	}
	return env.Error.Code, env.Error.Detail
}

// keyWindows is every run of minSecretRun characters of each visitor key,
// raw, JSON-escaped and URL-encoded: a scan for these finds a whole,
// truncated or encoded key.
func keyWindows() []string {
	seen := map[string]bool{}
	var out []string
	for _, k := range []string{visitorTF, visitorTV} {
		for _, f := range []string{k, strings.ReplaceAll(k, "/", `\/`), url.QueryEscape(k), url.PathEscape(k)} {
			for i := 0; i+minSecretRun <= len(f); i++ {
				w := f[i : i+minSecretRun]
				if !seen[w] {
					seen[w] = true
					out = append(out, w)
				}
			}
		}
	}
	return out
}

// A host that echoes the key cut at the clients' 300-byte limit, JSON
// escaped or URL-encoded leaves no run of it in the database, the WAL, the
// job record, the check's error or the logs; the stored failure carries
// only the status and a fixed sentence.
func TestVisitorKeyEchoFormsLeaveNoTrace(t *testing.T) {
	for _, mode := range []string{"", "truncated", "json", "url"} {
		t.Run("echo_"+mode, func(t *testing.T) {
			e := newBYOKEnv(t, 5)
			e.tf.echo, e.tv.echo = true, true
			e.tf.mode, e.tv.mode = mode, mode
			resp, b := byokPost(t, e.url, `{"title":"Fortnite","age":11}`, visitorHeaders())
			if resp.StatusCode != http.StatusAccepted {
				t.Fatalf("status %d body %s, want 202", resp.StatusCode, b)
			}
			id := startID(t, b)
			status, body := waitCheck(t, e.url, id)
			if status != "error" {
				t.Fatalf("status %s body %s, want error", status, body)
			}
			if e.tv.saw(visitorTV) == 0 {
				t.Fatal("the host never saw the visitor key")
			}
			res, err := e.runner.Result(context.Background(), strings.TrimPrefix(id, "j_"))
			if err != nil || res.Err == nil {
				t.Fatalf("result %v %v", res, err)
			}
			if got := res.Err.Error(); got != "Tavily answered HTTP 401 on the visitor's key" {
				t.Errorf("stored error %q, want the status and a fixed sentence only", got)
			}
			if res.Code != CodeCheckFailed {
				t.Errorf("code %q, want check_failed", res.Code)
			}
			scanForKeys(t, e, id, body)
		})
	}
}

// redact removes the whole key, a run of 8 or more of its characters (a
// cut echo) and its escaped and encoded forms, and leaves other text.
func TestRedactPartialAndEncodedForms(t *testing.T) {
	keys := []string{visitorTV}
	cases := map[string]string{
		"whole":     "key " + visitorTV + " end",
		"cut":       "key " + visitorTV[:10],
		"tail":      visitorTV[len(visitorTV)-9:] + " end",
		"json":      "key " + strings.ReplaceAll(visitorTV, "/", `\/`),
		"unicode":   `key \u0074\u0076\u006c\u0079\u002d\u0076\u0069\u0073\u0069`,
		"url query": "k=" + url.QueryEscape(visitorTV),
		"url path":  "/" + url.PathEscape(visitorTV),
	}
	for name, in := range cases {
		out := redact(in, keys)
		if !strings.Contains(out, "[redacted]") {
			t.Errorf("%s: %q not redacted", name, out)
		}
		for _, w := range keyWindows() {
			if strings.Contains(out, w) && strings.Contains(visitorTV+url.QueryEscape(visitorTV), w) {
				t.Errorf("%s: %q keeps %q", name, out, w)
			}
		}
	}
	if got := redact("Tavily answered HTTP 401", keys); got != "Tavily answered HTTP 401" {
		t.Errorf("plain text changed: %q", got)
	}
}

// panicStore panics inside a check with a value that holds the key.
type panicStore struct{ configurableStore }

func (*panicStore) Find(context.Context, string, int) ([]appstore.App, error) {
	panic("store blew up holding " + visitorTV)
}

// A visitor job that panics is recovered into a generic check_failed; the
// panic value reaches neither the job record nor the logs nor the DB.
func TestVisitorPanicIsScrubbed(t *testing.T) {
	e := newBYOKEnv(t, 5)
	e.srv.Pipeline.Gatherer.Store = &panicStore{}
	resp, b := byokPost(t, e.url, `{"title":"Fortnite","age":11}`, visitorHeaders())
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d body %s, want 202", resp.StatusCode, b)
	}
	id := startID(t, b)
	status, body := waitCheck(t, e.url, id)
	if status != "error" {
		t.Fatalf("status %s body %s, want error", status, body)
	}
	res, err := e.runner.Result(context.Background(), strings.TrimPrefix(id, "j_"))
	if err != nil || res.Err == nil || res.Err.Error() != visitorPanicMessage || res.Code != CodeCheckFailed {
		t.Errorf("result code %q err %v, want check_failed %q", res.Code, res.Err, visitorPanicMessage)
	}
	if !strings.Contains(e.logs.String(), "visitor check panicked") {
		t.Error("the panic was not logged (scrubbed)")
	}
	scanForKeys(t, e, id, body)
}

// Half a key pair is refused with 400 before the card cache, so even a
// title with a fresh card gets 400 (docs/api.md: validation, then cache).
func TestHalfPairRefusedBeforeCardCache(t *testing.T) {
	e := newBYOKEnv(t, 5)
	if _, err := e.srv.Cards.Put(context.Background(), pipeline.Card{
		Title: "Minecraft", Age: 9, ChecklistVersion: checklist.Version,
		CheckedAt: time.Now().UTC(), Verdict: checklist.YesWithSettings,
	}); err != nil {
		t.Fatal(err)
	}
	if resp, b := byokPost(t, e.url, `{"title":"Minecraft","age":9}`, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("cached without keys: status %d body %s, want 200", resp.StatusCode, b)
	}
	for _, h := range []map[string]string{{HeaderTokenFactoryKey: visitorTF}, {HeaderTavilyKey: visitorTV}} {
		resp, b := byokPost(t, e.url, `{"title":"Minecraft","age":9}`, h)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("cached title, half pair: status %d body %s, want 400", resp.StatusCode, b)
		}
	}
}
