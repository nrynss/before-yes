package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// T0.4b sources: Tavily advanced extracts of the Common Sense user-review
// pages and of the first two Reddit parent-search hits. It runs only under
// -only advanced. Per title it makes exactly two POSTs to /extract, and no
// other request: no App Store call, no search.

const (
	kindAdvanced        = "advanced"
	maxT04bCredits      = 4 // per title; the run stops and reports past this
	t04bHeading         = "## T0.4b"
	fileCSMAdvanced     = "tavily-extract-csm-advanced.json"
	fileRedditAdvanced  = "tavily-extract-reddit-advanced.json"
	redditParentsFile   = "tavily-search-reddit-parents.json"
	redditAdvancedCount = 2
)

// csmAdvancedPages is the fixed table of Common Sense review pages, by slug.
// The pages are not searched for; T0.4a showed that search picks the wrong
// page for some titles.
var csmAdvancedPages = map[string]string{
	"roblox":            "https://www.commonsensemedia.org/website-reviews/roblox",
	"minecraft":         "https://www.commonsensemedia.org/app-reviews/minecraft",
	"khan-academy-kids": "https://www.commonsensemedia.org/app-reviews/khan-academy-kids",
	"toca-life-world":   "https://www.commonsensemedia.org/app-reviews/toca-life-world-build-stories",
}

// csmAdvancedURLs returns the adult and child user-review pages for a slug.
func csmAdvancedURLs(slug string) ([]string, error) {
	page, ok := csmAdvancedPages[slug]
	if !ok {
		return nil, fmt.Errorf("no Common Sense page in the fixed table for slug %q", slug)
	}
	return []string{page + "/user-reviews/adult", page + "/user-reviews/child"}, nil
}

// redditFirstURLs returns the URLs of the first n results of a title's saved
// parents search. It reads the file only; it makes no request.
func redditFirstURLs(dir string, n int) ([]string, error) {
	path := filepath.Join(dir, redditParentsFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w (the T0.4a reddit kind must have run first)", redditParentsFile, err)
	}
	hits := hitsOf(raw)
	if len(hits) < n {
		return nil, fmt.Errorf("%s has %d results, want at least %d", redditParentsFile, len(hits), n)
	}
	urls := make([]string, 0, n)
	for _, h := range hits[:n] {
		if h.URL == "" {
			return nil, fmt.Errorf("%s: a top result has no url", redditParentsFile)
		}
		urls = append(urls, h.URL)
	}
	return urls, nil
}

