package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// altAnswer answers the alternatives search with n results, the first raw of
// them with page text, at the given credits.
func altAnswer(credits float64, n, raw int) func(string, map[string]any) (int, string) {
	return func(path string, body map[string]any) (int, string) {
		var results []map[string]any
		for i := 0; i < n; i++ {
			r := map[string]any{
				"title":   fmt.Sprintf("Sample Pick %d", i+1),
				"url":     fmt.Sprintf("https://www.example.com/list-page-%d", i+1),
				"content": "a snippet",
			}
			if i < raw {
				r["raw_content"] = fmt.Sprintf("1. Sample Pick %d", i+1)
			}
			results = append(results, r)
		}
		out, _ := json.Marshal(map[string]any{"results": results, "usage": map[string]any{"credits": credits}})
		return http.StatusOK, string(out)
	}
}

// altRun runs captureAlternatives for d into a fresh temp directory. It
// returns the scripted server, the Tavily base URL, the directory and the
// run's error. The App Store server fails the test if anything calls it.
func altRun(t *testing.T, d demo, answer func(string, map[string]any) (int, string)) (*scripted, string, string, error) {
	t.Helper()
	dir := t.TempDir()
	s := &scripted{answer: answer}
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
	return s, srv.URL, dir, c.captureAlternatives(d, dir)
}

// The request is the one search FindAlternatives makes, with usage asked for.
func TestAlternativesCaptureRequestShape(t *testing.T) {
	s, _, _, err := altRun(t, roblox, altAnswer(1, 1, 1))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	b, _ := json.Marshal(map[string]any{
		"query":               "safer app like Roblox for a 9 year old",
		"max_results":         8,
		"include_raw_content": true,
		"include_usage":       true,
	})
	checkCalls(t, s.calls, []struct{ path, body string }{{"/search", string(b)}})
}

