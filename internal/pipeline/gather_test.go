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
	if len(f.extracts) > 0 {
		add("tavily-extract.json")
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
			kinds: map[string]int{KindReview: 500, KindStore: 1, KindReddit: 16, KindForum: 5, KindNews: 1, KindRating: 6},
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
