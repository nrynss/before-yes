package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// T0.4a sources: Common Sense Media parent and kid reviews, and two wider
// Reddit searches. They run only under -only, so the App Store and the first
// Tavily kinds are never called in this mode.

const (
	kindCSM        = "csm"
	kindReddit     = "reddit"
	maxT04aCredits = 6 // per title; the run stops and reports past this
	t04aHeading    = "## T0.4a"
	csmHost        = "commonsensemedia.org"
)

// kindFiles are the files each kind writes. A run removes and rewrites the
// files of the kinds it runs, and leaves the others alone.
var kindFiles = map[string][]string{
	kindCSM: {
		"tavily-search-csm.json",
		"tavily-extract-csm.json",
		"tavily-extract-csm-retry.json",
	},
	kindReddit: {
		"tavily-search-reddit-parents.json",
		"tavily-search-reddit-community.json",
	},
}

// csmCategories are the review path prefixes. A page under one of them, with
// the title's slug right after it, is the title's main review page.
var csmCategories = []string{"/app-reviews/", "/game-reviews/", "/website-reviews/"}

// parseOnly reads the -only list. An empty flag means no filter (nil).
func parseOnly(s string) (map[string]bool, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	kinds := map[string]bool{}
	for _, k := range strings.Split(s, ",") {
		k = strings.TrimSpace(k)
		switch k {
		case kindCSM, kindReddit, kindAdvanced:
			kinds[k] = true
		default:
			return nil, fmt.Errorf("-only: unknown kind %q (want csm, reddit or advanced)", k)
		}
	}
	return kinds, nil
}

