package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// T3.12 sources: the alternatives search of each demo title, with page text.
// It is exactly the one search pipeline.FindAlternatives makes for a card
// (internal/pipeline/alternatives.go, alternativesSearch). The demo titles
// are iOS cards, and the pipeline never fills AppInfo.Genre, so the query's
// genre word is "app" for all of them.
//
// The kind runs only under -only alternatives, like storepage runs alone. It
// makes no App Store call and writes its own directory testdata/alternatives/<slug>,
// beside the title directories the pipeline tests read. Per title it makes
// one search, about one Tavily credit, and stops past maxT312Credits.

const (
	kindAlternatives = "alternatives"
	maxT312Credits   = 3 // per title; the run stops and reports past this
	fileAlternatives = "tavily-search-alternatives.json"
	altOutRoot       = "testdata/alternatives"
)

// job is one capture: the demo table entry and its output directory.
type job struct {
	d   demo
	out string
}

// altTitles are the T3.12 capture titles, with the demo age each is searched
// at: the three demo titles at their demo ages.
var altTitles = []struct {
	slug  string
	title string
	age   int
}{
	{"roblox", "Roblox", 9},
	{"minecraft", "Minecraft", 9},
	{"khan-academy-kids", "Khan Academy Kids", 5},
}

// altAge is a slug's demo age.
func altAge(slug string) (int, error) {
	for _, a := range altTitles {
		if a.slug == slug {
			return a.age, nil
		}
	}
	return 0, fmt.Errorf("no demo age for slug %q (want roblox, minecraft or khan-academy-kids)", slug)
}

// altSearchReq mirrors alternativesSearch for an iOS card whose listing has
// no genre (the pipeline never fills AppInfo.Genre): the genre word is
// "app", MaxResults and IncludeRaw are the search's own fields. If
// alternativesSearch changes, this mirror changes too.
func altSearchReq(title string, age int) searchReq {
	return searchReq{
		Query:      fmt.Sprintf("safer app like %s for a %d year old", title, age),
		MaxResults: 8,
		IncludeRaw: true,
	}
}

// altJobs builds the jobs of -only alternatives: with -all the three demo
// titles into testdata/alternatives/<slug>; or one title with -slug, -title
// and -out. The -dir flag does not apply: the alternatives captures live
// under their own root.
func altJobs(all bool, slug, title, out string) ([]job, error) {
	switch {
	case all:
		var jobs []job
		for _, a := range altTitles {
			jobs = append(jobs, job{demo{Slug: a.slug, Title: a.title}, filepath.Join(altOutRoot, a.slug)})
		}
		return jobs, nil
	case slug != "" && title != "" && out != "":
		return []job{{demo{Slug: slug, Title: title}, out}}, nil
	default:
		return nil, fmt.Errorf("use -all, or -slug, -title and -out together")
	}
}

// captureAlternatives runs the alternatives search for one title into out, a
// fresh directory this kind owns, and writes MANIFEST.md there. The manifest
// is written even when the call fails or the slug is unknown, so spent
// credits stay on the record.
func (c *capturer) captureAlternatives(d demo, out string) error {
	age, ageErr := altAge(d.Slug)
	r := &titleRun{c: c, out: out, guard: maxT312Credits}
	if ageErr == nil {
		ageErr = r.runAlternatives(d.Title, age)
	} else {
		r.note("%v", ageErr)
	}
	if werr := r.writeAltManifest(d, age, ageErr); werr != nil && ageErr == nil {
		return werr
	}
	return ageErr
}

// runAlternatives makes the one search and keeps its answer.
func (r *titleRun) runAlternatives(title string, age int) error {
	if err := os.MkdirAll(r.out, 0o755); err != nil {
		return err
	}
	// A rerun replaces the answer, so no stale page survives.
	if err := os.Remove(filepath.Join(r.out, fileAlternatives)); err != nil && !os.IsNotExist(err) {
		return err
	}
	resp, err := r.c.post(r.c.tavily+"/search", tavilySearch{searchReq: altSearchReq(title, age), IncludeUsage: true})
	if err != nil {
		return err
	}
	if err := r.keepTavily(fileAlternatives, resp); err != nil {
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
	if resp.Status != http.StatusOK {
		r.note("search answered HTTP %d", resp.Status)
	}
	return nil
}

// writeAltManifest writes the run's MANIFEST.md, in the T3.8a style: the
// query and its credits, one row per file, and the notes. A rerun rewrites
// the file.
func (r *titleRun) writeAltManifest(d demo, age int, runErr error) error {
	credits := strconv.FormatFloat(r.spent, 'f', -1, 64)
	if r.unreported > 0 {
		credits += fmt.Sprintf(" (plus %d call(s) with credits not reported)", r.unreported)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Capture manifest: %s (alternatives)\n\n", d.Title)
	b.WriteString("Written by `tools/capture -only alternatives` (T3.12). This directory holds the one Tavily search `pipeline.FindAlternatives` makes for this title, with page text. The Tavily key is read from `TAVILY_KEY` and is never recorded here.\n\n")
	fmt.Fprintf(&b, "- Slug: `%s`\n", d.Slug)
	fmt.Fprintf(&b, "- Title searched: %s\n", d.Title)
	if runErr != nil && age == 0 {
		fmt.Fprintf(&b, "- Demo age: not known (%v)\n", runErr)
	} else {
		fmt.Fprintf(&b, "- Demo age: %d\n", age)
		fmt.Fprintf(&b, "- Search query: %s\n", altSearchReq(d.Title, age).Query)
	}
	fmt.Fprintf(&b, "- T3.12 Tavily credits for this title: %s\n\n", credits)
	b.WriteString("## Files\n\n| File | Source | HTTP status | Fetched (UTC) | Tavily credits | Note |\n|---|---|---|---|---|---|\n")
	for _, f := range r.files {
		fmt.Fprintf(&b, "| %s | %s | %d | %s | %s | %s |\n",
			f.name, f.url, f.status, f.fetched.UTC().Format("2006-01-02T15:04:05Z"), f.credits, f.note)
	}
	b.WriteString("\n## Notes\n\n")
	if len(r.notes) == 0 {
		b.WriteString("- none\n")
	}
	for _, n := range r.notes {
		fmt.Fprintf(&b, "- %s\n", n)
	}
	if err := os.MkdirAll(r.out, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.out, "MANIFEST.md"), []byte(b.String()), 0o644)
}
