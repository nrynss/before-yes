package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/pipeline"
	"github.com/nrynss/before-yes/internal/usage"
)

// fakeUsage records counts in memory. boom makes Count panic, to show that a
// broken counter never fails a request.
type fakeUsage struct {
	mu   sync.Mutex
	n    map[string]int
	boom bool
}

func (f *fakeUsage) Count(event string) {
	if f.boom {
		panic("the counter is broken")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.n == nil {
		f.n = map[string]int{}
	}
	f.n[event]++
}

func (f *fakeUsage) get(event string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n[event]
}

// snapshot is every count so far, for asserting nothing else was counted.
func (f *fakeUsage) snapshot() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]int{}
	for k, v := range f.n {
		out[k] = v
	}
	return out
}

// noCookie fails the test if a response sets a cookie.
func noCookie(t *testing.T, resp *http.Response) {
	t.Helper()
	if v := resp.Header.Values("Set-Cookie"); len(v) != 0 {
		t.Errorf("%s %s set a cookie: %v", resp.Request.Method, resp.Request.URL.Path, v)
	}
}

// The app's own pages: the index page counts home, a compare page counts
// compare_view, and nothing else (assets, client routes, HEAD, other methods)
// is counted.
func TestUsageCountsHomeAndCompareOnly(t *testing.T) {
	fu := &fakeUsage{}
	web := fstest.MapFS{
		"index.html":  {Data: []byte("<!doctype html><title>Before Yes</title>")},
		"_app/app.js": {Data: []byte("console.log('app')")},
	}
	ts := httptest.NewServer(App(http.NotFoundHandler(), &Status{Usage: fu}, web))
	t.Cleanup(ts.Close)

	for _, c := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/", http.StatusOK},
		{http.MethodHead, "/", http.StatusOK},
		{http.MethodGet, "/compare/c_aaaa/c_bbbb", http.StatusOK},
		{http.MethodGet, "/_app/app.js", http.StatusOK},
		{http.MethodGet, "/card/c_aaaa", http.StatusOK},
		{http.MethodPost, "/", http.StatusMethodNotAllowed},
	} {
		req, _ := http.NewRequest(c.method, ts.URL+c.path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != c.status {
			t.Errorf("%s %s: status %d, want %d", c.method, c.path, resp.StatusCode, c.status)
		}
		noCookie(t, resp)
	}
	want := map[string]int{usage.EventHome: 1, usage.EventCompareView: 1}
	if got := fu.snapshot(); len(got) != len(want) || got[usage.EventHome] != 1 || got[usage.EventCompareView] != 1 {
		t.Fatalf("counts %v, want %v", got, want)
	}
}

