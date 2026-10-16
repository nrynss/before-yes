package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/before-yes/internal/appstore"
	"github.com/nrynss/before-yes/internal/finding"
	"github.com/nrynss/before-yes/internal/spend"
	"github.com/nrynss/before-yes/internal/tavily"
)

// demo is one fixture title under testdata/titles.
type demo struct {
	slug, title string
}

var demos = []demo{
	{"roblox", "Roblox"},
	{"minecraft", "Minecraft"},
	{"khan-academy-kids", "Khan Academy Kids"},
	{"toca-life-world", "Toca Life World"},
}

var fixedNow = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

// fixture serves one title's recorded captures as the iTunes and Tavily
// hosts, so the real clients parse the real bytes. A search is matched to its
// file by the plan query that sent it, and its hits are narrowed by
// include_domains the way Tavily narrows them: the captures were recorded
// with open policy and terms searches where the host was unknown.
type fixture struct {
	t     *testing.T
	dir   string
	title string

	mu       sync.Mutex
	searches []tavily.Search
	extracts [][]string
	// storePageReads counts the extracts of the listing page.
	storePageReads int
}

func newFixture(t *testing.T, d demo) (*fixture, *httptest.Server) {
	t.Helper()
	f := &fixture{t: t, dir: filepath.Join("..", "..", "testdata", "titles", d.slug), title: d.title}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

var feedPath = regexp.MustCompile(`^/[a-z]{2}/rss/customerreviews/page=(\d+)/id=\d+/sortby=mostrecent/json$`)

func (f *fixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/search" && r.Method == http.MethodGet:
		f.file(w, "appstore-search.json")
	case feedPath.MatchString(r.URL.Path):
		n, _ := strconv.Atoi(feedPath.FindStringSubmatch(r.URL.Path)[1])
		name := filepath.Join(f.dir, "reviews-page-"+pad(n)+".json")
		if _, err := os.Stat(name); err != nil {
			_, _ = io.WriteString(w, `{"feed":{}}`)
			return
		}
		f.file(w, filepath.Base(name))
	case r.URL.Path == "/search":
		f.search(w, r)
	case r.URL.Path == "/extract":
		f.extract(w, r)
	default:
		f.t.Errorf("fixture: unexpected %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}
}

func pad(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func (f *fixture) file(w http.ResponseWriter, name string) {
	b, err := os.ReadFile(filepath.Join(f.dir, name))
	if err != nil {
		f.t.Errorf("fixture: %v", err)
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

func (f *fixture) search(w http.ResponseWriter, r *http.Request) {
	var s tavily.Search
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		f.t.Errorf("fixture: search body: %v", err)
	}
	f.mu.Lock()
	f.searches = append(f.searches, s)
	f.mu.Unlock()
	name := ""
	for _, q := range Plan(f.title, "") {
		if q.Search.Query == s.Query {
			name = q.Name
		}
	}
	if name == "" {
		f.t.Errorf("fixture: search %q is not in the plan", s.Query)
		http.Error(w, "unplanned", 400)
		return
	}
	var body map[string]any
	b, _ := os.ReadFile(filepath.Join(f.dir, "tavily-search-"+name+".json"))
	if err := json.Unmarshal(b, &body); err != nil {
		f.t.Errorf("fixture: %s: %v", name, err)
	}
	if len(s.IncludeDomains) > 0 {
		var keep []any
		for _, h := range body["results"].([]any) {
			u := h.(map[string]any)["url"].(string)
			for _, d := range s.IncludeDomains {
				if registrable(u) == d || strings.HasSuffix(registrable(u), "."+d) {
					keep = append(keep, h)
					break
				}
			}
		}
		body["results"] = keep
	}
	_ = json.NewEncoder(w).Encode(body)
}

// extract answers from tavily-extract.json. A URL the capture did not read
// is a failed result, which is what a live extract of it would be offline.
func (f *fixture) extract(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URLs []string `json:"urls"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	f.extracts = append(f.extracts, req.URLs)
	f.mu.Unlock()
	// The listing page itself is answered from its own capture (the US page
	// text), the way a live extract of it would be.
	if len(req.URLs) == 1 {
		if b, err := os.ReadFile(filepath.Join(f.dir, "tavily-extract-storepage.json")); err == nil {
			var page struct {
				Results []struct {
					URL string `json:"url"`
				} `json:"results"`
			}
			if json.Unmarshal(b, &page) == nil && len(page.Results) > 0 && page.Results[0].URL == req.URLs[0] {
				f.mu.Lock()
				f.storePageReads++
				f.mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(b)
				return
			}
		}
	}
	var rec struct {
		Results []map[string]any `json:"results"`
		Usage   map[string]any   `json:"usage"`
	}
	b, _ := os.ReadFile(filepath.Join(f.dir, "tavily-extract.json"))
	if err := json.Unmarshal(b, &rec); err != nil {
		f.t.Errorf("fixture: extract: %v", err)
	}
	results := []map[string]any{}
	failed := []map[string]any{}
	for _, u := range req.URLs {
		hit := false
		for _, p := range rec.Results {
			if p["url"] == u {
				results = append(results, p)
				hit = true
				break
			}
		}
		if !hit {
			failed = append(failed, map[string]any{"url": u, "error": "not in the capture"})
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"results": results, "failed_results": failed, "usage": rec.Usage})
}

func (f *fixture) credits() float64 {
	total := 0.0
	add := func(name string) {
		var rec struct {
			Usage struct {
				Credits float64 `json:"credits"`
			} `json:"usage"`
		}
		b, _ := os.ReadFile(filepath.Join(f.dir, name))
		_ = json.Unmarshal(b, &rec)
		total += rec.Usage.Credits
	}
	for _, q := range Plan(f.title, "") {
		add("tavily-search-" + q.Name + ".json")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.extracts) > f.storePageReads {
		add("tavily-extract.json")
	}
	if f.storePageReads > 0 {
		add("tavily-extract-storepage.json")
	}
	return total
}

// feedStore keeps the source URLs on Apple's public host, as a live check
// cites them, while the reads go to the fixture server.
type feedStore struct {
	*appstore.Client
	public *appstore.Client
}

func (s feedStore) FeedURL(id int64, page int) string { return s.public.FeedURL(id, page) }

func fixtureClients(t *testing.T, srv *httptest.Server) (Store, *tavily.Client) {
	tc := tavily.New(srv.URL, func() (string, error) { return "fixture", nil })
	tc.Retry.Backoff = time.Millisecond
	tc.Retry.Max = time.Millisecond
	tc.Retry.PerMinute = 0
	tc.Retry.Name = ""
	tc.Retry.Sleep = func(context.Context, time.Duration) error { return nil }
	return feedStore{appstore.New(srv.URL, "us"), appstore.New("", "us")}, tc
}

func openTestMeter(t *testing.T) *spend.Meter {
	t.Helper()
	m, err := spend.Open(context.Background(), spend.Config{
		Path: filepath.Join(t.TempDir(), "spend.db"),
		Pools: map[spend.Pool]spend.PoolConfig{
			spend.PoolModel:  {Limit: 1_000_000_000_000},
			spend.PoolTavily: {Limit: 1_000_000},
		},
	})
	if err != nil {
		t.Fatalf("open meter: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func gatherFixture(t *testing.T, d demo) (Gathered, *fixture, *spend.Meter) {
	t.Helper()
	f, srv := newFixture(t, d)
	store, tc := fixtureClients(t, srv)
	m := openTestMeter(t)
	g := &Gatherer{Store: store, Search: tc, Meter: m, Now: func() time.Time { return fixedNow }}
	got, err := g.Gather(context.Background(), d.title, "check-"+d.slug)
	if err != nil {
		t.Fatalf("Gather(%s): %v", d.title, err)
	}
	return got, f, m
}

func countKinds(docs []Doc) map[string]int {
	n := map[string]int{}
	for _, d := range docs {
		n[d.Source.Kind]++
	}
	return n
}

func docURLs(docs []Doc, kind string) []string {
	var us []string
	for _, d := range docs {
		if d.Source.Kind == kind {
			us = append(us, d.Source.URL)
		}
	}
	return us
}

// TestGatherFixtures pins what each demo title gathers from its captures.
func TestGatherFixtures(t *testing.T) {
	type want struct {
		host, from string
		app        int64
		kinds      map[string]int
		policy     []string
		terms      []string
		missing    []string
	}
	cases := map[string]want{
		"roblox": {
			host: "roblox.com", from: "seller_url", app: 431946152,
			kinds: map[string]int{KindReview: 500, KindStore: 1, KindTerms: 1, KindReddit: 16, KindForum: 4, KindNews: 7, KindRating: 6},
			terms: []string{"https://en.help.roblox.com/hc/en-us/articles/14663412502676-Roblox-is-Updating-our-Terms-of-Use"},
			// The open policy search's hits on roblox.com are the download
			// page and two homepages, none of them a policy.
			missing: []string{KindPolicy},
		},
		"minecraft": {
			host: "minecraft.net", from: "policy_hit", app: 479516143,
			kinds: map[string]int{KindReview: 500, KindStore: 1, KindReddit: 16, KindForum: 6, KindNews: 8, KindRating: 6},
			// The only policy hit on minecraft.net is the usage guidelines,
			// which is not a privacy policy. The narrowed terms hits are the
			// EULA pages, which the capture never extracted, so offline
			// they fail to read.
			missing: []string{KindPolicy, KindTerms},
		},
		"khan-academy-kids": {
			host: "khankids.org", from: "seller_url", app: 1378467217,
			// The off-product filter drops 15 sibling pages (see
			// TestOffProductFilterFixtures): T3.10's 10 (8 reddit threads and
			// the internetmatters and Common Sense pages about Khan Academy,
			// the website, not the Kids app) and, by the shorter-sibling text
			// rule, 2 reddit threads and 3 Common Sense articles that name
			// Khan Academy and never Khan Academy Kids.
			kinds: map[string]int{KindReview: 500, KindStore: 1, KindReddit: 6, KindForum: 1, KindNews: 1, KindRating: 5},
			// Both searches return only the homepage, which is skipped, so
			// no extract call is made.
			missing: []string{KindPolicy, KindTerms},
		},
		"toca-life-world": {
			host: "tocaboca.com", from: "policy_hit", app: 1208138685,
			kinds:  map[string]int{KindReview: 500, KindStore: 1, KindPolicy: 1, KindTerms: 1, KindReddit: 16, KindForum: 3, KindNews: 8, KindRating: 6},
			policy: []string{"https://www.tocaboca.com/docs/en/privacy-policy"},
			terms:  []string{"https://www.tocaboca.com/docs/en/terms-of-service"},
		},
	}
	for _, d := range demos {
		t.Run(d.slug, func(t *testing.T) {
			w := cases[d.slug]
			got, f, m := gatherFixture(t, d)
			if got.Host != w.host || got.HostFrom != w.from {
				t.Errorf("host = %q from %q, want %q from %q", got.Host, got.HostFrom, w.host, w.from)
			}
			if got.App == nil || got.App.ID != w.app {
				t.Fatalf("app = %+v, want trackId %d", got.App, w.app)
			}
			kinds := countKinds(got.Docs)
			for k, n := range w.kinds {
				if kinds[k] != n {
					t.Errorf("%s docs = %d, want %d", k, kinds[k], n)
				}
			}
			for k := range kinds {
				if _, ok := w.kinds[k]; !ok {
					t.Errorf("unexpected %d %s docs", kinds[k], k)
				}
			}
			if w.policy != nil && strings.Join(docURLs(got.Docs, KindPolicy), " ") != strings.Join(w.policy, " ") {
				t.Errorf("policy docs = %v, want %v", docURLs(got.Docs, KindPolicy), w.policy)
			}
			if w.terms != nil && strings.Join(docURLs(got.Docs, KindTerms), " ") != strings.Join(w.terms, " ") {
				t.Errorf("terms docs = %v, want %v", docURLs(got.Docs, KindTerms), w.terms)
			}
			if strings.Join(got.Missing, ",") != strings.Join(w.missing, ",") {
				t.Errorf("missing = %v, want %v (notes %q)", got.Missing, w.missing, got.Notes)
			}

			// Policy and terms are narrowed once the host is known, and
			// every policy or terms document is on it.
			for _, s := range f.searches {
				policyOrTerms := strings.HasSuffix(s.Query, " privacy policy") || strings.HasSuffix(s.Query, " terms of service")
				if !policyOrTerms {
					continue
				}
				open := len(s.IncludeDomains) == 0
				if open && !(w.from == "policy_hit" && strings.HasSuffix(s.Query, " privacy policy")) {
					t.Errorf("search %q ran without include_domains", s.Query)
				}
				if !open && s.IncludeDomains[0] != w.host {
					t.Errorf("search %q narrowed to %v, want %s", s.Query, s.IncludeDomains, w.host)
				}
			}
			for _, doc := range got.Docs {
				if (doc.Source.Kind == KindPolicy || doc.Source.Kind == KindTerms) && registrable(doc.Source.URL) != w.host {
					t.Errorf("%s doc %s is off the official host", doc.Source.Kind, doc.Source.URL)
				}
			}

			// Seven searches and at most one extract, and the meter booked
			// what Tavily reported.
			if len(f.searches) != 7 || len(f.extracts) > 2 {
				t.Errorf("calls = %d searches, %d extracts; want 7 and at most 2", len(f.searches), len(f.extracts))
			}
			if got.Credits != f.credits() {
				t.Errorf("credits = %g, want the captures' %g", got.Credits, f.credits())
			}
			if got.Credits > 8 {
				t.Errorf("credits = %g, over the plan's eight", got.Credits)
			}
			spent, err := m.Spent(context.Background(), spend.PoolTavily, "check-"+d.slug)
			if err != nil {
				t.Fatal(err)
			}
			if spent != credits(got.Credits) {
				t.Errorf("meter booked %d, want %d", spent, credits(got.Credits))
			}

			for _, doc := range got.Docs {
				if strings.TrimSpace(doc.Text) == "" {
					t.Errorf("empty %s doc %s", doc.Source.Kind, doc.Source.URL)
				}
				if len(doc.Text) > maxDocChars {
					t.Errorf("%s doc %s is %d chars, over the cap", doc.Source.Kind, doc.Source.URL, len(doc.Text))
				}
				if doc.Source.Kind == KindReddit {
					for _, c := range []string{"Skip to main content", "Open menu", "Log In"} {
						if strings.Contains(doc.Text, c) {
							t.Errorf("reddit doc %s keeps chrome %q", doc.Source.URL, c)
						}
					}
				}
				if strings.Contains(doc.Source.URL, "127.0.0.1") {
					t.Errorf("doc cites the fixture server: %s", doc.Source.URL)
				}
			}
		})
	}
}

// TestOffProductFilterFixtures pins T3.10's product-identity filter on the
// demo captures: Khan Academy Kids drops the sibling Khan Academy pages and
// counts them in OffProduct, keeps its own pages (the org's kids page, its
// helpdesk host, its stores, its named reviews of the app), and never drops a
// review or the store description. Roblox, Minecraft and Toca drop nothing:
// their brand is a single word (or has no shorter sibling), so any page
// naming the brand names the product.
//
// The shorter-sibling rule adds 5 drops to T3.10's 10: pages whose subject
// names neither app but whose text names "Khan Academy" (the sibling whose
// name is a strict part of "Khan Academy Kids") and never "Khan Academy
// Kids".
func TestOffProductFilterFixtures(t *testing.T) {
	khan := demos[2]
	got, _, _ := gatherFixture(t, khan)
	if got.OffProduct != 15 {
		t.Errorf("OffProduct = %d, want 15 (T3.10's 10, plus 2 reddit and 3 Common Sense pages naming only the shorter sibling)", got.OffProduct)
	}
	dropped := []string{
		// The Khan Academy website, not the Kids app.
		"https://www.internetmatters.org/advice/apps-and-platforms/skills-building/khan-academy",
		"https://www.commonsensemedia.org/website-reviews/khan-academy",
		// Reddit threads about Khan Academy in general.
		"https://www.reddit.com/r/videos/comments/got5ih/khan_academy_is_struggling_with_rising_demand",
		"https://www.reddit.com/r/HomeschoolRecovery/comments/18o02u5/how_to_set_up_khan_academy",
		"https://www.reddit.com/r/Brighterly/comments/1tb8os5/abcmouse_vs_khan_academy_vs_brighterly_what",
		"https://www.reddit.com/r/NoStupidQuestions/comments/wi1f3z/how_did_khan_academy_go_so_quickly_from_beloved",
		"https://www.reddit.com/r/learnmath/comments/u18fbs/is_khan_academy_good_or_not",
		"https://www.reddit.com/r/personalfinance/comments/5n4xhp/reminder_khan_academy_still_has_basic",
		"https://www.reddit.com/r/math/comments/1de3z2q/i_feel_like_khan_academy_is_garbage_for_learning",
		"https://www.reddit.com/r/learnmath/comments/1jbq4dk/why_does_my_math_teacher_hate_khan_academy",
		// The shorter-sibling rule: the subject names neither app, the text
		// names Khan Academy only.
		"https://www.reddit.com/r/mathteachers/comments/1bf2j6j/how_do_you_convince_a_parentsspouse_that_kumon",
		"https://www.reddit.com/r/learnmath/comments/vvb3s9/is_brilliantorg_worth_it",
		"https://www.commonsensemedia.org/articles/parents-ultimate-guide-to-youtube-kids",
		"https://www.commonsensemedia.org/articles/parents-ultimate-guide-to-google-classroom",
		"https://www.commonsensemedia.org/articles/how-to-keep-kids-learning-when-theyre-stuck-at-home",
	}
	for _, u := range dropped {
		if d, ok := docByURL(got.Docs, u); ok {
			t.Errorf("sibling page kept: %s (%s)", u, d.Source.Title)
		}
	}
	kept := []string{
		// The product's own page on the org's domain, its helpdesk (the host
		// names the product though the title says only "Khan Academy"), its
		// store pages and its named app review.
		"https://www.khanacademy.org/kids",
		"https://khankids.zendesk.com/hc/en-us/articles/360047566151-How-do-I-access-parental-controls",
		"https://apps.apple.com/mx/app/khan-academy-kids/id1378467217?l=en-GB",
		"https://play.google.com/store/apps/details?id=org.khankids.android&hl=en_US",
		"https://www.modulo.app/all-resources/khan-academy-kids",
		"https://www.commonsensemedia.org/app-reviews/khan-academy-kids",
		"https://www.reddit.com/r/homeschool/comments/1irs0ld/want_to_try_khan_academy_kids_for_our_little_ones",
		// Threads about other developers' products name neither the product
		// nor its sibling, so they are noise, not off-product drops: the
		// Roblox fixtures keep other developers' news the same way.
		"https://www.reddit.com/r/Teachers/comments/1tb6n5r/i_hate_iready_so_much",
	}
	for _, u := range kept {
		if _, ok := docByURL(got.Docs, u); !ok {
			t.Errorf("the Kids app's own page was dropped: %s", u)
		}
	}
	if n := countKinds(got.Docs)[KindReview]; n != 500 {
		t.Errorf("reviews = %d, want all 500: the filter never drops a review", n)
	}
	if _, ok := docByURL(got.Docs, "https://apps.apple.com/us/app/khan-academy-kids/id1378467217?uo=4"); !ok {
		t.Error("the store description was dropped")
	}
	if strings.Join(got.Missing, ",") != "policy,terms" {
		t.Errorf("missing = %v: no kind lost every document", got.Missing)
	}

	// Nothing new drops for the other captures: neither Roblox, Minecraft
	// nor Toca Life World has a same-seller app whose name is a strict part of
	// the title's, so the text rule never runs for them.
	for _, d := range []demo{demos[0], demos[1], demos[3]} {
		got, _, _ := gatherFixture(t, d)
		if got.OffProduct != 0 {
			t.Errorf("%s: OffProduct = %d, want 0 (brand and product are one word)", d.slug, got.OffProduct)
		}
	}
}

func docByURL(docs []Doc, u string) (Doc, bool) {
	for _, d := range docs {
		if d.Source.URL == u {
			return d, true
		}
	}
	return Doc{}, false
}

// TestOffProductClassifier pins the filter's rule on hand-made documents: a
// sibling's name run drops, the product's name run keeps (in the title, the
// URL path, or a sibling-titled page on the product's own helpdesk host), a
// page on the official host keeps, and a review or store doc never drops.
func TestOffProductClassifier(t *testing.T) {
	kids := &appstore.App{Name: "Khan Academy Kids", Seller: "Khan Academy"}
	apps := []appstore.App{
		{Name: "Khan Academy Kids", Seller: "Khan Academy"},
		{Name: "Khan Academy", Seller: "Khan Academy"},
		{Name: "PBS KIDS Games", Seller: "PBS Kids"},
	}
	const host = "khankids.org"
	docs := []Doc{
		{Source: finding.Source{Kind: KindRating, Title: "What is Khan Academy? Safety guide for parents", URL: "https://www.internetmatters.org/advice/skills-building/khan-academy"}},
		{Source: finding.Source{Kind: KindReddit, Title: "Khan academy is struggling with rising demand", URL: "https://www.reddit.com/r/videos/comments/got5ih/khan_academy_is_struggling"}},
		{Source: finding.Source{Kind: KindRating, Title: "Free Learning App for Kids Ages 2-8 | Khan Academy Kids", URL: "https://www.khanacademy.org/kids"}},
		{Source: finding.Source{Kind: KindRating, Title: "How do I access parental controls? - Khan Academy - Zendesk", URL: "https://khankids.zendesk.com/hc/en-us/articles/360047566151"}},
		{Source: finding.Source{Kind: KindRating, Title: "‎Khan Academy Kids App - App Store", URL: "https://apps.apple.com/mx/app/khan-academy-kids/id1378467217?l=en-GB"}},
		{Source: finding.Source{Kind: KindReddit, Title: "Want to try Khan Academy Kids", URL: "https://www.reddit.com/r/homeschool/comments/1irs0ld/want_to_try_khan_academy_kids"}},
		{Source: finding.Source{Kind: KindPolicy, Title: "Privacy Policy", URL: "https://www.khankids.org/privacy"}}, // official host
		{Source: finding.Source{Kind: KindReview, Title: "Khan Academy the website is better", URL: "https://itunes.apple.com/us/rss/customerreviews/page=1/id=1378467217/sortby=mostrecent/json", Index: 2}},
		{Source: finding.Source{Kind: KindStore, Title: "Khan Academy on the App Store", URL: "https://apps.apple.com/us/app/khan-academy-kids/id1378467217?uo=4"}},
		{Source: finding.Source{Kind: KindNews, Title: "A Khan Academy school page on the sibling's own host", URL: "https://khanacademy.org/teachers"}},
	}
	kept, dropped := offProduct(apps, kids, host, docs)
	if dropped != 3 {
		t.Errorf("dropped = %d, want 3", dropped)
	}
	for _, i := range []int{0, 1, 9} {
		if _, ok := docByURL(kept, docs[i].Source.URL); ok {
			t.Errorf("doc %d (%s) kept, want dropped", i, docs[i].Source.URL)
		}
	}
	for i, d := range docs {
		if i == 0 || i == 1 || i == 9 {
			continue
		}
		if _, ok := docByURL(kept, d.Source.URL); !ok {
			t.Errorf("doc %d (%s) dropped, want kept", i, d.Source.URL)
		}
	}

	// The shorter-sibling text rule: a page whose subject names neither app
	// (a score for a title, as Common Sense titles its privacy evaluations)
	// and whose text names only "Khan Academy" is about the website.
	text := []Doc{
		{Source: finding.Source{Kind: KindRating, Title: "Privacy Evaluation: 78%", URL: "https://privacy.commonsense.org/evaluation/1021"}, Text: "Khan Academy. We may share information with our trusted vendors. Khan Academy is a nonprofit."},
		{Source: finding.Source{Kind: KindRating, Title: "Privacy Evaluation: 80%", URL: "https://privacy.commonsense.org/evaluation/9999"}, Text: "Khan Academy and Khan Academy Kids are covered by one policy."},
		{Source: finding.Source{Kind: KindRating, Title: "Privacy Evaluation: 64%", URL: "https://privacy.commonsense.org/evaluation/5555"}, Text: "Seesaw for families."},
		{Source: finding.Source{Kind: KindPolicy, Title: "Privacy Policy", URL: "https://www.khankids.org/privacy"}, Text: "Khan Academy collects."},
		{Source: finding.Source{Kind: KindReview, Title: "Great", URL: "https://itunes.apple.com/x", Index: 1}, Text: "Khan Academy is the same."},
		{Source: finding.Source{Kind: KindStore, Title: "Khan", URL: "https://apps.apple.com/us/app/khan-academy-kids/id1378467217"}, Text: "Khan Academy nonprofit."},
	}
	keptText, droppedText := offProduct(apps, kids, host, text)
	if droppedText != 1 {
		t.Errorf("text rule: dropped = %d, want 1 (the evaluation naming only Khan Academy)", droppedText)
	}
	if _, ok := docByURL(keptText, text[0].Source.URL); ok {
		t.Error("text rule: a page naming only the shorter sibling was kept")
	}
	// A seller whose other app is not a part of the title's name never
	// triggers the text rule.
	if _, n := offProduct([]appstore.App{{Name: "Khan Academy Kids", Seller: "Khan Academy"}, {Name: "Khan Math", Seller: "Khan Academy"}}, kids, host, text[:1]); n != 0 {
		t.Errorf("text rule: dropped %d for a sibling that is not part of the title's name, want 0", n)
	}

	// No sibling beside the product: nothing drops, whatever the subject.
	solo := []appstore.App{{Name: "Roblox", Seller: "Roblox Corporation"}, {Name: "Block Blast", Seller: "ARETIS LIMITED"}}
	rbx := []Doc{{Source: finding.Source{Kind: KindNews, Title: "Epic Games to pay record $520 million fine", URL: "https://gameworldobserver.com/x"}, Text: "text"}}
	if kept, dropped := offProduct(solo, &appstore.App{Name: "Roblox", Seller: "Roblox Corporation"}, "roblox.com", rbx); dropped != 0 || len(kept) != 1 {
		t.Errorf("Roblox: dropped %d, want 0", dropped)
	}

	// No matched app: no product identity, nothing drops.
	if _, dropped := offProduct(nil, nil, "", docs); dropped != 0 {
		t.Errorf("no app: dropped %d, want 0", dropped)
	}
}

// cannedSearch answers each plan query from a fixed hit list, so a test can
// shape one kind's results without a capture. Everything the gatherer asks
// to extract comes back unread.
type cannedSearch struct {
	title   string
	results map[string][]tavily.Result
}

func (c *cannedSearch) Search(_ context.Context, s tavily.Search) ([]tavily.Result, tavily.Credits, error) {
	for _, q := range Plan(c.title, "") {
		if q.Search.Query == s.Query {
			return c.results[q.Name], tavily.Credits{Used: 1, Reported: true}, nil
		}
	}
	return nil, tavily.Credits{}, nil
}

func (c *cannedSearch) ExtractWithFailures(_ context.Context, urls []string) (tavily.Extracted, error) {
	failed := make([]tavily.Failed, len(urls))
	for i, u := range urls {
		failed[i] = tavily.Failed{URL: u, Error: "not served"}
	}
	return tavily.Extracted{Failed: failed}, nil
}

// TestOffProductDroppedKindGoesMissing pins the branch that turns a kind
// whose every page was a sibling's into a missing source (T3.10, fault 2;
// round-1 review L2). The Khan capture's App Store search supplies the
// sibling, a canned search answers the two Reddit queries and the forum
// query with sibling-only hits, and after the drop both kinds are missing
// with the note, the hits counted in OffProduct.
func TestOffProductDroppedKindGoesMissing(t *testing.T) {
	d := demos[2] // Khan Academy Kids, whose sibling Khan Academy shares the seller
	f, srv := newFixture(t, d)
	store, _ := fixtureClients(t, srv)
	m := openTestMeter(t)
	search := &cannedSearch{
		title: d.title,
		results: map[string][]tavily.Result{
			"reddit-parents": {{
				URL:     "https://www.reddit.com/r/videos/comments/got5ih/khan_academy_is_struggling_with_rising_demand",
				Title:   "Khan Academy is struggling with rising demand",
				Content: "Khan Academy is struggling to keep up with the rising demand for its free courses.",
			}},
			"forum": {{
				URL:     "https://www.internetmatters.org/advice/apps-and-platforms/skills-building/khan-academy",
				Title:   "What is Khan Academy? Safety guide for parents",
				Content: "Khan Academy offers free online lessons for learners of every age.",
			}},
		},
	}
	_ = f
	g := &Gatherer{Store: store, Search: search, Meter: m, Now: func() time.Time { return fixedNow }}
	got, err := g.Gather(context.Background(), d.title, "check-khan-canned")
	if err != nil {
		t.Fatal(err)
	}
	if got.OffProduct != 2 {
		t.Errorf("OffProduct = %d, want 2 (one reddit hit, one forum hit)", got.OffProduct)
	}
	kinds := countKinds(got.Docs)
	if kinds[KindReddit] != 0 || kinds[KindForum] != 0 {
		t.Errorf("sibling-only kinds kept docs: reddit %d, forum %d", kinds[KindReddit], kinds[KindForum])
	}
	for _, kind := range []string{KindReddit, KindForum} {
		if !contains(got.Missing, kind) {
			t.Errorf("%s is not missing: %v", kind, got.Missing)
		}
		want := "every " + kind + " page was about another product of the same developer"
		if !contains(got.Notes, want) {
			t.Errorf("note %q not recorded: %v", want, got.Notes)
		}
	}
}

// TestGatherReviewSource checks a review document carries its index and its
// feed page on Apple's public host.
func TestGatherReviewSource(t *testing.T) {
	got, _, _ := gatherFixture(t, demos[0])
	for _, d := range got.Docs {
		if d.Source.Kind != KindReview || d.Source.Index != 355 {
			continue
		}
		want := "https://itunes.apple.com/us/rss/customerreviews/page=8/id=431946152/sortby=mostrecent/json"
		if d.Source.URL != want {
			t.Errorf("review 355 url = %s, want %s", d.Source.URL, want)
		}
		if !d.Source.FetchedAt.Equal(fixedNow) {
			t.Errorf("fetched at %v", d.Source.FetchedAt)
		}
		return
	}
	t.Fatal("no review 355")
}

func TestHostFromHitsRefusesLookAlikes(t *testing.T) {
	// The App Store hits as captured: Minecraft's own listing has no seller
	// URL, its sibling Minecraft Education names education.minecraft.net,
	// and Toca's store name fails the prefix rule.
	toca := []appstore.App{
		{Name: "Toca Boca World: Game & Play", Seller: "Toca Boca AB", SellerURL: "https://tocaboca.com"},
		{Name: "Avatar World ®", Seller: "Pazu Games Ltd", SellerURL: "http://pazugames.com"},
	}
	mc := []appstore.App{
		{Name: "Minecraft: Play with Friends!", Seller: "Mojang AB"},
		{Name: "Minecraft Education", Seller: "Mojang AB", SellerURL: "https://education.minecraft.net/"},
		{Name: "MultiCraft — Build and Mine!", Seller: "MultiCraft Studio OU", SellerURL: "https://multicraft.world"},
	}
	rbx := []appstore.App{{Name: "Roblox", Seller: "Roblox Corporation"}}
	hit := func(u string) []tavily.Result { return []tavily.Result{{URL: u}} }
	cases := []struct {
		url, title string
		apps       []appstore.App
		picked     bool
		want       string
	}{
		{"https://www.tocaboca.com/docs/en/privacy-policy", "Toca Life World", toca, false, "tocaboca.com"},
		{"https://tocaboca.co.uk/privacy", "Toca Life World", toca, false, ""},
		{"https://tocaboca.helpshift.com/hc/en/privacy", "Toca Life World", toca, false, ""},
		{"https://tocabocashop.com/docs/privacy-policy", "Toca Life World", toca, false, ""},
		{"https://toca-boca.com/privacy", "Toca Life World", toca, false, ""},
		{"https://tocaboca.com.evil.io/privacy", "Toca Life World", toca, false, ""},
		// Another developer's own domain does not stand for this title.
		{"https://pazugames.com/privacy", "Toca Life World", toca, false, ""},
		{"https://www.minecraft.net/en-us/usage-guidelines", "Minecraft", mc, true, "minecraft.net"},
		{"https://minecraft.wiki/w/Minecraft_Wiki:Privacy_policy", "Minecraft", mc, true, ""},
		{"https://minecraft.co.uk/privacy", "Minecraft", mc, true, ""},
		{"https://minecraftshop.com/pages/privacy-policy", "Minecraft", mc, true, ""},
		{"https://mojang.xyz/privacy", "Minecraft", mc, true, ""},
		{"https://multicraft.world/privacy", "Minecraft", mc, true, ""},
		{"https://roblox.app/privacy", "Roblox", rbx, true, ""},
		{"https://www.roblox.com/info/privacy", "Roblox", rbx, true, ""},
	}
	for _, c := range cases {
		var picked *appstore.App
		if c.picked {
			picked = &c.apps[0]
		}
		if got, _ := hostFromHits(hit(c.url), c.apps, picked, c.title); got != c.want {
			t.Errorf("hostFromHits(%s, %q) = %q, want %q", c.url, c.title, got, c.want)
		}
	}
	// The developer match also names the app, so Toca's reviews are read.
	if _, a := hostFromHits(hit("https://www.tocaboca.com/p"), toca, nil, "Toca Life World"); a == nil || a.Seller != "Toca Boca AB" {
		t.Errorf("toca app = %+v", a)
	}
}

func TestAboutIgnoresQuery(t *testing.T) {
	if about(KindPolicy, "https://www.roblox.com/download?ref=privacy", "Download", "") {
		t.Error("a query string named the page a policy")
	}
	if !about(KindPolicy, "https://www.roblox.com/info/privacy", "", "") {
		t.Error("a privacy path was not a policy")
	}
	if got := candidates([]tavily.Result{{URL: "https://roblox.com/download?ref=privacy"}, {URL: "https://roblox.com/info/privacy"}}, KindPolicy, "roblox.com", map[string]string{}); got[0] != "https://roblox.com/info/privacy" {
		t.Errorf("candidates = %v", got)
	}
}

func TestErrorPage(t *testing.T) {
	type page struct{ title, text string }
	cases := map[page]bool{
		{"", ""}: true,
		{"", "Oops! Looks like that page doesn't exist. Go home."}: true,
		{"", "404 Not Found"}:                                                 true,
		{"", "Page not found\nTry the search box."}:                           true,
		{"Page Not Found | Common Sense Media", "Skip to main content\nMenu"}: true,
		{"", strings.Repeat("Menu item\n", 300) + "Oops! Looks like that page doesn’t exist.\nLet's help you find it."}:                                           true,
		{"Privacy Policy", "Privacy Policy\nWe collect your name and email to run the service."}:                                                                  false,
		{"", "Privacy Policy\nWe keep your data safe. If a page does not exist we show a notice, and nothing about you is logged when that happens on our site."}: false,
		{"", "My kid tried to open the store and the page does not exist anymore, so we stopped buying things in the game."}:                                      false,
		{"", "I got page not found when I clicked the link"}:                                                                                                      false,
		{"Privacy Policy", "Privacy Policy\n" + strings.Repeat("We keep data safe. ", 400) + "page not found"}:                                                    false,
		{"Why I get 404 error pages - Reddit", "Our site shows a 404 error when you log out."}:                                                                    false,
	}
	for in, want := range cases {
		if got := errorPage(in.title, in.text); got != want {
			t.Errorf("errorPage(%q, %.40q) = %v, want %v", in.title, in.text, got, want)
		}
	}
}

// The T0.4b captures: four Common Sense not-found pages Tavily read as
// successes, each opening with ~2400 characters of navigation. Every one
// must be caught, by its title and by its text alone.
func TestErrorPageOnCapturedNotFound(t *testing.T) {
	n := 0
	for _, slug := range []string{"roblox", "minecraft"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "titles", slug, "tavily-extract-csm-advanced.json"))
		if err != nil {
			t.Fatal(err)
		}
		var ex struct {
			Results []tavily.Page `json:"results"`
		}
		if err := json.Unmarshal(b, &ex); err != nil {
			t.Fatal(err)
		}
		for _, p := range ex.Results {
			n++
			if len(p.RawContent) < 5000 || !strings.Contains(p.RawContent, "Looks like that page doesn't exist") {
				t.Fatalf("%s: not the captured 404 shape", p.URL)
			}
			if !errorPage(p.Title, p.RawContent) || !errorPage("", p.RawContent) {
				t.Errorf("captured 404 read as a document: %s (%d chars)", p.URL, len(p.RawContent))
			}
		}
	}
	if n != 4 {
		t.Errorf("%d captured pages, want 4", n)
	}
}

// stubSearch serves one homepage hit per policy and terms search, and an
// extract that reads the homepage and the links named on it.
type stubSearch struct {
	home     string // the policy and terms searches' one hit; khankids.org when empty
	pages    map[string]string
	extracts [][]string
}

func (s *stubSearch) Search(_ context.Context, q tavily.Search) ([]tavily.Result, tavily.Credits, error) {
	if strings.HasSuffix(q.Query, "privacy policy") || strings.HasSuffix(q.Query, "terms of service") {
		home := s.home
		if home == "" {
			home = "https://www.khankids.org"
		}
		return []tavily.Result{{URL: home, Title: "Khan Academy Kids"}}, tavily.Credits{Used: 1, Reported: true}, nil
	}
	return nil, tavily.Credits{Used: 1, Reported: true}, nil
}

func (s *stubSearch) ExtractWithFailures(_ context.Context, urls []string) (tavily.Extracted, error) {
	s.extracts = append(s.extracts, urls)
	var ex tavily.Extracted
	for _, u := range urls {
		if t, ok := s.pages[u]; ok {
			ex.Pages = append(ex.Pages, tavily.Page{URL: u, RawContent: t})
		} else {
			ex.Failed = append(ex.Failed, tavily.Failed{URL: u, Error: "no"})
		}
	}
	ex.Credits = tavily.Credits{Used: 1, Reported: true}
	return ex, nil
}

type stubStore struct{ apps []appstore.App }

func (s stubStore) Find(context.Context, string, int) ([]appstore.App, error) { return s.apps, nil }
func (s stubStore) Reviews(context.Context, int64, int) ([]appstore.Review, error) {
	return nil, nil
}
func (s stubStore) FeedURL(int64, int) string { return "" }

// A homepage-only hit is read, and the privacy and terms links named on it
// are followed in one more extract call.
func TestGatherFollowsHomepageLinks(t *testing.T) {
	s := &stubSearch{pages: map[string]string{
		"https://www.khankids.org": "Khan Academy Kids\nLearn more. Privacy: https://www.khankids.org/privacy-policy. " +
			"Terms: https://www.khankids.org/terms-of-service. Elsewhere: https://evil.example/privacy.",
		"https://www.khankids.org/privacy-policy":   "Privacy Policy\nWe collect as little as we can.",
		"https://www.khankids.org/terms-of-service": "Terms of Service\nThese terms govern the app.",
	}}
	g := &Gatherer{Store: stubStore{[]appstore.App{{ID: 1, Name: "Khan Academy Kids", Seller: "Khan Academy", SellerURL: "http://khankids.org"}}},
		Search: s, Meter: openTestMeter(t)}
	got, err := g.Gather(context.Background(), "Khan Academy Kids", "c")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.extracts) != 2 || len(s.extracts[0]) != 1 || len(s.extracts[1]) != 2 {
		t.Fatalf("extracts = %v", s.extracts)
	}
	if u := docURLs(got.Docs, KindPolicy); len(u) != 1 || u[0] != "https://www.khankids.org/privacy-policy" {
		t.Errorf("policy = %v", u)
	}
	if u := docURLs(got.Docs, KindTerms); len(u) != 1 || u[0] != "https://www.khankids.org/terms-of-service" {
		t.Errorf("terms = %v", u)
	}
	for _, m := range got.Missing {
		if m == KindPolicy || m == KindTerms {
			t.Errorf("missing %v", got.Missing)
		}
	}
	// An unreadable homepage says why no link was followed.
	delete(s.pages, "https://www.khankids.org")
	s.extracts = nil
	got, _ = g.Gather(context.Background(), "Khan Academy Kids", "c2")
	if len(s.extracts) != 1 || !strings.Contains(strings.Join(got.Notes, "\n"), "homepage could not be read, so no policy link could be followed") {
		t.Errorf("extracts %v notes %q", s.extracts, got.Notes)
	}
}

func TestStripChrome(t *testing.T) {
	in := "Skip to main content Open menu Open navigation Go to Reddit Home r/RobloxParents Get App Log In\nMy son got a friend request from a stranger.\nLog Ins are weird"
	got := stripChrome(in)
	want := "r/RobloxParents\nMy son got a friend request from a stranger.\nLog Ins are weird"
	if got != want {
		t.Errorf("stripChrome = %q, want %q", got, want)
	}
}

func TestDropSummaries(t *testing.T) {
	in := "Roblox review\nParents say: kids love it\n## Kids say it is fun\nA parent wrote: my kid met a stranger"
	if got := dropSummaries(in); got != "Roblox review\nA parent wrote: my kid met a stranger" {
		t.Errorf("dropSummaries = %q", got)
	}
}

func TestPlanCredits(t *testing.T) {
	p := Plan("Roblox", "roblox.com")
	total := 0.0
	for _, q := range p {
		total += tavily.SearchCredits(q.Search.SearchDepth)
	}
	total += tavily.ExtractCredits(2 * extractTries)
	if total != 8 {
		t.Errorf("plan prices at %g credits, want 8", total)
	}
	for _, q := range p[:2] {
		if len(q.Search.IncludeDomains) != 1 || q.Search.IncludeDomains[0] != "roblox.com" || !q.Extract {
			t.Errorf("%s = %+v", q.Name, q.Search)
		}
	}
}

func TestGatherNeedsMeter(t *testing.T) {
	if _, err := (&Gatherer{}).Gather(context.Background(), "Roblox", "x"); err == nil {
		t.Error("Gather without a meter did not fail")
	}
}

// With no official host, a homepage the policy search turns up belongs to
// whoever runs it. Common Sense's homepage links its own privacy policy,
// which must never be read as Khan Academy Kids'.
func TestGatherNoHostFollowsNoLinks(t *testing.T) {
	s := &stubSearch{home: "https://www.commonsensemedia.org", pages: map[string]string{
		"https://www.commonsensemedia.org":                                     "Common Sense Media\nPrivacy: https://www.commonsensemedia.org/about-us/our-mission/privacy-policy",
		"https://www.commonsensemedia.org/about-us/our-mission/privacy-policy": "Privacy Policy\nCommon Sense collects your email.",
	}}
	g := &Gatherer{Store: stubStore{}, Search: s, Meter: openTestMeter(t)}
	got, err := g.Gather(context.Background(), "Khan Academy Kids", "c")
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "" {
		t.Fatalf("host = %q", got.Host)
	}
	if len(s.extracts) != 0 {
		t.Errorf("extracts = %v, want none", s.extracts)
	}
	if u := docURLs(got.Docs, KindPolicy); len(u) != 0 {
		t.Errorf("a third party's policy was read as the title's: %v", u)
	}
	// No signal places the title, so it is KindUnknown, whose general plan
	// has no policy search: nothing is read, and policy is not missing.
	if got.Kind != KindUnknown || contains(got.Missing, KindPolicy) {
		t.Errorf("kind %q missing %v notes %q", got.Kind, got.Missing, got.Notes)
	}
	if l := pageLinks("https://www.commonsensemedia.org/privacy-policy", "", KindPolicy); l != nil {
		t.Errorf("pageLinks with no host = %v", l)
	}
	if l := pageLinks("https://www.khankids.org/privacy https://evil.example/privacy https://help.khankids.org/privacy", "khankids.org", KindPolicy); len(l) != 2 {
		t.Errorf("pageLinks on host = %v", l)
	}
}

// A developer named for only the title's first word is not the title's.
func TestHostFromHitsNeedsMoreThanFirstWord(t *testing.T) {
	hits := []tavily.Result{{URL: "https://toca.xyz/privacy"}}
	apps := []appstore.App{{Name: "Toca Fake Hair", Seller: "Toca Ltd", SellerURL: "https://toca.xyz"}}
	if h, a := hostFromHits(hits, apps, nil, "Toca Boca World"); h != "" || a != nil {
		t.Errorf("toca.xyz accepted: %q %v", h, a)
	}
	// The full title, or the developer's full name, still passes.
	apps[0].Name = "Toca Boca World"
	apps[0].Seller = "Toca AB"
	if h, _ := hostFromHits(hits, apps, nil, "Toca Boca World"); h != "toca.xyz" {
		t.Errorf("full title match refused: %q", h)
	}
}

// The public suffix list decides the registrable domain, and a shared
// platform is never an official host.
func TestOfficialDomain(t *testing.T) {
	for in, want := range map[string][2]string{
		"https://www.tocaboca.com.au/x":        {"tocaboca.com.au", "tocaboca.com.au"},
		"https://help.minecraft.net/privacy":   {"minecraft.net", "minecraft.net"},
		"https://www.bbc.co.uk/":               {"bbc.co.uk", "bbc.co.uk"},
		"https://foo.github.io/privacy":        {"foo.github.io", ""},
		"https://www.facebook.com/tocaboca":    {"facebook.com", ""},
		"https://apps.apple.com/us/app/id1":    {"apple.com", ""},
		"https://someone.blogspot.com/":        {"someone.blogspot.com", ""},
		"https://sites.google.com/view/a-game": {"google.com", ""},
		"not a url":                            {"", ""},
	} {
		if r, o := registrable(in), officialDomain(in); r != want[0] || o != want[1] {
			t.Errorf("%s: registrable %q official %q, want %q %q", in, r, o, want[0], want[1])
		}
	}
	hits := []tavily.Result{{URL: "https://foo.github.io/privacy"}}
	apps := []appstore.App{{Name: "Foo", Seller: "Foo", SellerURL: "https://foo.github.io"}}
	if h, _ := hostFromHits(hits, apps, nil, "Foo"); h != "" {
		t.Errorf("a github.io page was taken as official: %q", h)
	}
}

type failStore struct {
	stubStore
	findErr, feedErr error
}

func (s failStore) Find(ctx context.Context, t string, n int) ([]appstore.App, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}
	return s.stubStore.Find(ctx, t, n)
}
func (s failStore) Reviews(context.Context, int64, int) ([]appstore.Review, error) {
	return nil, s.feedErr
}

// A call that errored is a failure; a source that is simply absent is not.
func TestGatherFailuresVsAbsent(t *testing.T) {
	khan := []appstore.App{{ID: 1, Name: "Khan Academy Kids", Seller: "Khan Academy", SellerURL: "http://khankids.org"}}
	pages := map[string]string{
		"https://www.khankids.org":                "Khan Academy Kids\nPrivacy: https://www.khankids.org/privacy-policy",
		"https://www.khankids.org/privacy-policy": "Privacy Policy\nWe collect as little as we can.",
	}
	for _, c := range []struct {
		name  string
		store Store
		pages map[string]string
		want  []string
	}{
		{"absent", failStore{stubStore: stubStore{khan}}, pages, nil},
		{"no listing", stubStore{}, pages, nil},
		{"store search", failStore{findErr: errors.New("503")}, pages, []string{"appstore_search"}},
		{"feed", failStore{stubStore: stubStore{khan}, feedErr: errors.New("503")}, pages, []string{"review_feed"}},
		{"extract", stubStore{khan}, map[string]string{}, []string{"extract https://www.khankids.org"}},
	} {
		s := &stubSearch{pages: c.pages}
		g := &Gatherer{Store: c.store, Search: s, Meter: openTestMeter(t)}
		got, err := g.Gather(context.Background(), "Khan Academy Kids", "c")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(got.Failures, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: failures %q, want %q", c.name, got.Failures, c.want)
		}
	}
}

// An unread candidate counts as a failure only when its kind is then
// missing. On the fixtures, Roblox's unread terms candidate cost nothing
// (another terms page was kept); Minecraft's unread terms pages and Khan's
// unread homepage did.
func TestGatherFailuresOnFixtures(t *testing.T) {
	want := map[string]string{
		"Roblox":            "",
		"Minecraft":         "extract https://www.minecraft.net/en-us/eula,extract https://www.minecraft.net/en-us/article/minecraft-eula-and-commercial-usage-guidelines-updates",
		"Khan Academy Kids": "extract https://www.khankids.org",
		"Toca Life World":   "",
	}
	for _, d := range demos {
		_, srv := newFixture(t, d)
		store, tc := fixtureClients(t, srv)
		g := &Gatherer{Store: store, Search: tc, Meter: openTestMeter(t), Now: func() time.Time { return fixedNow }}
		got, err := g.Gather(context.Background(), d.title, "c")
		if err != nil {
			t.Fatal(err)
		}
		if f := strings.Join(got.Failures, ","); f != want[d.title] {
			t.Errorf("%s: failures %q, want %q", d.title, f, want[d.title])
		}
	}
}
