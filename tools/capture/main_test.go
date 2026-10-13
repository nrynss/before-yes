package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const fakeKey = "fake-key-7f3a9c"

func TestOfficialHost(t *testing.T) {
	tests := []struct {
		name  string
		apps  []app
		title string
		want  string
		hit   int64 // trackId of the expected hit; 0 for none
	}{
		{
			name:  "strips www and lowercases",
			apps:  []app{{ID: 1, Name: "Roblox", SellerURL: "https://www.Roblox.com/support"}},
			title: "Roblox",
			want:  "roblox.com",
			hit:   1,
		},
		{
			name:  "match is case-insensitive",
			apps:  []app{{ID: 2, Name: "ROBLOX", SellerURL: "https://roblox.com/"}},
			title: "roblox",
			want:  "roblox.com",
			hit:   2,
		},
		{
			name:  "keeps a subdomain other than www",
			apps:  []app{{ID: 3, Name: "Minecraft", SellerURL: "https://minecraft.net/en-us"}},
			title: "Minecraft",
			want:  "minecraft.net",
			hit:   3,
		},
		{
			name:  "no hit whose name starts with the title gives no host",
			apps:  []app{{ID: 4, Name: "Toca Boca World: Game & Play", SellerURL: "https://tocaboca.com/"}},
			title: "Toca Life World",
			want:  "",
			hit:   0,
		},
		{
			name: "takes the first matching hit, not the first hit",
			apps: []app{
				{ID: 5, Name: "Roblox Mobile Helper", SellerURL: "https://fan.example/"},
				{ID: 6, Name: "Roblox", SellerURL: "https://www.roblox.com/"},
			},
			title: "Roblox",
			want:  "fan.example",
			hit:   5,
		},
		{
			name:  "a matching hit with no sellerUrl gives no host",
			apps:  []app{{ID: 7, Name: "Roblox", SellerURL: ""}},
			title: "Roblox",
			want:  "",
			hit:   7,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, picked := officialHost(tc.apps, tc.title)
			if got != tc.want {
				t.Errorf("host = %q, want %q", got, tc.want)
			}
			switch {
			case tc.hit == 0 && picked != nil:
				t.Errorf("picked %q, want no hit", picked.Name)
			case tc.hit != 0 && (picked == nil || picked.ID != tc.hit):
				t.Errorf("picked %+v, want trackId %d", picked, tc.hit)
			}
		})
	}
}

// tavilyCall is one request the fake Tavily server received.
type tavilyCall struct {
	path     string
	rawQuery string
	auth     string
	body     map[string]any
}

// fakeTavily answers searches and extracts. Each call reports credits. Policy
// and terms return their own URLs so the extract target is checkable.
type fakeTavily struct {
	mu      sync.Mutex
	calls   []tavilyCall
	credits float64
}

func (f *fakeTavily) handler(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	f.mu.Lock()
	f.calls = append(f.calls, tavilyCall{path: r.URL.Path, rawQuery: r.URL.RawQuery, auth: r.Header.Get("Authorization"), body: body})
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/extract" {
		var results []string
		urls, _ := body["urls"].([]any)
		for _, u := range urls {
			results = append(results, fmt.Sprintf(`{"url":%q,"raw_content":"policy text"}`, u))
		}
		fmt.Fprintf(w, `{"results":[%s],"usage":{"credits":%v}}`, strings.Join(results, ","), f.credits)
		return
	}
	q, _ := body["query"].(string)
	u := "https://example.org/other"
	switch {
	case strings.Contains(q, "privacy policy"):
		u = "https://www.roblox.com/privacy"
	case strings.Contains(q, "terms of service"):
		u = "https://www.roblox.com/terms"
	}
	fmt.Fprintf(w, `{"results":[{"title":"t","url":%q,"content":"c","raw_content":"r"}],"usage":{"credits":%v}}`, u, f.credits)
}

