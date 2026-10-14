package pipeline

import (
	"context"
	"encoding/json"
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
			if len(f.searches) != 7 || len(f.extracts) > 1 {
				t.Errorf("calls = %d searches, %d extracts; want 7 and at most 1", len(f.searches), len(f.extracts))
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
	apps := []appstore.App{
		{Name: "Toca Boca World: Game & Play", Seller: "Toca Boca AB"},
		{Name: "Minecraft: Play with Friends!", Seller: "Mojang AB"},
	}
	hit := func(u string) []tavily.Result { return []tavily.Result{{URL: u}} }
	cases := []struct {
		url, title, want string
	}{
		{"https://www.tocaboca.com/docs/en/privacy-policy", "Toca Life World", "tocaboca.com"},
		{"https://tocaboca.helpshift.com/hc/en/privacy", "Toca Life World", ""},
		{"https://tocabocashop.com/docs/privacy-policy", "Toca Life World", ""},
		{"https://toca-boca.com/privacy", "Toca Life World", ""},
		{"https://tocaboca.com.evil.io/privacy", "Toca Life World", ""},
		{"https://www.minecraft.net/en-us/usage-guidelines", "Minecraft", "minecraft.net"},
		{"https://minecraftshop.com/pages/privacy-policy", "Minecraft", ""},
		{"https://www.minecraftexperience.com/privacy-policy", "Minecraft", ""},
		{"https://mojang.com/privacy", "Minecraft", "mojang.com"},
		{"https://mojangab.com/privacy", "Minecraft", ""},
		{"https://minecraft.co.uk/privacy", "Minecraft", "minecraft.co.uk"},
		{"https://minecraft.evil.co.uk/privacy", "Minecraft", ""},
	}
	for _, c := range cases {
		if got, _ := hostFromHits(hit(c.url), apps, c.title); got != c.want {
			t.Errorf("hostFromHits(%s, %q) = %q, want %q", c.url, c.title, got, c.want)
		}
	}
	// The developer match also names the app, so Toca's reviews are read.
	if _, a := hostFromHits(hit("https://www.tocaboca.com/p"), apps, "Toca Life World"); a == nil || a.Seller != "Toca Boca AB" {
		t.Errorf("toca app = %+v", a)
	}
}

func TestErrorPage(t *testing.T) {
	cases := map[string]bool{
		"": true,
		"Oops! Looks like that page doesn't exist. Go home.": true,
		"404 Not Found": true,
		"Privacy Policy\nWe collect your name and email to run the service.":               false,
		"Privacy Policy\n" + strings.Repeat("We keep data safe. ", 400) + "page not found": false,
	}
	for in, want := range cases {
		if got := errorPage(in); got != want {
			t.Errorf("errorPage(%.40q) = %v, want %v", in, got, want)
		}
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
