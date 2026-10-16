// Command capture records the public material Before Yes reads for one
// title: the App Store search and review feed, plus the Tavily searches and
// extract that the gatherer plans. It is run by hand, never by CI.
//
// Usage, from the repository root:
//
//	set -a; . /path/to/before-yes/.env; set +a
//	go run ./tools/capture -slug roblox -title "Roblox" -id 431946152 -out testdata/titles/roblox
//	go run ./tools/capture -all
//	go run ./tools/capture -all -only csm,reddit
//	go run ./tools/capture -all -only controls
//	go run ./tools/capture -all -only storepage -depth auto
//
// -only runs just the named kinds: csm and reddit (T0.4a), controls
// (plan-6's parental-controls search), advanced (T0.4b), storepage (T3.8a),
// or alternatives (T3.12), which each run alone. Storepage and alternatives
// run with no App Store call: storepage reads each title's trackViewUrl from
// the saved appstore-search.json; alternatives makes the one search
// pipeline.FindAlternatives makes and writes testdata/alternatives/<slug>,
// its own directory with its own MANIFEST.md, instead of the title
// directories. Neither calls the first Tavily kinds, and both add to
// (storepage) or write (alternatives) a MANIFEST.md.
// -depth sets the storepage extract: auto (basic, then
// advanced when the basic answer shows no review text), basic or advanced.
//
// The Tavily key is read from TAVILY_KEY and sent only in the Authorization
// header. It is never written to a file, printed, or put in a URL.
//
// The searches come from pipeline.Plan, so a plan change reaches the
// captures without a second copy. The request shapes mirror
// internal/tavily; if those change, this program and its test change too.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nrynss/before-yes/internal/pipeline"
)

const (
	itunesBase      = "https://itunes.apple.com"
	tavilyBase      = "https://api.tavily.com"
	keyEnv          = "TAVILY_KEY"
	maxReviewPages  = 10
	maxTitleCredits = 10 // stop and report if one title costs more than this
)

// demo is one title in the demo table.
type demo struct {
	Slug  string
	Title string
	ID    int64
}

var demoTitles = []demo{
	{Slug: "khan-academy-kids", Title: "Khan Academy Kids", ID: 1378467217},
	{Slug: "minecraft", Title: "Minecraft", ID: 479516143},
	{Slug: "roblox", Title: "Roblox", ID: 431946152},
	{Slug: "toca-life-world", Title: "Toca Life World", ID: 1208138685},
}