// A stored card fetched by id counts card_view. A check on a fresh card is
// answered from the cache and counts cache_hit, not check_started. A fresh
// check counts check_started and check_finished.
func TestUsageCardViewCacheHitAndFreshCheck(t *testing.T) {
	env := newTestServerEnv(t, 4)
	fu := &fakeUsage{}
	env.server.Usage = fu

	resp, err := http.Post(env.srv.URL+"/api/checks", "application/json", strings.NewReader(`{"title":"Roblox","age":9}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("fresh check: status %d body %s, want 202", resp.StatusCode, b)
	}
	noCookie(t, resp)
	out := waitCheckDone(t, env, checkIDOf(t, b))
	if out["status"] != "done" {
		t.Fatalf("fresh check ended %v", out)
	}
	if got := fu.snapshot(); got[usage.EventCheckStarted] != 1 || got[usage.EventCheckFinished] != 1 || got[usage.EventCheckFailed] != 0 || got[usage.EventCacheHit] != 0 {
		t.Fatalf("after a fresh check: %v", got)
	}

	cardID, _ := out["card_id"].(string)
	cr, err := http.Get(env.srv.URL + "/api/cards/" + cardID)
	if err != nil {
		t.Fatal(err)
	}
	cr.Body.Close()
	if cr.StatusCode != http.StatusOK {
		t.Fatalf("card status %d", cr.StatusCode)
	}
	noCookie(t, cr)
	if fu.get(usage.EventCardView) != 1 {
		t.Fatalf("card_view %d, want 1", fu.get(usage.EventCardView))
	}

	// An unknown card is not a view.
	nf, err := http.Get(env.srv.URL + "/api/cards/c_0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	nf.Body.Close()
	if fu.get(usage.EventCardView) != 1 {
		t.Fatalf("an unknown card counted as a view")
	}

	// The same title again is answered from the card: a cache hit, and no
	// check starts.
	resp, err = http.Post(env.srv.URL+"/api/checks", "application/json", strings.NewReader(`{"title":"Roblox","age":9}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("repeat: status %d, want 200 from the cache", resp.StatusCode)
	}
	noCookie(t, resp)
	if got := fu.snapshot(); got[usage.EventCacheHit] != 1 || got[usage.EventCheckStarted] != 1 {
		t.Fatalf("after a cache hit: %v", got)
	}
}

// A check that ends in a failure counts check_failed, not check_finished.
func TestUsageCheckFailedCounted(t *testing.T) {
	env := newTestServerEnv(t, 4)
	fu := &fakeUsage{}
	env.server.Usage = fu
	// Empty sources trigger pipeline.ErrNoSources, as the failure test does.
	env.store.mu.Lock()
	env.store.empty = true
	env.store.mu.Unlock()
	env.searcher.mu.Lock()
	env.searcher.emptyDocs = true
	env.searcher.mu.Unlock()

	resp, err := http.Post(env.srv.URL+"/api/checks", "application/json", strings.NewReader(`{"title":"ObscureGame","age":7}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d body %s, want 202", resp.StatusCode, b)
	}
	if out := waitCheckDone(t, env, checkIDOf(t, b)); out["status"] != "error" {
		t.Fatalf("check ended %v, want error", out)
	}
	if got := fu.snapshot(); got[usage.EventCheckFailed] != 1 || got[usage.EventCheckFinished] != 0 || got[usage.EventCapRefused] != 0 {
		t.Fatalf("after a failed check: %v", got)
	}
}

// A counter that panics, and a recorder whose database is closed, never fail
// or slow a request. The recorder logs its failure at warn.
func TestUsageBrokenCounterDoesNotFailRequest(t *testing.T) {
	env := newTestServerEnv(t, 4)
	ctx := context.Background()
	if _, err := env.cardStore.Put(ctx, pipeline.Card{
		Title: "Roblox", Age: 9, Verdict: checklist.No,
		ChecklistVersion: checklist.Version, CheckedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	probe := func(label string) {
		t.Helper()
		listed, err := http.Get(env.srv.URL + "/api/cards")
		if err != nil {
			t.Fatal(err)
		}
		listed.Body.Close()
		if listed.StatusCode != http.StatusOK {
			t.Fatalf("%s: GET /api/cards %d", label, listed.StatusCode)
		}
		resp, err := http.Post(env.srv.URL+"/api/checks", "application/json", strings.NewReader(`{"title":"Roblox","age":9}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: cache hit status %d, want 200", label, resp.StatusCode)
		}
	}

	env.server.Usage = &fakeUsage{boom: true}
	probe("panicking counter")

	db := openTestDB(t, filepath.Join(t.TempDir(), "usage.db"))
	us, err := usage.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	logs := &lockedBuf{}
	rec := usage.NewRecorder(us, slog.New(slog.NewTextHandler(logs, nil)))
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	env.server.Usage = rec
	probe("recorder over a closed database")
	rec.Close()
	if !strings.Contains(logs.String(), "usage: count not stored") {
		t.Fatalf("the failed write was not logged at warn: %q", logs.String())
	}
}

// POST /api/checks refused by the passcode counts passcode_refused. A
// request that passes the gate does not.
func TestUsagePasscodeRefusalCounted(t *testing.T) {
	e := newGatedEnv(t, GateConfig{Passcode: "open-sesame"})
	fu := &fakeUsage{}
	e.server.Usage = fu

	resp, b := postCheck(t, e.url, `{"title":"Roblox","age":9}`, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("no passcode: status %d body %s, want 403", resp.StatusCode, b)
	}
	resp, b = postCheck(t, e.url, `{"title":"Roblox","age":9}`, map[string]string{"X-Passcode": "wrong"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong passcode: status %d body %s, want 403", resp.StatusCode, b)
	}
	if fu.get(usage.EventPasscodeRefused) != 2 {
		t.Fatalf("passcode_refused %d, want 2", fu.get(usage.EventPasscodeRefused))
	}
	resp, b = postCheck(t, e.url, `{"title":"Roblox","age":9}`, map[string]string{"X-Passcode": "open-sesame"})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("with passcode: status %d body %s, want 202", resp.StatusCode, b)
	}
	got := fu.snapshot()
	if got[usage.EventPasscodeRefused] != 2 || got[usage.EventCheckStarted] != 1 {
		t.Fatalf("counts %v, want passcode_refused 2 and check_started 1", got)
	}
}

// A visitor-key check counts visitor_keys and check_started. A check our
// daily cap refuses counts cap_refused, and the cached card counts cache_hit.
func TestUsageVisitorKeysAndCapCounted(t *testing.T) {
	e := newBYOKEnv(t, 1)
	fu := &fakeUsage{}
	e.srv.Usage = fu

	resp, b := byokPost(t, e.url, `{"title":"Roblox","age":9}`, visitorHeaders())
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("visitor check: status %d body %s", resp.StatusCode, b)
	}
	if st, body := waitCheck(t, e.url, startID(t, b)); st != "done" {
		t.Fatalf("visitor check: %s %s", st, body)
	}
	if got := fu.snapshot(); got[usage.EventVisitorKeys] != 1 || got[usage.EventCheckStarted] != 1 || got[usage.EventCheckFinished] != 1 {
		t.Fatalf("after a visitor check: %v", got)
	}

	// Our own check spends the one daily check.
	resp, b = byokPost(t, e.url, `{"title":"Fortnite","age":11}`, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("our check: status %d body %s", resp.StatusCode, b)
	}
	if st, body := waitCheck(t, e.url, startID(t, b)); st != "done" {
		t.Fatalf("our check: %s %s", st, body)
	}

	resp, b = byokPost(t, e.url, `{"title":"Dune","age":12}`, nil)
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("over the cap: status %d body %s, want 402", resp.StatusCode, b)
	}
	noCookie(t, resp)
	if fu.get(usage.EventCapRefused) != 1 {
		t.Fatalf("cap_refused %d, want 1", fu.get(usage.EventCapRefused))
	}

	resp, b = byokPost(t, e.url, `{"title":"Roblox","age":9}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cached title: status %d body %s, want 200", resp.StatusCode, b)
	}
	got := fu.snapshot()
	if got[usage.EventCacheHit] != 1 || got[usage.EventCheckStarted] != 2 || got[usage.EventVisitorKeys] != 1 {
		t.Fatalf("final counts %v", got)
	}
}
