package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeRedditFixture saves a title's parents search with the given result
// URLs, as the T0.4a reddit kind leaves it.
func writeRedditFixture(t *testing.T, dir string, urls ...string) {
	t.Helper()
	var results []map[string]any
	for _, u := range urls {
		results = append(results, map[string]any{"url": u, "raw_content": nil})
	}
	raw, err := json.Marshal(map[string]any{"results": results})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tavily-search-reddit-parents.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// advancedRedditURLs are the three parents-search hits in the fixture. Only
// the first two may be extracted.
var advancedRedditURLs = []string{
	"https://www.reddit.com/r/Roblox/comments/1a/first",
	"https://www.reddit.com/r/Roblox/comments/2b/second",
	"https://www.reddit.com/r/Roblox/comments/3c/third",
}

const (
	robloxAdult = "https://www.commonsensemedia.org/website-reviews/roblox/user-reviews/adult"
	robloxChild = "https://www.commonsensemedia.org/website-reviews/roblox/user-reviews/child"
)

// advancedDir is a title directory with a T0.4 manifest and the reddit fixture.
func advancedDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "MANIFEST.md"), []byte(t04aSeed), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRedditFixture(t, dir, advancedRedditURLs...)
	return dir
}

func advancedBody(urls ...string) string {
	b, _ := json.Marshal(map[string]any{"urls": urls, "extract_depth": "advanced", "format": "text", "include_usage": true})
	return string(b)
}

// TestAdvancedBodies checks the two advanced POSTs: the Common Sense pair from
// the fixed table, and the first two reddit URLs from the fixture.
func TestAdvancedBodies(t *testing.T) {
	dir := advancedDir(t)
	s, err := runT04aIn(t, dir, "advanced", nil, nil, 1)
	if err != nil {
		t.Fatalf("advanced run: %v", err)
	}
	checkCalls(t, s.calls, []struct{ path, body string }{
		{"/extract", advancedBody(robloxAdult, robloxChild)},
		{"/extract", advancedBody(advancedRedditURLs[0], advancedRedditURLs[1])},
	})
	for _, name := range []string{"tavily-extract-csm-advanced.json", "tavily-extract-reddit-advanced.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s not saved: %v", name, err)
		}
	}
}

// TestAdvancedOnlyMakesNoOtherRequest runs -only advanced. The App Store
// server fails the test on any request, and exactly two Tavily calls must be
// made, both extracts.
func TestAdvancedOnlyMakesNoOtherRequest(t *testing.T) {
	dir := advancedDir(t)
	s, err := runT04aIn(t, dir, "advanced", nil, nil, 1)
	if err != nil {
		t.Fatalf("advanced run: %v", err)
	}
	if len(s.calls) != 2 {
		t.Fatalf("got %d Tavily calls, want exactly 2: %+v", len(s.calls), s.calls)
	}
	for i, call := range s.calls {
		if call.path != "/extract" {
			t.Errorf("call %d path = %s, want /extract", i, call.path)
		}
		if call.body["extract_depth"] != "advanced" {
			t.Errorf("call %d extract_depth = %v, want advanced", i, call.body["extract_depth"])
		}
		if call.body["format"] != "text" || call.body["include_usage"] != true {
			t.Errorf("call %d format or include_usage wrong: %v", i, call.body)
		}
	}
	for _, name := range []string{"appstore-search.json", "reviews-page-01.json", "tavily-search-policy.json", "tavily-extract.json", "tavily-search-reddit-community.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s written in -only advanced mode, stat err = %v", name, err)
		}
	}
}

// TestAdvancedCombinedWithOtherKindsRefused checks that advanced runs alone,
// and that a refused combination makes no request.
func TestAdvancedCombinedWithOtherKindsRefused(t *testing.T) {
	dir := advancedDir(t)
	s, err := runT04aIn(t, dir, "advanced,csm", []string{csmPage}, nil, 1)
	if err == nil || !strings.Contains(err.Error(), "runs alone") {
		t.Fatalf("err = %v, want the runs-alone refusal", err)
	}
	if len(s.calls) != 0 {
		t.Errorf("made %d calls for a refused combination, want 0", len(s.calls))
	}
}