func main() {
	slug := flag.String("slug", "", "slug for one title, such as roblox")
	title := flag.String("title", "", "title as written in the demo table")
	id := flag.Int64("id", 0, "iOS trackId, used for the review feed")
	out := flag.String("out", "", "output directory for one title")
	all := flag.Bool("all", false, "capture the four demo titles into <dir>/<slug>")
	dir := flag.String("dir", "testdata/titles", "root directory for -all")
	pause := flag.Duration("pause", 500*time.Millisecond, "wait between App Store requests")
	only := flag.String("only", "", "run only these kinds, comma-separated: csm, reddit, controls (the plan-6 controls search), advanced (T0.4b), storepage (T3.8a) or alternatives (T3.12); advanced, storepage and alternatives run alone")
	depth := flag.String("depth", depthAuto, "for -only storepage: auto (basic, then advanced if no review text), basic or advanced")
	flag.Parse()

	kinds, err := parseOnly(*only)
	if err != nil {
		fail(err.Error())
	}
	storeDepth, err := parseDepth(*depth)
	if err != nil {
		fail(err.Error())
	}

	var jobs []job
	switch {
	case kinds != nil && kinds[kindAlternatives]:
		js, jerr := altJobs(*all, *slug, *title, *out)
		if jerr != nil {
			fail(jerr.Error())
		}
		jobs = js
	case *all:
		for _, d := range demoTitles {
			jobs = append(jobs, job{d, filepath.Join(*dir, d.Slug)})
		}
	case *slug != "" && *title != "" && *id != 0 && *out != "":
		jobs = []job{{demo{Slug: *slug, Title: *title, ID: *id}, *out}}
	default:
		fail("use -slug, -title, -id and -out together, or -all")
	}

	key := os.Getenv(keyEnv)
	if key == "" {
		fail(keyEnv + " is not set; load the .env file first (see the usage note)")
	}
	c := &capturer{
		itunes:     itunesBase,
		tavily:     tavilyBase,
		key:        key,
		client:     &http.Client{Timeout: 60 * time.Second},
		pause:      *pause,
		now:        time.Now,
		storeDepth: storeDepth,
	}
	for _, j := range jobs {
		if kinds != nil {
			if err := c.captureT04a(j.d, j.out, kinds); err != nil {
				fail(fmt.Sprintf("%s: %v", j.d.Slug, err))
			}
			fmt.Printf("%s: -only %s captured into %s\n", j.d.Slug, *only, j.out)
			continue
		}
		if err := c.capture(j.d, j.out); err != nil {
			fail(fmt.Sprintf("%s: %v", j.d.Slug, err))
		}
		fmt.Printf("%s: captured into %s\n", j.d.Slug, j.out)
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}

// capturer holds the endpoints and the key. Tests point the bases at httptest
// servers.
type capturer struct {
	itunes string
	tavily string
	key    string
	client *http.Client
	pause  time.Duration
	now    func() time.Time
	// storeDepth is the -depth flag for -only storepage; "" means auto.
	storeDepth string
}

// capture writes one title's fixtures and MANIFEST.md. The manifest is written
// even when a step fails, so spent credits are never lost from the record.
func (c *capturer) capture(d demo, out string) error {
	r := &titleRun{c: c, out: out, guard: maxTitleCredits}
	err := r.run(d)
	if werr := r.writeManifest(d); werr != nil && err == nil {
		err = werr
	}
	return err
}

// fileEntry is one saved file, for the manifest.
type fileEntry struct {
	name    string
	url     string
	status  int
	fetched time.Time
	credits string // Tavily calls only
	note    string
}

// titleRun is the state of one title's capture.
type titleRun struct {
	c          *capturer
	out        string
	files      []fileEntry
	notes      []string
	spent      float64 // reported Tavily credits so far
	unreported int     // Tavily calls with no usage.credits
	guard      float64 // stop the title once spent passes this
	host       string
	trackName  string
	reviews    int
	csmRan     bool   // T0.4a: the csm kind ran in this run
	csmPage    string // T0.4a: the Common Sense review page, if one was found
}

func (r *titleRun) note(format string, args ...any) {
	r.notes = append(r.notes, fmt.Sprintf(format, args...))
}

func (r *titleRun) run(d demo) error {
	if err := os.MkdirAll(r.out, 0o755); err != nil {
		return err
	}
	// A rerun must not leave a stale page behind if the feed now ends sooner.
	old, _ := filepath.Glob(filepath.Join(r.out, "reviews-page-*.json"))
	for _, f := range old {
		if err := os.Remove(f); err != nil {
			return err
		}
	}

	// 1. App Store search, with the same query as appstore.Find(title, 5).
	q := url.Values{"term": {d.Title}, "entity": {"software"}, "country": {"us"}, "limit": {"5"}}
	resp, err := r.c.get(r.c.itunes + "/search?" + q.Encode())
	if err != nil {
		return err
	}
	if err := r.keep("appstore-search.json", resp, ""); err != nil {
		return err
	}
	if resp.Status != http.StatusOK {
		return fmt.Errorf("appstore-search.json: HTTP %d", resp.Status)
	}
	var search struct {
		Results []app `json:"results"`
	}
	if err := json.Unmarshal(resp.Body, &search); err != nil {
		return fmt.Errorf("appstore-search.json: %w", err)
	}
	for _, a := range search.Results {
		if a.ID == d.ID {
			r.trackName = a.Name
		}
	}
	host, picked := officialHost(search.Results, d.Title)
	r.host = host
	switch {
	case picked == nil:
		r.note("no App Store hit's trackName starts with %q, so policy and terms run without include_domains", d.Title)
	case picked.ID != d.ID:
		r.note("the title match is trackId %d (%q), not the table's %d; reviews use the table's id", picked.ID, picked.Name, d.ID)
	}
	if host == "" && picked != nil {
		r.note("the matched hit %q has no usable sellerUrl host, so policy and terms run without include_domains", picked.Name)
	}

	// 2. Review feed, every page until the first page with no entries, a
	// non-200, or the page limit. A stopping page is not saved.
	for p := 1; p <= maxReviewPages; p++ {
		time.Sleep(r.c.pause)
		resp, err := r.c.get(feedURL(r.c.itunes, d.ID, p))
		if err != nil {
			r.note("reviews stopped before page %d: %v", p, err)
			break
		}
		if resp.Status != http.StatusOK {
			r.note("reviews stopped at page %d: HTTP %d (page not saved)", p, resp.Status)
			break
		}
		var feed struct {
			Feed struct {
				Entry json.RawMessage `json:"entry"`
			} `json:"feed"`
		}
		if err := json.Unmarshal(resp.Body, &feed); err != nil {
			r.note("reviews stopped at page %d: not JSON (page not saved)", p)
			break
		}
		entries, reviews := countEntries(feed.Feed.Entry)
		if entries == 0 {
			r.note("reviews stopped at page %d: no feed.entry (page not saved)", p)
			break
		}
		if err := r.keep(fmt.Sprintf("reviews-page-%02d.json", p), resp, fmt.Sprintf("%d entries, %d reviews", entries, reviews)); err != nil {
			return err
		}
		r.reviews += reviews
		if p == maxReviewPages {
			r.note("reviews stopped at the feed's page limit of %d", maxReviewPages)
		}
	}

	// 3. Tavily searches, in plan() order. The top hit of policy and terms is
	// the extract target.
	top := map[string]string{}
	for _, qu := range plan(d.Title, host) {
		resp, err := r.c.post(r.c.tavily+"/search", tavilySearch{searchReq: qu.Search, IncludeUsage: true})
		if err != nil {
			return err
		}
		name := "tavily-search-" + qu.Kind + ".json"
		if err := r.keepTavily(name, resp); err != nil {
			return err
		}
		if resp.Status != http.StatusOK {
			return fmt.Errorf("%s: HTTP %d", name, resp.Status)
		}
		var res struct {
			Results []struct {
				URL string `json:"url"`
			} `json:"results"`
		}
		if err := json.Unmarshal(resp.Body, &res); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if len(res.Results) == 0 {
			r.note("%s search returned no results", qu.Kind)
			continue
		}
		if qu.Extract {
			top[qu.Kind] = res.Results[0].URL
		}
	}

	// 4. Extract the top policy and terms pages, skipping any kind with no hit.
	var urls []string
	for _, k := range []string{"policy", "terms"} {
		if u := top[k]; u != "" {
			urls = append(urls, u)
		}
	}
	if len(urls) == 0 {
		r.note("no policy or terms hit, so no extract call was made")
		return nil
	}
	resp, err = r.c.post(r.c.tavily+"/extract", tavilyExtract{URLs: urls, ExtractDepth: "basic", Format: "text", IncludeUsage: true})
	if err != nil {
		return err
	}
	if err := r.keepTavily("tavily-extract.json", resp); err != nil {
		return err
	}
	if resp.Status != http.StatusOK {
		return fmt.Errorf("tavily-extract.json: HTTP %d", resp.Status)
	}
	return nil
}

// keep saves one response, pretty-printed when it is JSON, and records it.
// Content is not altered: indenting only changes whitespace.
func (r *titleRun) keep(name string, resp response, note string) error {
	data := resp.Body
	var pretty bytes.Buffer
	if json.Indent(&pretty, resp.Body, "", "  ") == nil {
		data = append(pretty.Bytes(), '\n')
	}
	if err := os.WriteFile(filepath.Join(r.out, name), data, 0o644); err != nil {
		return err
	}
	r.files = append(r.files, fileEntry{name: name, url: resp.URL, status: resp.Status, fetched: resp.At, note: note})
	return nil
}

// keepTavily saves a Tavily response, records its credits, and refuses to go
// on once the title passes the guard.
func (r *titleRun) keepTavily(name string, resp response) error {
	credits, reported := creditsOf(resp.Body)
	if err := r.keep(name, resp, ""); err != nil {
		return err
	}
	last := &r.files[len(r.files)-1]
	if reported {
		last.credits = strconv.FormatFloat(credits, 'f', -1, 64)
		r.spent += credits
	} else {
		last.credits = "not reported"
		r.unreported++
	}
	if r.spent > r.guard {
		return fmt.Errorf("spent %.1f Tavily credits, over the guard of %g; stopped", r.spent, r.guard)
	}
	return nil
}

func (r *titleRun) writeManifest(d demo) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Capture manifest: %s\n\n", d.Title)
	b.WriteString("Written by `tools/capture`. The Tavily key is read from `TAVILY_KEY` and is never recorded here.\n\n")
	fmt.Fprintf(&b, "- Slug: `%s`\n", d.Slug)
	fmt.Fprintf(&b, "- Title searched: %s\n", d.Title)
	fmt.Fprintf(&b, "- iOS trackId (reviews): %d\n", d.ID)
	if r.trackName != "" {
		fmt.Fprintf(&b, "- App Store trackName for that trackId: %s\n", r.trackName)
	} else {
		b.WriteString("- App Store trackName for that trackId: not in the search results\n")
	}
	host := r.host
	if host == "" {
		host = "none"
	}
	fmt.Fprintf(&b, "- Official host used for policy and terms: %s\n", host)
	fmt.Fprintf(&b, "- Reviews captured: %d\n", r.reviews)
	credits := strconv.FormatFloat(r.spent, 'f', -1, 64)
	if r.unreported > 0 {
		credits += fmt.Sprintf(" (plus %d call(s) with credits not reported)", r.unreported)
	}
	fmt.Fprintf(&b, "- Tavily credits for this title: %s\n\n", credits)

	b.WriteString("## Files\n\n| File | Source | HTTP status | Fetched (UTC) | Tavily credits | Note |\n|---|---|---|---|---|---|\n")
	for _, f := range r.files {
		fmt.Fprintf(&b, "| %s | %s | %d | %s | %s | %s |\n",
			f.name, f.url, f.status, f.fetched.UTC().Format(time.RFC3339), f.credits, f.note)
	}

	b.WriteString("\n## Notes\n\n")
	if len(r.notes) == 0 {
		b.WriteString("- none\n")
	}
	for _, n := range r.notes {
		fmt.Fprintf(&b, "- %s\n", n)
	}
	return os.WriteFile(filepath.Join(r.out, "MANIFEST.md"), []byte(b.String()), 0o644)
}

