package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// T3.8a sources: the App Store web page of each title, extracted with Tavily.
// The page URL is the trackViewUrl of the title's hit in the saved App Store
// search, so this kind makes no App Store request. It runs only under
// -only storepage. Per title it makes one basic extract, and one advanced
// extract only when the basic answer shows no review text.

const (
	kindStorePage      = "storepage"
	depthAuto          = "auto"
	depthBasic         = "basic"
	depthAdvanced      = "advanced"
	maxT38aCredits     = 3 // per title; the run stops and reports past this
	t38aHeading        = "## T3.8a"
	fileStorePage      = "tavily-extract-storepage.json"
	fileStorePageBasic = "tavily-extract-storepage-basic.json"
	fileAppStoreSearch = "appstore-search.json"
	minReviewLine      = 60 // characters; shorter lines are headings, counts or labels
)

// storeReviewsHeading is the App Store page's own section heading. Text under
// it is the only place review text is looked for.
var storeReviewsHeading = regexp.MustCompile(`(?i)ratings (and|&) reviews`)

// parseDepth validates the -depth flag for -only storepage.
func parseDepth(s string) (string, error) {
	switch s {
	case depthAuto, depthBasic, depthAdvanced:
		return s, nil
	}
	return "", fmt.Errorf("-depth: unknown value %q (want auto, basic or advanced)", s)
}

// storePageURL returns the trackViewUrl of the hit whose trackId is id, from
// the saved App Store search. It reads the file only.
func storePageURL(dir string, id int64) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, fileAppStoreSearch))
	if err != nil {
		return "", fmt.Errorf("%s: %w (the T0.4 capture must have run first)", fileAppStoreSearch, err)
	}
	var search struct {
		Results []struct {
			ID  int64  `json:"trackId"`
			URL string `json:"trackViewUrl"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &search); err != nil {
		return "", fmt.Errorf("%s: %w", fileAppStoreSearch, err)
	}
	for _, r := range search.Results {
		if r.ID != id {
			continue
		}
		if r.URL == "" {
			return "", fmt.Errorf("%s: trackId %d has no trackViewUrl", fileAppStoreSearch, id)
		}
		return r.URL, nil
	}
	return "", fmt.Errorf("%s: no hit with trackId %d", fileAppStoreSearch, id)
}

// storePageHasReviews reports whether an extract answer shows review text: a
// line of at least minReviewLine characters after the page's "Ratings and
// Reviews" heading. It is a budget gate for the advanced depth, not the
// judge of usability; the inspection in the T3.8a report makes that call.
func storePageHasReviews(body []byte) bool {
	for _, h := range hitsOf(body) {
		loc := storeReviewsHeading.FindStringIndex(h.Raw)
		if loc == nil {
			continue
		}
		for _, line := range strings.Split(h.Raw[loc[1]:], "\n") {
			if len(strings.TrimSpace(line)) >= minReviewLine {
				return true
			}
		}
	}
	return false
}

// captureStorePage adds the store-page extract to a title's directory and
// rewrites its "## T3.8a" section of MANIFEST.md. The manifest is written even
// when a call fails, so spent credits stay on the record.
func (c *capturer) captureStorePage(d demo, out string) error {
	manifest := filepath.Join(out, "MANIFEST.md")
	old, err := os.ReadFile(manifest)
	if err != nil {
		return fmt.Errorf("no MANIFEST.md to append to (run the T0.4 capture first): %w", err)
	}
	pageURL, err := storePageURL(out, d.ID)
	if err != nil {
		return err
	}
	depth := c.storeDepth
	if depth == "" {
		depth = depthAuto
	}
	for _, name := range []string{fileStorePage, fileStorePageBasic} {
		if err := os.Remove(filepath.Join(out, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	r := &titleRun{c: c, out: out, guard: maxT38aCredits}
	err = r.runStorePage(pageURL, depth)
	if werr := r.writeStorePageManifest(manifest, string(old)); werr != nil && err == nil {
		err = werr
	}
	return err
}

// runStorePage makes the extract calls for one page. In auto mode a basic
// answer with no review text is kept as the basic file, and advanced is tried
// next; the advanced answer is the final file either way.
func (r *titleRun) runStorePage(pageURL, depth string) error {
	if depth != depthAdvanced {
		resp, err := r.c.post(r.c.tavily+"/extract", tavilyExtract{URLs: []string{pageURL}, ExtractDepth: "basic", Format: "text", IncludeUsage: true})
		if err != nil {
			return err
		}
		if depth == depthBasic || resp.Status != http.StatusOK || storePageHasReviews(resp.Body) {
			return r.keepStorePage(fileStorePage, resp, pageURL, depthBasic)
		}
		r.note("basic extract shows no review text under the Ratings and Reviews heading, so advanced was run")
		if err := r.keepStorePage(fileStorePageBasic, resp, pageURL, depthBasic); err != nil {
			return err
		}
	}
	resp, err := r.c.post(r.c.tavily+"/extract", tavilyExtract{URLs: []string{pageURL}, ExtractDepth: "advanced", Format: "text", IncludeUsage: true})
	if err != nil {
		return err
	}
	return r.keepStorePage(fileStorePage, resp, pageURL, depthAdvanced)
}

// keepStorePage saves one extract call, records its page URL and a note, and
// applies the credit guard through keepTavily.
func (r *titleRun) keepStorePage(name string, resp response, pageURL, mode string) error {
	err := r.keepTavily(name, resp)
	if n := len(r.files); n > 0 && r.files[n-1].name == name {
		last := &r.files[n-1]
		last.url = pageURL
		last.note = storePageNote(mode, resp.Body)
	}
	return err
}

// storePageNote summarises one call: the depth, how many results carry text,
// whether the text has review text, and the failed_results.
func storePageNote(mode string, body []byte) string {
	withText := 0
	for _, h := range hitsOf(body) {
		if h.Raw != "" {
			withText++
		}
	}
	reviews := "no"
	if storePageHasReviews(body) {
		reviews = "yes"
	}
	fail := "none"
	if failed := failedOf(body); len(failed) > 0 {
		fail = strings.Join(failed, "; ")
	}
	return cell(fmt.Sprintf("%s; %d results with text; review text: %s; failed_results: %s", mode, withText, reviews, fail))
}

// writeStorePageManifest rewrites the "## T3.8a" section and keeps the rest of
// the manifest, including the T0.4 and T0.4b text.
func (r *titleRun) writeStorePageManifest(path, old string) error {
	credits := strconv.FormatFloat(r.spent, 'f', -1, 64)
	if r.unreported > 0 {
		credits += fmt.Sprintf(" (plus %d call(s) with credits not reported)", r.unreported)
	}
	var b strings.Builder
	b.WriteString(t38aHeading + "\n\n")
	b.WriteString("Written by `tools/capture -only storepage`. A run rewrites this section. The Tavily key is never recorded here.\n\n")
	fmt.Fprintf(&b, "- T3.8a Tavily credits for this title: %s\n\n", credits)
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

	base := strings.TrimRight(withoutSection(old, t38aHeading), "\n")
	return os.WriteFile(path, []byte(base+"\n\n"+b.String()), 0o644)
}