// captureT04a adds the requested T0.4a kinds to a title's directory. It needs
// the MANIFEST.md from the T0.4 capture. It rewrites only the "## T0.4a"
// section, and only the rows of the kinds it ran. The credits of rows it
// replaces stay in the section's total.
func (c *capturer) captureT04a(d demo, out string, only map[string]bool) error {
	if only[kindAdvanced] {
		if len(only) > 1 {
			return fmt.Errorf("-only advanced runs alone; it cannot be combined with csm or reddit")
		}
		return c.captureT04b(d, out)
	}
	manifest := filepath.Join(out, "MANIFEST.md")
	old, err := os.ReadFile(manifest)
	if err != nil {
		return fmt.Errorf("no MANIFEST.md to append to (run the T0.4 capture first): %w", err)
	}
	drop := map[string]bool{}
	for kind := range only {
		for _, name := range kindFiles[kind] {
			drop[name] = true
			if err := os.Remove(filepath.Join(out, name)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	r := &titleRun{c: c, out: out, guard: maxT04aCredits}
	err = r.runT04a(d, only)
	if werr := r.writeT04aManifest(manifest, string(old), drop); werr != nil && err == nil {
		err = werr
	}
	return err
}

// runT04a runs the kinds in a fixed order, whatever order -only lists them.
func (r *titleRun) runT04a(d demo, only map[string]bool) error {
	if only[kindCSM] {
		if err := r.csm(d); err != nil {
			return err
		}
	}
	if only[kindReddit] {
		if err := r.reddit(d); err != nil {
			return err
		}
	}
	return nil
}

// csm searches for the title's Common Sense review page, then extracts its
// adult and child user-review pages. If either extract URL has no text, it
// makes one retry with a /user-reviews/ URL from the search hits.
func (r *titleRun) csm(d demo) error {
	r.csmRan = true
	search := searchReq{
		Query:          d.Title + " Common Sense Media review",
		MaxResults:     5,
		IncludeDomains: []string{csmHost},
	}
	resp, err := r.c.post(r.c.tavily+"/search", tavilySearch{searchReq: search, IncludeUsage: true})
	if err != nil {
		return err
	}
	if err := r.keepTavily("tavily-search-csm.json", resp); err != nil {
		return err
	}
	hits := hitsOf(resp.Body)
	if resp.Status != http.StatusOK {
		r.note("csm search: HTTP %d, no extract made", resp.Status)
		return nil
	}
	urls := make([]string, 0, len(hits))
	for _, h := range hits {
		urls = append(urls, h.URL)
	}
	slug := titleSlug(d.Title)
	page, near := pickReviewPage(urls, slug)
	if len(near) > 0 {
		r.note("review pages for other titles, not used: %s", strings.Join(near, ", "))
	}
	if page == "" {
		r.note("no Common Sense Media review page for slug %q among %d search hits; csm extract skipped", slug, len(hits))
		return nil
	}
	r.csmPage = page
	r.files[len(r.files)-1].note = "review page: " + page

	requested := []string{page + "/user-reviews/adult", page + "/user-reviews/child"}
	resp, err = r.c.post(r.c.tavily+"/extract", tavilyExtract{URLs: requested, ExtractDepth: "basic", Format: "text", IncludeUsage: true})
	if err != nil {
		return err
	}
	if err := r.keepTavily("tavily-extract-csm.json", resp); err != nil {
		return err
	}
	r.files[len(r.files)-1].note = "urls: " + strings.Join(requested, " ")
	failed := missingText(requested, resp)
	if len(failed) == 0 {
		return nil
	}
	r.note("csm extract returned no text for: %s", strings.Join(failed, ", "))

	retry := ""
	for _, h := range hits {
		if strings.Contains(h.URL, "/user-reviews/") && !containsString(requested, h.URL) {
			retry = h.URL
			break
		}
	}
	if retry == "" {
		r.note("no other /user-reviews/ search hit, so no retry was made")
		return nil
	}
	resp, err = r.c.post(r.c.tavily+"/extract", tavilyExtract{URLs: []string{retry}, ExtractDepth: "basic", Format: "text", IncludeUsage: true})
	if err != nil {
		return err
	}
	if err := r.keepTavily("tavily-extract-csm-retry.json", resp); err != nil {
		return err
	}
	r.files[len(r.files)-1].note = "the one retry, urls: " + retry
	if len(missingText([]string{retry}, resp)) == 0 {
		r.note("retry with %s returned text", retry)
	} else {
		r.note("retry with %s also returned no text", retry)
	}
	return nil
}

// reddit runs the two Reddit searches: parents' experiences, and the title's
// own community. Each keeps raw content, and the note counts the threads.
func (r *titleRun) reddit(d demo) error {
	searches := []struct {
		file  string
		query string
	}{
		{"tavily-search-reddit-parents.json", d.Title + " parents kids safety experience"},
		{"tavily-search-reddit-community.json", d.Title + " chat scams moderation players"},
	}
	for _, s := range searches {
		search := searchReq{
			Query:          s.query,
			MaxResults:     8,
			IncludeDomains: []string{"reddit.com"},
			IncludeRaw:     true,
		}
		resp, err := r.c.post(r.c.tavily+"/search", tavilySearch{searchReq: search, IncludeUsage: true})
		if err != nil {
			return err
		}
		if err := r.keepTavily(s.file, resp); err != nil {
			return err
		}
		hits := hitsOf(resp.Body)
		withRaw := 0
		for _, h := range hits {
			if h.Raw != "" {
				withRaw++
			}
		}
		r.files[len(r.files)-1].note = fmt.Sprintf("%d results, %d with raw_content", len(hits), withRaw)
	}
	return nil
}

// titleSlug is the review-page slug for a title: its words, lowercased and
// joined with hyphens, as "Khan Academy Kids" gives "khan-academy-kids".
func titleSlug(title string) string {
	var words []string
	for _, w := range strings.Fields(strings.ToLower(title)) {
		var b strings.Builder
		for _, c := range w {
			if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
				b.WriteRune(c)
			}
		}
		if b.Len() > 0 {
			words = append(words, b.String())
		}
	}
	return strings.Join(words, "-")
}

// pickReviewPage returns the title's own Common Sense review page from the
// search hits. A page qualifies when it is on commonsensemedia.org, sits under
// a review category with the slug right after it, and is not a user-reviews
// page. It returns the page without a trailing slash, or "". Review pages for
// other titles are returned as near misses, so the manifest can show them.
func pickReviewPage(urls []string, slug string) (string, []string) {
	page := ""
	var near []string
	for _, u := range urls {
		pu, err := url.Parse(u)
		if err != nil || pu.Host == "" || slug == "" {
			continue
		}
		host := strings.ToLower(pu.Host)
		if host != csmHost && !strings.HasSuffix(host, "."+csmHost) {
			continue
		}
		p := pu.Path
		if strings.Contains(p, "/user-reviews/") {
			continue
		}
		for _, cat := range csmCategories {
			i := strings.Index(p, cat)
			if i < 0 {
				continue
			}
			rest := strings.Trim(p[i+len(cat):], "/")
			if rest == "" {
				break
			}
			seg := strings.Split(rest, "/")[0]
			if seg != slug {
				near = append(near, u)
				break
			}
			if page == "" {
				end := i + len(cat) + len(seg)
				page = pu.Scheme + "://" + pu.Host + p[:end]
			}
			break
		}
	}
	return page, near
}

// hit is one Tavily search or extract result, with only the fields read here.
type hit struct {
	URL string `json:"url"`
	Raw string `json:"raw_content"`
}

// hitsOf reads a Tavily search response's results. A body that is not JSON
// has no hits.
func hitsOf(body []byte) []hit {
	var res struct {
		Results []hit `json:"results"`
	}
	if json.Unmarshal(body, &res) != nil {
		return nil
	}
	return res.Results
}

// missingText returns the requested extract URLs that did not come back with
// text. A non-200 response counts every URL as missing.
func missingText(requested []string, resp response) []string {
	ok := map[string]bool{}
	if resp.Status == http.StatusOK {
		for _, h := range hitsOf(resp.Body) {
			if h.Raw != "" {
				ok[strings.TrimSuffix(h.URL, "/")] = true
			}
		}
	}
	var missing []string
	for _, u := range requested {
		if !ok[strings.TrimSuffix(u, "/")] {
			missing = append(missing, u)
		}
	}
	return missing
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// t04aPrior is what the existing T0.4a section says, read before a rewrite.
type t04aPrior struct {
	rows       []string // table rows kept, one per file
	page       string   // the Common Sense review page line
	replaced   float64  // credits of rows removed by earlier runs
	kept       float64  // credits of the kept rows
	unreported int      // kept rows whose credits were not reported
}

// readPrior reads the T0.4a section of old, keeping the rows of files in drop
// out of the rows and adding their credits to replaced.
func readPrior(old string, drop map[string]bool) t04aPrior {
	var p t04aPrior
	for _, line := range sectionLines(old, t04aHeading) {
		switch {
		case strings.HasPrefix(line, "| tavily-"):
			cells := strings.Split(line, "|")
			if len(cells) < 7 {
				continue
			}
			credit := strings.TrimSpace(cells[5])
			if drop[strings.TrimSpace(cells[1])] {
				if v, err := strconv.ParseFloat(credit, 64); err == nil {
					p.replaced += v
				}
				continue
			}
			p.rows = append(p.rows, line)
			if v, err := strconv.ParseFloat(credit, 64); err == nil {
				p.kept += v
			} else if credit == "not reported" {
				p.unreported++
			}
		case strings.HasPrefix(line, "- Common Sense review page: "):
			p.page = strings.TrimPrefix(line, "- Common Sense review page: ")
		case strings.HasPrefix(line, "- Credits on calls replaced by later runs: "):
			f := strings.Fields(strings.TrimPrefix(line, "- Credits on calls replaced by later runs: "))
			if len(f) > 0 {
				if v, err := strconv.ParseFloat(f[0], 64); err == nil {
					p.replaced += v
				}
			}
		}
	}
	return p
}

// writeT04aManifest rewrites MANIFEST.md with its T0.4a section rebuilt from
// the kept rows, this run's rows, and the totals. Everything else is kept.
func (r *titleRun) writeT04aManifest(path, old string, drop map[string]bool) error {
	prior := readPrior(old, drop)

	page := prior.page
	if r.csmRan {
		page = r.csmPage
	}
	if page == "" {
		page = "none found"
	}
	// Replaced credits are the earlier rows this run removed, kept in the
	// total so that spend is not lost from the record.
	replaced := prior.replaced

	rows := append([]string(nil), prior.rows...)
	for _, f := range r.files {
		rows = append(rows, fmt.Sprintf("| %s | %s | %d | %s | %s | %s |",
			f.name, f.url, f.status, f.fetched.UTC().Format("2006-01-02T15:04:05Z"), f.credits, f.note))
	}
	sort.Strings(rows)

	total := prior.kept + r.spent
	unreported := prior.unreported + r.unreported

	var b strings.Builder
	b.WriteString(t04aHeading + "\n\n")
	b.WriteString("Written by `tools/capture -only`. A run rewrites the files of the kinds it ran and keeps the other rows. The credits of replaced calls stay in the total. The Tavily key is never recorded here.\n\n")
	fmt.Fprintf(&b, "- Common Sense review page: %s\n", page)
	fmt.Fprintf(&b, "- Credits on calls replaced by later runs: %s\n", strconv.FormatFloat(replaced, 'f', -1, 64))
	credits := strconv.FormatFloat(total+replaced, 'f', -1, 64)
	if unreported > 0 {
		credits += fmt.Sprintf(" (plus %d call(s) with credits not reported)", unreported)
	}
	fmt.Fprintf(&b, "- T0.4a Tavily credits for this title: %s\n\n", credits)

	b.WriteString("### Files\n\n| File | Source | HTTP status | Fetched (UTC) | Tavily credits | Note |\n|---|---|---|---|---|---|\n")
	for _, row := range rows {
		b.WriteString(row + "\n")
	}
	b.WriteString("\n### Notes\n\n")
	if len(r.notes) == 0 {
		b.WriteString("- none\n")
	}
	for _, n := range r.notes {
		fmt.Fprintf(&b, "- %s\n", n)
	}

	base := strings.TrimRight(withoutSection(old, t04aHeading), "\n")
	return os.WriteFile(path, []byte(base+"\n\n"+b.String()), 0o644)
}

// sectionLines returns the lines under a heading, up to the next "## " heading.
func sectionLines(doc, heading string) []string {
	var out []string
	in := false
	for _, line := range strings.Split(doc, "\n") {
		if line == heading {
			in = true
			continue
		}
		if in && strings.HasPrefix(line, "## ") {
			break
		}
		if in {
			out = append(out, line)
		}
	}
	return out
}

// withoutSection drops the block that starts at the heading line and runs to
// the next "## " heading or the end of the document.
func withoutSection(doc, heading string) string {
	var keep []string
	skipping := false
	for _, line := range strings.Split(doc, "\n") {
		if line == heading {
			skipping = true
			continue
		}
		if skipping && strings.HasPrefix(line, "## ") {
			skipping = false
		}
		if !skipping {
			keep = append(keep, line)
		}
	}
	return strings.Join(keep, "\n")
}