func fakeItunes(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("term") != "Roblox" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"resultCount":1,"results":[{"trackId":431946152,"trackName":"Roblox","sellerUrl":"https://www.roblox.com/","trackViewUrl":"https://apps.apple.com/us/app/roblox/id431946152"}]}`)
	})
	mux.HandleFunc("/us/rss/customerreviews/page=1/id=431946152/sortby=mostrecent/json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"feed":{"entry":[
			{"id":{"label":"app"},"title":{"label":"Roblox"}},
			{"id":{"label":"312"},"title":{"label":"Filter failed"},"content":{"label":"The filter did not catch it when someone asked my 7 year old for her number"},"im:rating":{"label":"1"}}
		]}}`)
	})
	mux.HandleFunc("/us/rss/customerreviews/page=2/id=431946152/sortby=mostrecent/json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"feed":{"title":"empty"}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newCapturer(t *testing.T, itunesURL string, f *fakeTavily) *capturer {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	return &capturer{
		itunes: itunesURL,
		tavily: srv.URL,
		key:    fakeKey,
		client: &http.Client{Timeout: 10 * time.Second},
		pause:  0,
		now:    time.Now,
	}
}

// roblox is the title the request-body test runs against.
var roblox = demo{Slug: "roblox", Title: "Roblox", ID: 431946152}