// response is one answer, with the URL it came from and when it arrived.
type response struct {
	URL    string
	Status int
	Body   []byte
	At     time.Time
}

func (c *capturer) get(u string) (response, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return response{}, err
	}
	req.Header.Set("Accept", "application/json")
	return c.do(req, u)
}

// post sends the key in the Authorization header only.
func (c *capturer) post(u string, body any) (response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return response{}, err
	}
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.key)
	return c.do(req, u)
}

func (c *capturer) do(req *http.Request, u string) (response, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return response{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return response{}, err
	}
	return response{URL: u, Status: resp.StatusCode, Body: raw, At: c.now().UTC()}, nil
}

// creditsOf reads usage.credits. ok is false when Tavily did not report it.
func creditsOf(body []byte) (float64, bool) {
	var u struct {
		Usage *struct {
			Credits *float64 `json:"credits"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &u) != nil || u.Usage == nil || u.Usage.Credits == nil {
		return 0, false
	}
	return *u.Usage.Credits, true
}

// feedURL is the review feed page, as appstore.FeedURL builds it.
func feedURL(base string, id int64, page int) string {
	return fmt.Sprintf("%s/us/rss/customerreviews/page=%d/id=%d/sortby=mostrecent/json", base, page, id)
}

// countEntries returns the feed entries on a page, and how many carry a review
// body. The first page leads with the app itself, which has no body.
func countEntries(raw json.RawMessage) (entries, reviews int) {
	s := strings.TrimSpace(string(raw))
	switch {
	case s == "" || s == "null":
		return 0, 0
	case strings.HasPrefix(s, "["):
		var es []feedEntry
		if json.Unmarshal(raw, &es) != nil {
			return 0, 0
		}
		for _, e := range es {
			entries++
			if e.Content.Label != "" {
				reviews++
			}
		}
		return entries, reviews
	default:
		var e feedEntry
		if json.Unmarshal(raw, &e) != nil {
			return 0, 0
		}
		if e.Content.Label != "" {
			return 1, 1
		}
		return 1, 0
	}
}

type feedEntry struct {
	Content struct {
		Label string `json:"label"`
	} `json:"content"`
}

// app is one App Store search hit, with only the fields this program reads.
type app struct {
	ID        int64  `json:"trackId"`
	Name      string `json:"trackName"`
	SellerURL string `json:"sellerUrl"`
}

// officialHost returns the sellerUrl host of the first hit whose name starts
// with the title, and that hit. This is pickApp and hostOf from gather.go.
func officialHost(apps []app, title string) (string, *app) {
	a := pickApp(apps, title)
	if a == nil {
		return "", nil
	}
	return hostOf(a.SellerURL), a
}

func pickApp(apps []app, title string) *app {
	t := strings.ToLower(strings.TrimSpace(title))
	for i := range apps {
		if strings.HasPrefix(strings.ToLower(apps[i].Name), t) {
			return &apps[i]
		}
	}
	return nil
}

func hostOf(u string) string {
	pu, err := url.Parse(u)
	if err != nil || pu.Host == "" {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(pu.Host), "www.")
}

// searchReq is one Tavily search, with the fields plan() sets. Zero fields are
// omitted, as the pipeline's tavily.Search omits them.
type searchReq struct {
	Query          string   `json:"query"`
	Topic          string   `json:"topic,omitempty"`
	MaxResults     int      `json:"max_results,omitempty"`
	IncludeDomains []string `json:"include_domains,omitempty"`
	IncludeRaw     bool     `json:"include_raw_content,omitempty"`
}

type tavilySearch struct {
	searchReq
	IncludeUsage bool `json:"include_usage"`
}

type tavilyExtract struct {
	URLs         []string `json:"urls"`
	ExtractDepth string   `json:"extract_depth"`
	Format       string   `json:"format"`
	IncludeUsage bool     `json:"include_usage"`
}

// query is one planned search. Extract marks the policy and terms searches,
// whose top hit is fetched in full.
type query struct {
	Kind    string
	Search  searchReq
	Extract bool
}

// plan is the gatherer's own source plan, pipeline.Plan, in the request
// shape this program sends, so the fixtures keep matching what the pipeline
// asks. Kind is the plan entry's name, which is also the fixture file name.
func plan(title, officialHost string) []query {
	var out []query
	for _, q := range pipeline.Plan(title, officialHost) {
		out = append(out, query{Kind: q.Name, Extract: q.Extract, Search: searchReq{
			Query:          q.Search.Query,
			Topic:          q.Search.Topic,
			MaxResults:     q.Search.MaxResults,
			IncludeDomains: q.Search.IncludeDomains,
			IncludeRaw:     q.Search.IncludeRaw,
		}})
	}
	return out
}
