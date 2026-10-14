package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const robloxPage = "https://apps.apple.com/us/app/roblox/id431946152?uo=4"

// noReviewsText is an extract answer with the page's description and footer
// but no text under "Ratings and Reviews".
const noReviewsText = "Roblox\n\nGet the app. Free.\n\nThe platform where anyone can create and play games, with a long description line here.\n\nInformation\n\nSeller: Roblox Corporation, with a long footer line for the test."

// reviewsText is an extract answer with one review under the heading.
const reviewsText = "Roblox\n\nRatings & Reviews\n\n4.5 out of 5\n\nGreat game for my kids, they love building worlds and the review text here is long enough to count.\n\nReviewer A, 2026-01-02"

// storeDir is a title directory with the T0.4 manifest and a two-hit App Store
// search. The title's own hit is the second one.
func storeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "MANIFEST.md"), []byte(t04aSeed), 0o644); err != nil {
		t.Fatal(err)
	}
	search := `{"resultCount":2,"results":[` +
		`{"trackId":1617391485,"trackName":"Block Blast","trackViewUrl":"https://apps.apple.com/us/app/block-blast/id1617391485?uo=4"},` +
		`{"trackId":431946152,"trackName":"Roblox","trackViewUrl":"` + robloxPage + `"}]}`
	if err := os.WriteFile(filepath.Join(dir, "appstore-search.json"), []byte(search), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// storeAnswer answers every extract with text, at the given credits.
func storeAnswer(text string, credits float64) func(string, map[string]any) (int, string) {
	return func(path string, body map[string]any) (int, string) {
		var results []map[string]any
		for _, u := range bodyURLs(body) {
			results = append(results, map[string]any{"url": u, "raw_content": text})
		}
		out, _ := json.Marshal(map[string]any{"results": results, "failed_results": []any{}, "usage": map[string]any{"credits": credits}})
		return http.StatusOK, string(out)
	}
}

// storeRun runs captureT04a with -only storepage at the given depth. The App
// Store server fails the test if anything calls it.
func storeRun(t *testing.T, dir, depth string, answer func(string, map[string]any) (int, string)) (*scripted, error) {
	t.Helper()
	s := &scripted{answer: answer}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	itunes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("App Store was called: %s", r.URL.Path)
		http.NotFound(w, r)
	}))
	t.Cleanup(itunes.Close)
	c := &capturer{
		itunes:     itunes.URL,
		tavily:     srv.URL,
		key:        fakeKey,
		client:     &http.Client{Timeout: 10 * time.Second},
		now:        time.Now,
		storeDepth: depth,
	}
	kinds, err := parseOnly(kindStorePage)
	if err != nil {
		t.Fatal(err)
	}
	return s, c.captureT04a(roblox, dir, kinds)
}

// storeBody is the extract body the store-page kind must send.
func storeBody(depth string) string {
	b, _ := json.Marshal(map[string]any{"urls": []string{robloxPage}, "extract_depth": depth, "format": "text", "include_usage": true})
	return string(b)
}

func readManifest(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "MANIFEST.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestStorePageURL(t *testing.T) {
	dir := storeDir(t)
	got, err := storePageURL(dir, 431946152)
	if err != nil || got != robloxPage {
		t.Fatalf("storePageURL = %q, %v; want %q", got, err, robloxPage)
	}
	if _, err := storePageURL(dir, 1); err == nil || !strings.Contains(err.Error(), "no hit with trackId 1") {
		t.Errorf("unknown trackId: err = %v", err)
	}
}

func TestStorePageHasReviews(t *testing.T) {
	cases := map[string]bool{
		reviewsText:   true,
		noReviewsText: false,
		"Ratings & Reviews\n\n4.5 out of 5\n\nshort\n":                                                    false,
		"Reviews\n\nA long line of text that is well over sixty characters, but no ratings heading here.": false,
		"": false,
	}
	for text, want := range cases {
		body, _ := json.Marshal(map[string]any{"results": []map[string]any{{"url": robloxPage, "raw_content": text}}})
		if got := storePageHasReviews(body); got != want {
			t.Errorf("storePageHasReviews(%.40q...) = %v, want %v", text, got, want)
		}
	}
	if storePageHasReviews([]byte("not json")) {
		t.Error("unparseable answer counted as review text")
	}
}

// TestStorePageStopsAfterBasicWithReviews: a basic answer with review text is
// final, and advanced is not called.
func TestStorePageStopsAfterBasicWithReviews(t *testing.T) {
	dir := storeDir(t)
	s, err := storeRun(t, dir, depthAuto, storeAnswer(reviewsText, 1))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	checkCalls(t, s.calls, []struct{ path, body string }{{"/extract", storeBody("basic")}})
	if _, err := os.Stat(filepath.Join(dir, fileStorePage)); err != nil {
		t.Errorf("%s not saved: %v", fileStorePage, err)
	}
	if _, err := os.Stat(filepath.Join(dir, fileStorePageBasic)); !os.IsNotExist(err) {
		t.Errorf("%s saved although basic had review text, stat err = %v", fileStorePageBasic, err)
	}
}