func TestRequestBodiesMatchPlan(t *testing.T) {
	itunes := fakeItunes(t)
	f := &fakeTavily{credits: 1}
	c := newCapturer(t, itunes.URL, f)
	out := t.TempDir()
	if err := c.capture(roblox, out); err != nil {
		t.Fatalf("capture: %v", err)
	}

	// Expected bodies, written out from gather.go's plan() and the extract call.
	want := []string{
		`{"query":"Roblox privacy policy","max_results":5,"include_domains":["roblox.com"],"include_usage":true}`,
		`{"query":"Roblox terms of service","max_results":5,"include_domains":["roblox.com"],"include_usage":true}`,
		`{"query":"Roblox parents experience kids chat strangers purchases","max_results":8,"include_domains":["reddit.com","commonsensemedia.org","mumsnet.com","netmums.com"],"include_raw_content":true,"include_usage":true}`,
		`{"query":"Roblox children safety fine lawsuit FTC regulator data breach","topic":"news","max_results":8,"include_raw_content":true,"include_usage":true}`,
		`{"query":"Roblox age rating ESRB PEGI parental controls","max_results":6,"include_raw_content":true,"include_usage":true}`,
		`{"urls":["https://www.roblox.com/privacy","https://www.roblox.com/terms"],"extract_depth":"basic","format":"text","include_usage":true}`,
	}
	if len(f.calls) != len(want) {
		t.Fatalf("got %d Tavily calls, want %d", len(f.calls), len(want))
	}
	paths := []string{"/search", "/search", "/search", "/search", "/search", "/extract"}
	for i, call := range f.calls {
		var wantBody map[string]any
		if err := json.Unmarshal([]byte(want[i]), &wantBody); err != nil {
			t.Fatal(err)
		}
		if call.path != paths[i] {
			t.Errorf("call %d path = %s, want %s", i, call.path, paths[i])
		}
		if !reflect.DeepEqual(call.body, wantBody) {
			t.Errorf("call %d body = %v, want %v", i, call.body, wantBody)
		}
		if call.auth != "Bearer "+fakeKey {
			t.Errorf("call %d Authorization header not the bearer key", i)
		}
		if strings.Contains(call.rawQuery, fakeKey) || strings.Contains(call.path, fakeKey) {
			t.Errorf("call %d carries the key outside the header", i)
		}
	}

	// Review feed stops at the first page with no entries, and that page is not saved.
	if _, err := os.Stat(filepath.Join(out, "reviews-page-01.json")); err != nil {
		t.Errorf("page 1 not saved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "reviews-page-02.json")); !os.IsNotExist(err) {
		t.Errorf("page 2 should not be saved, stat err = %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(out, "MANIFEST.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"Official host used for policy and terms: roblox.com",
		"Reviews captured: 1",
		"Tavily credits for this title: 6",
		"reviews stopped at page 2: no feed.entry",
	} {
		if !strings.Contains(string(manifest), line) {
			t.Errorf("manifest missing %q", line)
		}
	}
}

func TestKeyNeverWritten(t *testing.T) {
	itunes := fakeItunes(t)
	f := &fakeTavily{credits: 1}
	c := newCapturer(t, itunes.URL, f)
	out := t.TempDir()
	if err := c.capture(roblox, out); err != nil {
		t.Fatalf("capture: %v", err)
	}

	files := 0
	err := filepath.Walk(out, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		files++
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), fakeKey) {
			t.Errorf("%s contains the key", filepath.Base(path))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 8 {
		t.Fatalf("only %d files written; the check would be vacuous", files)
	}
}

func TestCreditGuardStopsTheTitle(t *testing.T) {
	itunes := fakeItunes(t)
	f := &fakeTavily{credits: 4}
	c := newCapturer(t, itunes.URL, f)
	err := c.capture(roblox, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "over the guard") {
		t.Fatalf("err = %v, want the credit guard", err)
	}
	if len(f.calls) != 3 {
		t.Errorf("made %d Tavily calls before stopping, want 3 (12 credits passes the guard of 10)", len(f.calls))
	}
}

// csmPage is the Common Sense title page the T0.4a tests search for.
const csmPage = "https://www.commonsensemedia.org/app-reviews/roblox"

// t04aSeed is a T0.4 MANIFEST.md, with its own credit line that must survive.
const t04aSeed = "# Capture manifest: Roblox\n\n- Tavily credits for this title: 6\n\n## Files\n\n| File |\n|---|\n\n## Notes\n\n- none\n"

// scripted answers Tavily calls through answer and records each one.
type scripted struct {
	mu     sync.Mutex
	calls  []tavilyCall
	answer func(path string, body map[string]any) (int, string)
}

func (s *scripted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	s.mu.Lock()
	s.calls = append(s.calls, tavilyCall{path: r.URL.Path, rawQuery: r.URL.RawQuery, auth: r.Header.Get("Authorization"), body: body})
	s.mu.Unlock()
	status, out := s.answer(r.URL.Path, body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprint(w, out)
}

// bodyURLs returns the urls field of an extract body.
func bodyURLs(body map[string]any) []string {
	var out []string
	urls, _ := body["urls"].([]any)
	for _, u := range urls {
		s, _ := u.(string)
		out = append(out, s)
	}
	return out
}

// answerT04a answers the T0.4a calls. Searches for the Common Sense query return
// csmHits; every other search returns one Reddit thread. Extract returns text
// for each URL except those in failing, which go to failed_results.
func answerT04a(csmHits []string, failing map[string]bool, credits float64) func(string, map[string]any) (int, string) {
	return func(path string, body map[string]any) (int, string) {
		if path == "/extract" {
			var ok, bad []map[string]any
			for _, u := range bodyURLs(body) {
				if failing[u] {
					bad = append(bad, map[string]any{"url": u, "error": "fetch failed"})
					continue
				}
				ok = append(ok, map[string]any{"url": u, "raw_content": "review text for " + u})
			}
			out, _ := json.Marshal(map[string]any{"results": ok, "failed_results": bad, "usage": map[string]any{"credits": credits}})
			return http.StatusOK, string(out)
		}
		q, _ := body["query"].(string)
		var results []map[string]any
		if strings.Contains(q, "Common Sense Media review") {
			for _, u := range csmHits {
				results = append(results, map[string]any{"url": u, "title": "t", "content": "c"})
			}
		} else {
			results = []map[string]any{{"url": "https://www.reddit.com/r/Roblox/comments/1/x", "raw_content": "thread"}}
		}
		out, _ := json.Marshal(map[string]any{"results": results, "usage": map[string]any{"credits": credits}})
		return http.StatusOK, string(out)
	}
}

// runT04a runs captureT04a for Roblox in a directory seeded with a T0.4
// manifest. The App Store server fails the test if anything calls it.
func runT04a(t *testing.T, only string, csmHits []string, failing map[string]bool, credits float64) (*scripted, string, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "MANIFEST.md"), []byte(t04aSeed), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := runT04aIn(t, dir, only, csmHits, failing, credits)
	return s, dir, err
}

// runT04aIn runs captureT04a for Roblox in dir, which must hold a MANIFEST.md.
func runT04aIn(t *testing.T, dir, only string, csmHits []string, failing map[string]bool, credits float64) (*scripted, error) {
	t.Helper()
	s := &scripted{answer: answerT04a(csmHits, failing, credits)}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	itunes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("App Store was called: %s", r.URL.Path)
		http.NotFound(w, r)
	}))
	t.Cleanup(itunes.Close)
	c := &capturer{
		itunes: itunes.URL,
		tavily: srv.URL,
		key:    fakeKey,
		client: &http.Client{Timeout: 10 * time.Second},
		now:    time.Now,
	}
	kinds, err := parseOnly(only)
	if err != nil {
		t.Fatal(err)
	}
	err = c.captureT04a(roblox, dir, kinds)
	return s, err
}