// The answer and the manifest land in the directory, with the query, the
// credits and the note.
func TestAlternativesCaptureSavesAnswerAndManifest(t *testing.T) {
	s, base, dir, err := altRun(t, roblox, altAnswer(1, 3, 2))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(s.calls) != 1 {
		t.Fatalf("made %d calls, want 1", len(s.calls))
	}
	raw, err := os.ReadFile(filepath.Join(dir, fileAlternatives))
	if err != nil {
		t.Fatalf("%s: %v", fileAlternatives, err)
	}
	var saved struct {
		Results []struct {
			Title string `json:"title"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &saved); err != nil || len(saved.Results) != 3 {
		t.Errorf("saved answer: %d results, err %v", len(saved.Results), err)
	}
	m := readManifest(t, dir)
	for _, line := range []string{
		"# Capture manifest: Roblox (alternatives)",
		"- Slug: `roblox`",
		"- Demo age: 9",
		"- Search query: safer app like Roblox for a 9 year old",
		"- T3.12 Tavily credits for this title: 1",
		"| " + fileAlternatives + " | " + base + "/search | 200 |",
		"| 1 | 3 results, 2 with raw_content |",
	} {
		if !strings.Contains(m, line) {
			t.Errorf("manifest missing %q:\n%s", line, m)
		}
	}
}

// The credit guard stops a title whose capture costs more than three
// credits, and the manifest keeps the spend.
func TestAlternativesCaptureGuard(t *testing.T) {
	_, _, dir, err := altRun(t, roblox, altAnswer(4, 1, 1))
	if err == nil || !strings.Contains(err.Error(), "over the guard") {
		t.Fatalf("err = %v, want the credit guard", err)
	}
	if m := readManifest(t, dir); !strings.Contains(m, "- T3.12 Tavily credits for this title: 4") {
		t.Errorf("manifest does not record the spend:\n%s", m)
	}
}

// A rerun replaces the answer and rebuilds the manifest.
func TestAlternativesCaptureRerunReplaces(t *testing.T) {
	d := roblox
	run := func(answer func(string, map[string]any) (int, string)) (*scripted, string) {
		t.Helper()
		s, _, dir, err := altRun(t, d, answer)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		return s, dir
	}
	_, dir := run(altAnswer(1, 3, 2))
	first := readManifest(t, dir)
	if !strings.Contains(first, "3 results, 2 with raw_content") {
		t.Errorf("first manifest:\n%s", first)
	}
	// A second run into the same directory, with a different answer.
	s := &scripted{answer: altAnswer(1, 1, 0)}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	c := &capturer{itunes: "http://unused.invalid", tavily: srv.URL, key: fakeKey, client: &http.Client{Timeout: 10 * time.Second}, now: time.Now}
	if err := c.captureAlternatives(d, dir); err != nil {
		t.Fatalf("second run: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, fileAlternatives))
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Results []json.RawMessage `json:"results"`
	}
	if json.Unmarshal(raw, &saved) != nil || len(saved.Results) != 1 {
		t.Errorf("rerun kept the old answer: %d results", len(saved.Results))
	}
	second := readManifest(t, dir)
	if n := strings.Count(second, "# Capture manifest: Roblox"); n != 1 {
		t.Errorf("manifest heading appears %d times, want 1", n)
	}
	if !strings.Contains(second, "1 results, 0 with raw_content") || strings.Contains(second, "3 results") {
		t.Errorf("manifest not rebuilt:\n%s", second)
	}
}

// An unknown slug makes no call, reports the missing age, and still writes
// the manifest.
func TestAlternativesCaptureUnknownSlug(t *testing.T) {
	s, _, dir, err := altRun(t, demo{Slug: "toca-life-world", Title: "Toca Life World"}, altAnswer(1, 1, 1))
	if err == nil || !strings.Contains(err.Error(), "no demo age") {
		t.Fatalf("err = %v, want the missing age", err)
	}
	if len(s.calls) != 0 {
		t.Errorf("made %d calls without an age, want 0", len(s.calls))
	}
	m := readManifest(t, dir)
	if !strings.Contains(m, "no demo age for slug") {
		t.Errorf("manifest does not record the failure:\n%s", m)
	}
}

// No file in the directory carries the key.
func TestAlternativesCaptureKeyNeverWritten(t *testing.T) {
	_, _, dir, err := altRun(t, roblox, altAnswer(1, 2, 1))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	files := 0
	walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
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
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if files != 2 { // the answer and the manifest
		t.Fatalf("only %d files written; the check would be vacuous", files)
	}
}

// The alternatives kind refuses to run beside another kind, and a refused
// combination makes no request.
func TestAlternativesRefusedWithOtherKinds(t *testing.T) {
	dir := storeDir(t)
	s, err := runT04aIn(t, dir, "alternatives,csm", []string{csmPage}, nil, 1)
	if err == nil || !strings.Contains(err.Error(), "runs alone") {
		t.Fatalf("err = %v, want the runs-alone refusal", err)
	}
	if len(s.calls) != 0 {
		t.Errorf("made %d calls for a refused combination, want 0", len(s.calls))
	}
	if _, err := parseOnly("alternatives"); err != nil {
		t.Errorf("parseOnly(alternatives): %v", err)
	}
}

func TestAltJobs(t *testing.T) {
	jobs, err := altJobs(true, "", "", "")
	if err != nil || len(jobs) != 3 {
		t.Fatalf("altJobs(all) = %d jobs, %v", len(jobs), err)
	}
	for i, want := range []struct{ slug, out string }{
		{"roblox", "testdata/alternatives/roblox"},
		{"minecraft", "testdata/alternatives/minecraft"},
		{"khan-academy-kids", "testdata/alternatives/khan-academy-kids"},
	} {
		if jobs[i].d.Slug != want.slug || jobs[i].out != want.out {
			t.Errorf("job %d = %s → %s, want %s → %s", i, jobs[i].d.Slug, jobs[i].out, want.slug, want.out)
		}
	}
	if jobs, err := altJobs(false, "minecraft", "Minecraft", "/tmp/alt"); err != nil || len(jobs) != 1 || jobs[0].out != "/tmp/alt" {
		t.Errorf("single job = %v, %v", jobs, err)
	}
	if _, err := altJobs(false, "", "", ""); err == nil {
		t.Error("no flags accepted")
	}
}