// TestAdvancedManifestSection checks the T0.4b section: URLs, status,
// credits per call, failed_results, and that the T0.4 text survives.
func TestAdvancedManifestSection(t *testing.T) {
	dir := advancedDir(t)
	failing := map[string]bool{robloxChild: true}
	if _, err := runT04aIn(t, dir, "advanced", nil, failing, 1); err != nil {
		t.Fatalf("advanced run: %v", err)
	}
	manifest, _ := os.ReadFile(filepath.Join(dir, "MANIFEST.md"))
	m := string(manifest)
	for _, line := range []string{
		"## T0.4b",
		"- T0.4b Tavily credits for this title: 2",
		"| tavily-extract-csm-advanced.json | " + robloxAdult + " " + robloxChild + " | 200 |",
		"failed_results: " + robloxChild + " (fetch failed)",
		"| tavily-extract-reddit-advanced.json |",
		"Tavily credits for this title: 6",
	} {
		if !strings.Contains(m, line) {
			t.Errorf("manifest missing %q:\n%s", line, m)
		}
	}
	// A second run replaces the section rather than adding another.
	if _, err := runT04aIn(t, dir, "advanced", nil, nil, 1); err != nil {
		t.Fatalf("second advanced run: %v", err)
	}
	manifest, _ = os.ReadFile(filepath.Join(dir, "MANIFEST.md"))
	if n := strings.Count(string(manifest), "## T0.4b"); n != 1 {
		t.Errorf("T0.4b heading appears %d times after a rerun, want 1", n)
	}
	if strings.Contains(string(manifest), "fetch failed") {
		t.Error("rerun kept the failed_results of the earlier run")
	}
}

// TestAdvancedGuardStopsBeforeSecondCall: one advanced call past the guard of
// 4 credits stops the title before the reddit extract is made.
func TestAdvancedGuardStopsBeforeSecondCall(t *testing.T) {
	dir := advancedDir(t)
	s, err := runT04aIn(t, dir, "advanced", nil, nil, 5)
	if err == nil || !strings.Contains(err.Error(), "over the guard") {
		t.Fatalf("err = %v, want the credit guard", err)
	}
	if len(s.calls) != 1 {
		t.Errorf("made %d calls, want 1 before the guard stopped the title", len(s.calls))
	}
	manifest, _ := os.ReadFile(filepath.Join(dir, "MANIFEST.md"))
	if !strings.Contains(string(manifest), "- T0.4b Tavily credits for this title: 5") {
		t.Errorf("manifest does not record the spend:\n%s", manifest)
	}
}

// TestAdvancedKeyNeverWritten walks every file the advanced run leaves.
func TestAdvancedKeyNeverWritten(t *testing.T) {
	dir := advancedDir(t)
	failing := map[string]bool{advancedRedditURLs[0]: true}
	if _, err := runT04aIn(t, dir, "advanced", nil, failing, 1); err != nil {
		t.Fatalf("advanced run: %v", err)
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
	if files < 4 { // MANIFEST, reddit fixture, two advanced extracts
		t.Fatalf("only %d files written; the check would be vacuous", files)
	}
}

// TestAdvancedFixedTableAndFixtureLookups covers the table for all four titles
// and the refusals for an unknown slug or a short reddit fixture.
func TestAdvancedFixedTableAndFixtureLookups(t *testing.T) {
	want := map[string]string{
		"roblox":            "https://www.commonsensemedia.org/website-reviews/roblox",
		"minecraft":         "https://www.commonsensemedia.org/app-reviews/minecraft",
		"khan-academy-kids": "https://www.commonsensemedia.org/app-reviews/khan-academy-kids",
		"toca-life-world":   "https://www.commonsensemedia.org/app-reviews/toca-life-world-build-stories",
	}
	for slug, page := range want {
		got, err := csmAdvancedURLs(slug)
		if err != nil {
			t.Fatalf("%s: %v", slug, err)
		}
		if !reflect.DeepEqual(got, []string{page + "/user-reviews/adult", page + "/user-reviews/child"}) {
			t.Errorf("%s urls = %v", slug, got)
		}
	}
	if _, err := csmAdvancedURLs("no-such-title"); err == nil {
		t.Error("unknown slug accepted")
	}

	dir := t.TempDir()
	writeRedditFixture(t, dir, advancedRedditURLs[0])
	if _, err := redditFirstURLs(dir, 2); err == nil {
		t.Error("fixture with one result accepted for two")
	}
	if _, err := redditFirstURLs(t.TempDir(), 2); err == nil {
		t.Error("missing reddit fixture accepted")
	}
	if kinds, err := parseOnly("advanced"); err != nil || !kinds[kindAdvanced] {
		t.Errorf("parseOnly(advanced) = %v, %v", kinds, err)
	}
}