// checkCalls compares each recorded call with the expected path and body.
func checkCalls(t *testing.T, calls []tavilyCall, want []struct{ path, body string }) {
	t.Helper()
	if len(calls) != len(want) {
		t.Fatalf("got %d Tavily calls, want %d: %+v", len(calls), len(want), calls)
	}
	for i, call := range calls {
		var wantBody map[string]any
		if err := json.Unmarshal([]byte(want[i].body), &wantBody); err != nil {
			t.Fatal(err)
		}
		if call.path != want[i].path {
			t.Errorf("call %d path = %s, want %s", i, call.path, want[i].path)
		}
		if !reflect.DeepEqual(call.body, wantBody) {
			t.Errorf("call %d body = %v, want %v", i, call.body, wantBody)
		}
		if call.auth != "Bearer "+fakeKey {
			t.Errorf("call %d Authorization header not the bearer key", i)
		}
	}
}

func TestCSMSearchAndExtractBodies(t *testing.T) {
	s, _, err := runT04a(t, "csm", []string{csmPage, csmPage + "/user-reviews/adult"}, nil, 1)
	if err != nil {
		t.Fatalf("captureT04a: %v", err)
	}
	checkCalls(t, s.calls, []struct{ path, body string }{
		{"/search", `{"query":"Roblox Common Sense Media review","max_results":5,"include_domains":["commonsensemedia.org"],"include_usage":true}`},
		{"/extract", `{"urls":["https://www.commonsensemedia.org/app-reviews/roblox/user-reviews/adult","https://www.commonsensemedia.org/app-reviews/roblox/user-reviews/child"],"extract_depth":"basic","format":"text","include_usage":true}`},
	})
}

func TestRedditBodies(t *testing.T) {
	s, _, err := runT04a(t, "reddit", nil, nil, 1)
	if err != nil {
		t.Fatalf("captureT04a: %v", err)
	}
	checkCalls(t, s.calls, []struct{ path, body string }{
		{"/search", `{"query":"Roblox parents kids safety experience","max_results":8,"include_domains":["reddit.com"],"include_raw_content":true,"include_usage":true}`},
		{"/search", `{"query":"Roblox chat scams moderation players","max_results":8,"include_domains":["reddit.com"],"include_raw_content":true,"include_usage":true}`},
	})
}

func TestTitleSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Roblox":            "roblox",
		"Khan Academy Kids": "khan-academy-kids",
		"Toca Life World":   "toca-life-world",
		"Minecraft":         "minecraft",
	} {
		if got := titleSlug(in); got != want {
			t.Errorf("titleSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPickReviewPage(t *testing.T) {
	const brookhaven = "https://www.commonsensemedia.org/website-reviews/brookhaven-rp"
	tests := []struct {
		name string
		urls []string
		want string
		near []string
	}{
		{"app-reviews page for the title", []string{csmPage}, csmPage, nil},
		{"game-reviews page with trailing slash", []string{"https://www.commonsensemedia.org/game-reviews/roblox/"}, "https://www.commonsensemedia.org/game-reviews/roblox", nil},
		{"website-reviews page for the title", []string{"https://www.commonsensemedia.org/website-reviews/roblox"}, "https://www.commonsensemedia.org/website-reviews/roblox", nil},
		{"user-reviews page is skipped for the page above it", []string{csmPage + "/user-reviews/adult", csmPage}, csmPage, nil},
		{"a game inside the title is not the title", []string{brookhaven}, "", []string{brookhaven}},
		{"a slug that only starts with the title is not the title", []string{"https://www.commonsensemedia.org/app-reviews/roblox-mobile"}, "", []string{"https://www.commonsensemedia.org/app-reviews/roblox-mobile"}},
		{"category index without a slug is not a page", []string{"https://www.commonsensemedia.org/app-reviews", "https://www.commonsensemedia.org/app-reviews/"}, "", nil},
		{"no review page among parent guides", []string{"https://www.commonsensemedia.org/articles/parents-ultimate-guide-to-roblox"}, "", nil},
		{"a look-alike host is refused", []string{"https://commonsensemedia.org.example/app-reviews/roblox"}, "", nil},
		{"no hits", nil, "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, near := pickReviewPage(tc.urls, "roblox")
			if got != tc.want {
				t.Errorf("page = %q, want %q", got, tc.want)
			}
			if !reflect.DeepEqual(near, tc.near) {
				t.Errorf("near misses = %v, want %v", near, tc.near)
			}
		})
	}
}

func TestCSMNoReviewPageSkipsExtract(t *testing.T) {
	const brookhaven = "https://www.commonsensemedia.org/website-reviews/brookhaven-rp"
	s, dir, err := runT04a(t, "csm", []string{brookhaven}, nil, 1)
	if err != nil {
		t.Fatalf("captureT04a: %v", err)
	}
	if len(s.calls) != 1 || s.calls[0].path != "/search" {
		t.Fatalf("calls = %+v, want the search only", s.calls)
	}
	if _, err := os.Stat(filepath.Join(dir, "tavily-extract-csm.json")); !os.IsNotExist(err) {
		t.Errorf("extract file written with no review page, stat err = %v", err)
	}
	manifest, _ := os.ReadFile(filepath.Join(dir, "MANIFEST.md"))
	for _, line := range []string{
		"no Common Sense Media review page for slug \"roblox\"",
		"review pages for other titles, not used: " + brookhaven,
		"- Common Sense review page: none found",
	} {
		if !strings.Contains(string(manifest), line) {
			t.Errorf("manifest missing %q:\n%s", line, manifest)
		}
	}
}

// TestT04aRowsAndCreditsAcrossRuns checks that a run rewrites only its own
// kinds: reddit rows survive a csm run, and a replaced csm call keeps its
// credits in the total.
func TestT04aRowsAndCreditsAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "MANIFEST.md"), []byte(t04aSeed), 0o644); err != nil {
		t.Fatal(err)
	}
	// Reddit: two searches at 1 credit each.
	if _, err := runT04aIn(t, dir, "reddit", nil, nil, 1); err != nil {
		t.Fatalf("reddit run: %v", err)
	}
	// Csm with a review page: search and extract at 1 each.
	if _, err := runT04aIn(t, dir, "csm", []string{csmPage}, nil, 1); err != nil {
		t.Fatalf("csm run: %v", err)
	}
	manifest, _ := os.ReadFile(filepath.Join(dir, "MANIFEST.md"))
	if !strings.Contains(string(manifest), "T0.4a Tavily credits for this title: 4") {
		t.Errorf("after two runs, want total 4:\n%s", manifest)
	}
	// Csm with no page: the search is new, and the old csm rows are replaced.
	if _, err := runT04aIn(t, dir, "csm", []string{"https://www.commonsensemedia.org/website-reviews/brookhaven-rp"}, nil, 1); err != nil {
		t.Fatalf("third run: %v", err)
	}
	manifest, _ = os.ReadFile(filepath.Join(dir, "MANIFEST.md"))
	for _, line := range []string{
		"- Credits on calls replaced by later runs: 2",
		"- T0.4a Tavily credits for this title: 5",
		"tavily-search-reddit-parents.json",
		"tavily-search-reddit-community.json",
		"- Common Sense review page: none found",
	} {
		if !strings.Contains(string(manifest), line) {
			t.Errorf("manifest missing %q:\n%s", line, manifest)
		}
	}
	if strings.Contains(string(manifest), "| tavily-extract-csm.json |") {
		t.Error("replaced csm extract row is still in the manifest")
	}
	if _, err := os.Stat(filepath.Join(dir, "tavily-extract-csm.json")); !os.IsNotExist(err) {
		t.Errorf("stale csm extract file kept, stat err = %v", err)
	}
	if strings.Count(string(manifest), "## T0.4a") != 1 || !strings.Contains(string(manifest), "Tavily credits for this title: 6") {
		t.Error("T0.4 section or heading count changed")
	}
}

func TestCSMRetryOnceFromUserReviewHit(t *testing.T) {
	teen := csmPage + "/user-reviews/teen"
	adult := csmPage + "/user-reviews/adult"
	s, dir, err := runT04a(t, "csm", []string{csmPage, teen}, map[string]bool{adult: true}, 1)
	if err != nil {
		t.Fatalf("captureT04a: %v", err)
	}
	checkCalls(t, s.calls, []struct{ path, body string }{
		{"/search", `{"query":"Roblox Common Sense Media review","max_results":5,"include_domains":["commonsensemedia.org"],"include_usage":true}`},
		{"/extract", `{"urls":["` + adult + `","` + csmPage + `/user-reviews/child"],"extract_depth":"basic","format":"text","include_usage":true}`},
		{"/extract", `{"urls":["` + teen + `"],"extract_depth":"basic","format":"text","include_usage":true}`},
	})
	if _, err := os.Stat(filepath.Join(dir, "tavily-extract-csm-retry.json")); err != nil {
		t.Errorf("retry file not saved: %v", err)
	}
	manifest, _ := os.ReadFile(filepath.Join(dir, "MANIFEST.md"))
	if !strings.Contains(string(manifest), "returned no text for: "+adult) {
		t.Errorf("manifest does not name the failed URL:\n%s", manifest)
	}
}