// TestStorePageEscalatesWithoutReviews: a basic answer with no review text is
// kept as the basic file and advanced is run, which is the final file.
func TestStorePageEscalatesWithoutReviews(t *testing.T) {
	dir := storeDir(t)
	s, err := storeRun(t, dir, depthAuto, storeAnswer(noReviewsText, 1))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	checkCalls(t, s.calls, []struct{ path, body string }{
		{"/extract", storeBody("basic")},
		{"/extract", storeBody("advanced")},
	})
	for _, name := range []string{fileStorePage, fileStorePageBasic} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s not saved: %v", name, err)
		}
	}
	m := readManifest(t, dir)
	for _, line := range []string{
		"## T3.8a",
		"- T3.8a Tavily credits for this title: 2",
		"| tavily-extract-storepage-basic.json | " + robloxPage + " | 200 |",
		"basic; 1 results with text; review text: no; failed_results: none",
		"| tavily-extract-storepage.json | " + robloxPage + " | 200 |",
		"advanced; 1 results with text; review text: no; failed_results: none",
		"basic extract shows no review text",
		"Tavily credits for this title: 6",
	} {
		if !strings.Contains(m, line) {
			t.Errorf("manifest missing %q:\n%s", line, m)
		}
	}
}

// TestStorePageBasicDepthNeverEscalates: -depth basic makes one call even
// when the answer has no review text.
func TestStorePageBasicDepthNeverEscalates(t *testing.T) {
	dir := storeDir(t)
	s, err := storeRun(t, dir, depthBasic, storeAnswer(noReviewsText, 1))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	checkCalls(t, s.calls, []struct{ path, body string }{{"/extract", storeBody("basic")}})
}

// TestStorePageAdvancedDepthOnly: -depth advanced makes one advanced call.
func TestStorePageAdvancedDepthOnly(t *testing.T) {
	dir := storeDir(t)
	s, err := storeRun(t, dir, depthAdvanced, storeAnswer(reviewsText, 2))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	checkCalls(t, s.calls, []struct{ path, body string }{{"/extract", storeBody("advanced")}})
	if _, err := os.Stat(filepath.Join(dir, fileStorePageBasic)); !os.IsNotExist(err) {
		t.Errorf("basic file written in advanced-only mode, stat err = %v", err)
	}
}

// TestStorePageCreditGuard: basic costs 2, which is within the guard of 3;
// the advanced call takes the title to 4 and stops it, and the manifest keeps
// the spend.
func TestStorePageCreditGuard(t *testing.T) {
	dir := storeDir(t)
	s, err := storeRun(t, dir, depthAuto, storeAnswer(noReviewsText, 2))
	if err == nil || !strings.Contains(err.Error(), "over the guard") {
		t.Fatalf("err = %v, want the credit guard", err)
	}
	if len(s.calls) != 2 {
		t.Errorf("made %d calls, want 2", len(s.calls))
	}
	if m := readManifest(t, dir); !strings.Contains(m, "- T3.8a Tavily credits for this title: 4") {
		t.Errorf("manifest does not record the spend:\n%s", m)
	}
}

// TestStorePageRerunReplacesSection: a rerun that finds review text on basic
// removes the earlier basic file and leaves one T3.8a section.
func TestStorePageRerunReplacesSection(t *testing.T) {
	dir := storeDir(t)
	if _, err := storeRun(t, dir, depthAuto, storeAnswer(noReviewsText, 1)); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if _, err := storeRun(t, dir, depthAuto, storeAnswer(reviewsText, 1)); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, fileStorePageBasic)); !os.IsNotExist(err) {
		t.Errorf("stale basic file survived a rerun, stat err = %v", err)
	}
	m := readManifest(t, dir)
	if n := strings.Count(m, "## T3.8a"); n != 1 {
		t.Errorf("T3.8a heading appears %d times, want 1", n)
	}
	if strings.Contains(m, "tavily-extract-storepage-basic.json") {
		t.Error("manifest kept the row of the earlier basic file")
	}
	if !strings.Contains(m, "- T3.8a Tavily credits for this title: 1") {
		t.Errorf("manifest credits not rebuilt:\n%s", m)
	}
	if !strings.Contains(m, "Tavily credits for this title: 6") {
		t.Error("the T0.4 text was lost")
	}
}

// TestStorePageMissingSearch: without the saved App Store search there is no
// URL, so no Tavily call is made.
func TestStorePageMissingSearch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "MANIFEST.md"), []byte(t04aSeed), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := storeRun(t, dir, depthAuto, storeAnswer(reviewsText, 1))
	if err == nil || !strings.Contains(err.Error(), "appstore-search.json") {
		t.Fatalf("err = %v, want the missing search", err)
	}
	if len(s.calls) != 0 {
		t.Errorf("made %d calls without a URL, want 0", len(s.calls))
	}
}

// TestStorePageKeyNeverWritten walks every file of an escalating run.
func TestStorePageKeyNeverWritten(t *testing.T) {
	dir := storeDir(t)
	if _, err := storeRun(t, dir, depthAuto, storeAnswer(noReviewsText, 1)); err != nil {
		t.Fatalf("run: %v", err)
	}
	files := 0
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
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
	if files < 4 { // MANIFEST, appstore search, basic and final extracts
		t.Fatalf("only %d files written; the check would be vacuous", files)
	}
}

// TestStorePageRefusedWithOtherKinds: storepage runs alone, and a refused
// combination makes no request.
func TestStorePageRefusedWithOtherKinds(t *testing.T) {
	dir := storeDir(t)
	s, err := runT04aIn(t, dir, "storepage,csm", []string{csmPage}, nil, 1)
	if err == nil || !strings.Contains(err.Error(), "runs alone") {
		t.Fatalf("err = %v, want the runs-alone refusal", err)
	}
	if len(s.calls) != 0 {
		t.Errorf("made %d calls for a refused combination, want 0", len(s.calls))
	}
}

func TestParseDepth(t *testing.T) {
	for _, ok := range []string{"auto", "basic", "advanced"} {
		if got, err := parseDepth(ok); err != nil || got != ok {
			t.Errorf("parseDepth(%q) = %q, %v", ok, got, err)
		}
	}
	if _, err := parseDepth("deep"); err == nil {
		t.Error("parseDepth(deep) accepted an unknown depth")
	}
}