// captureT04b adds the advanced extracts to a title's directory and appends
// its "## T0.4b" section to MANIFEST.md. The manifest is written even when a
// call fails, so spent credits stay on the record.
func (c *capturer) captureT04b(d demo, out string) error {
	manifest := filepath.Join(out, "MANIFEST.md")
	old, err := os.ReadFile(manifest)
	if err != nil {
		return fmt.Errorf("no MANIFEST.md to append to (run the T0.4 capture first): %w", err)
	}
	csm, err := csmAdvancedURLs(d.Slug)
	if err != nil {
		return err
	}
	reddit, err := redditFirstURLs(out, redditAdvancedCount)
	if err != nil {
		return err
	}
	for _, name := range []string{fileCSMAdvanced, fileRedditAdvanced} {
		if err := os.Remove(filepath.Join(out, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	r := &titleRun{c: c, out: out, guard: maxT04bCredits}
	err = r.runT04b(csm, reddit)
	if werr := r.writeT04bManifest(manifest, string(old)); werr != nil && err == nil {
		err = werr
	}
	return err
}

// runT04b makes the two advanced extract calls. A call that returns a non-200
// status is recorded, and the second call still runs, so each source gets its
// own answer. The run then reports the failure.
func (r *titleRun) runT04b(csm, reddit []string) error {
	calls := []struct {
		name string
		urls []string
	}{
		{fileCSMAdvanced, csm},
		{fileRedditAdvanced, reddit},
	}
	var failed []string
	for _, call := range calls {
		resp, err := r.c.post(r.c.tavily+"/extract", tavilyExtract{URLs: call.urls, ExtractDepth: "advanced", Format: "text", IncludeUsage: true})
		if err != nil {
			return err
		}
		err = r.keepTavily(call.name, resp)
		if n := len(r.files); n > 0 && r.files[n-1].name == call.name {
			last := &r.files[n-1]
			last.url = strings.Join(call.urls, " ")
			last.note = t04bNote(resp.Body)
		}
		if err != nil {
			return err
		}
		if resp.Status != http.StatusOK {
			failed = append(failed, fmt.Sprintf("%s: HTTP %d", call.name, resp.Status))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%s", strings.Join(failed, "; "))
	}
	return nil
}

// t04bNote summarises one advanced extract: how many results carry text, and
// the failed_results, one per URL. Table-breaking characters are replaced.
func t04bNote(body []byte) string {
	withText := 0
	for _, h := range hitsOf(body) {
		if h.Raw != "" {
			withText++
		}
	}
	failed := failedOf(body)
	fail := "none"
	if len(failed) > 0 {
		fail = strings.Join(failed, "; ")
	}
	return cell(fmt.Sprintf("advanced; %d results with text; failed_results: %s", withText, fail))
}

// failedOf reads failed_results. Each entry is an object with url and error,
// or a bare string; both forms are accepted.
func failedOf(body []byte) []string {
	var res struct {
		Failed []json.RawMessage `json:"failed_results"`
	}
	if json.Unmarshal(body, &res) != nil {
		return nil
	}
	var out []string
	for _, raw := range res.Failed {
		var obj struct {
			URL   string `json:"url"`
			Error string `json:"error"`
		}
		var s string
		switch {
		case json.Unmarshal(raw, &obj) == nil && obj.URL != "":
			out = append(out, cell(obj.URL+" ("+obj.Error+")"))
		case json.Unmarshal(raw, &s) == nil:
			out = append(out, cell(s))
		default:
			out = append(out, cell(string(raw)))
		}
	}
	return out
}

// cell makes a value safe for one Markdown table cell.
func cell(s string) string {
	return strings.NewReplacer("|", "/", "\n", " ", "\r", " ").Replace(s)
}

// writeT04bManifest rewrites MANIFEST.md with its T0.4b section rebuilt from
// this run. Everything else in the file is kept.
func (r *titleRun) writeT04bManifest(path, old string) error {
	credits := strconv.FormatFloat(r.spent, 'f', -1, 64)
	if r.unreported > 0 {
		credits += fmt.Sprintf(" (plus %d call(s) with credits not reported)", r.unreported)
	}
	var b strings.Builder
	b.WriteString(t04bHeading + "\n\n")
	b.WriteString("Written by `tools/capture -only advanced`. A run rewrites this section. The Tavily key is never recorded here.\n\n")
	fmt.Fprintf(&b, "- T0.4b Tavily credits for this title: %s\n\n", credits)
	b.WriteString("### Files\n\n| File | Source | HTTP status | Fetched (UTC) | Tavily credits | Note |\n|---|---|---|---|---|---|\n")
	for _, f := range r.files {
		fmt.Fprintf(&b, "| %s | %s | %d | %s | %s | %s |\n",
			f.name, f.url, f.status, f.fetched.UTC().Format("2006-01-02T15:04:05Z"), f.credits, f.note)
	}
	b.WriteString("\n### Notes\n\n")
	if len(r.notes) == 0 {
		b.WriteString("- none\n")
	}
	for _, n := range r.notes {
		fmt.Fprintf(&b, "- %s\n", n)
	}

	base := strings.TrimRight(withoutSection(old, t04bHeading), "\n")
	return os.WriteFile(path, []byte(base+"\n\n"+b.String()), 0o644)
}