// TestOnlySkipsAppStoreAndOldTavily runs the -only path end to end through
// parseOnly. The App Store server fails the test on any request, and no Tavily
// call may be one of the first kinds.
func TestOnlySkipsAppStoreAndOldTavily(t *testing.T) {
	if _, err := parseOnly("bogus"); err == nil {
		t.Error("parseOnly accepted an unknown kind")
	}
	if kinds, err := parseOnly(""); err != nil || kinds != nil {
		t.Errorf("parseOnly(\"\") = %v, %v; want no filter", kinds, err)
	}

	s, dir, err := runT04a(t, "csm,reddit", []string{csmPage}, nil, 1)
	if err != nil {
		t.Fatalf("captureT04a: %v", err)
	}
	old := []string{"privacy policy", "terms of service", "parents experience kids chat strangers", "children safety fine", "age rating"}
	for i, call := range s.calls {
		q, _ := call.body["query"].(string)
		for _, o := range old {
			if strings.Contains(q, o) {
				t.Errorf("call %d ran an old kind: %q", i, q)
			}
		}
	}
	for _, name := range []string{"appstore-search.json", "tavily-search-policy.json", "tavily-search-terms.json", "tavily-extract.json", "reviews-page-01.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s written in -only mode, stat err = %v", name, err)
		}
	}
	if len(s.calls) != 4 {
		t.Errorf("got %d calls, want 4 (csm search, csm extract, two reddit)", len(s.calls))
	}
}

func TestT04aKeyNeverWritten(t *testing.T) {
	_, dir, err := runT04a(t, "csm,reddit", []string{csmPage}, map[string]bool{csmPage + "/user-reviews/adult": true}, 1)
	if err != nil {
		t.Fatalf("captureT04a: %v", err)
	}
	files := 0
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		files++
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), fakeKey) {
			t.Errorf("%s contains the key", filepath.Base(path))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 5 { // MANIFEST, csm search, csm extract, two reddit searches
		t.Fatalf("only %d files written; the check would be vacuous", files)
	}
}

func TestT04aManifestSectionReplaced(t *testing.T) {
	_, dir, err := runT04a(t, "reddit", nil, nil, 1)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	// A second run with the same kind must replace the section, not add to it.
	kinds, _ := parseOnly("reddit")
	c := &capturer{itunes: "http://unused.invalid", tavily: "", key: fakeKey, client: &http.Client{Timeout: time.Second}, now: time.Now}
	s := &scripted{answer: answerT04a(nil, nil, 1)}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	c.tavily = srv.URL
	if err := c.captureT04a(roblox, dir, kinds); err != nil {
		t.Fatalf("second run: %v", err)
	}
	manifest, _ := os.ReadFile(filepath.Join(dir, "MANIFEST.md"))
	if n := strings.Count(string(manifest), "## T0.4a"); n != 1 {
		t.Errorf("T0.4a heading appears %d times, want 1", n)
	}
	if !strings.Contains(string(manifest), "Tavily credits for this title: 6") {
		t.Error("the T0.4 section was lost")
	}
}

func TestT04aCreditGuard(t *testing.T) {
	// Search 4 and extract 4 make 8, past the T0.4a guard of 6.
	s, dir, err := runT04a(t, "csm", []string{csmPage}, nil, 4)
	if err == nil || !strings.Contains(err.Error(), "over the guard") {
		t.Fatalf("err = %v, want the credit guard", err)
	}
	if len(s.calls) != 2 {
		t.Errorf("made %d calls before stopping, want 2", len(s.calls))
	}
	manifest, _ := os.ReadFile(filepath.Join(dir, "MANIFEST.md"))
	if !strings.Contains(string(manifest), "T0.4a Tavily credits for this title: 8") {
		t.Errorf("manifest does not record the spend:\n%s", manifest)
	}
}
