package appstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// titlesDir is the fixture root captured by T0.4, relative to this package.
const titlesDir = "../../testdata/titles"

func readFixture(parts ...string) ([]byte, error) {
	return os.ReadFile(filepath.Join(append([]string{titlesDir}, parts...)...))
}

func mustFixture(t *testing.T, parts ...string) []byte {
	t.Helper()
	b, err := readFixture(parts...)
	if err != nil {
		t.Fatalf("fixture %s: %v", filepath.Join(parts...), err)
	}
	return b
}

// feedPathRE matches the review feed path for any two-letter country.
var feedPathRE = regexp.MustCompile(`^/([a-z]{2})/rss/customerreviews/page=(\d+)/id=(\d+)/sortby=mostrecent/json$`)

// feedServer serves a title's recorded review pages and records every path it
// was asked for, so a test can pin what the client requested.
type feedServer struct {
	t    *testing.T
	slug string

	mu     sync.Mutex
	seen   []string
	body   map[int][]byte
	status map[int]int
	srv    *httptest.Server
}

func newFeedServer(t *testing.T, slug string) *feedServer {
	t.Helper()
	f := &feedServer{t: t, slug: slug, body: map[int][]byte{}, status: map[int]int{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *feedServer) url() string { return f.srv.URL }

func (f *feedServer) handle(w http.ResponseWriter, r *http.Request) {
	m := feedPathRE.FindStringSubmatch(r.URL.Path)
	if m == nil {
		f.t.Errorf("feed server: unexpected path %s", r.URL.Path)
		http.NotFound(w, r)
		return
	}
	page, _ := strconv.Atoi(m[2])

	f.mu.Lock()
	f.seen = append(f.seen, r.URL.Path)
	status, hasStatus := f.status[page]
	body, hasBody := f.body[page]
	f.mu.Unlock()

	if hasStatus && status != http.StatusOK {
		http.Error(w, http.StatusText(status), status)
		return
	}
	if !hasBody {
		var err error
		body, err = readFixture(f.slug, fmt.Sprintf("reviews-page-%02d.json", page))
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

// replace makes page answer with body instead of its fixture.
func (f *feedServer) replace(page int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.body[page] = []byte(body)
}

// fail makes page answer with status instead of its fixture.
func (f *feedServer) fail(page, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status[page] = status
}

// requests returns every path the server was asked for, in order.
func (f *feedServer) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.seen...)
}

// The surface the pipeline's gather step (T3.1) holds a Store for.
var _ interface {
	Find(ctx context.Context, title string, limit int) ([]App, error)
	Reviews(ctx context.Context, appID int64, pages int) ([]Review, error)
	FeedURL(appID int64, page int) string
} = (*Client)(nil)

func TestFindDecodesSearchFixture(t *testing.T) {
	cases := []struct {
		slug        string
		term        string
		hits        int
		want        App
		description string
	}{
		{
			slug: "khan-academy-kids", term: "Khan Academy Kids", hits: 4,
			want: App{
				ID: 1378467217, Name: "Khan Academy Kids", Seller: "Khan Academy",
				SellerURL: "http://khankids.org",
				StoreURL:  "https://apps.apple.com/us/app/khan-academy-kids/id1378467217?uo=4",
				AgeRating: "4+",
			},
			description: "Make screen time more meaningful",
		},
		{
			slug: "minecraft", term: "Minecraft", hits: 5,
			want: App{
				ID: 479516143, Name: "Minecraft: Play with Friends!", Seller: "Mojang AB",
				SellerURL: "",
				StoreURL:  "https://apps.apple.com/us/app/minecraft-play-with-friends/id479516143?uo=4",
				AgeRating: "9+",
			},
			description: "Dive into an open world",
		},
		{
			slug: "roblox", term: "Roblox", hits: 3,
			want: App{
				ID: 431946152, Name: "Roblox", Seller: "Roblox Corporation",
				SellerURL: "http://www.roblox.com/",
				StoreURL:  "https://apps.apple.com/us/app/roblox/id431946152?uo=4",
				AgeRating: "12+",
			},
			description: "MILLIONS OF GAMES ACROSS EVERY GENRE",
		},
		{
			slug: "toca-life-world", term: "Toca Life World", hits: 4,
			want: App{
				ID: 1208138685, Name: "Toca Boca World: Game & Play", Seller: "Toca Boca AB",
				SellerURL: "https://tocaboca.com",
				StoreURL:  "https://apps.apple.com/us/app/toca-boca-world-game-play/id1208138685?uo=4",
				AgeRating: "4+",
			},
			description: "Welcome to a world where your imagination comes first",
		},
	}

	for _, tc := range cases {
		t.Run(tc.slug, func(t *testing.T) {
			data := mustFixture(t, tc.slug, "appstore-search.json")
			var (
				mu    sync.Mutex
				query url.Values
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				query = r.URL.Query()
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.Write(data)
			}))
			defer srv.Close()

			apps, err := New(srv.URL, "us").Find(context.Background(), tc.term, 5)
			if err != nil {
				t.Fatalf("Find: %v", err)
			}
			if len(apps) != tc.hits {
				t.Fatalf("got %d hits, want %d", len(apps), tc.hits)
			}
			got := apps[0]
			if got.ID != tc.want.ID {
				t.Errorf("ID = %d, want %d", got.ID, tc.want.ID)
			}
			if got.Name != tc.want.Name {
				t.Errorf("Name = %q, want %q", got.Name, tc.want.Name)
			}
			if got.Seller != tc.want.Seller {
				t.Errorf("Seller = %q, want %q", got.Seller, tc.want.Seller)
			}
			if got.SellerURL != tc.want.SellerURL {
				t.Errorf("SellerURL = %q, want %q", got.SellerURL, tc.want.SellerURL)
			}
			if got.StoreURL != tc.want.StoreURL {
				t.Errorf("StoreURL = %q, want %q", got.StoreURL, tc.want.StoreURL)
			}
			if got.AgeRating != tc.want.AgeRating {
				t.Errorf("AgeRating = %q, want %q", got.AgeRating, tc.want.AgeRating)
			}
			if got.Description == "" {
				t.Error("Description is empty")
			} else if !strings.Contains(got.Description, tc.description) {
				t.Errorf("Description does not contain %q", tc.description)
			}
			// The whole description decodes, not only its opening words. The
			// expectation comes from a generic decode of the same bytes, not
			// from the package's own struct.
			var generic struct {
				Results []map[string]any `json:"results"`
			}
			if err := json.Unmarshal(data, &generic); err != nil {
				t.Fatalf("fixture: %v", err)
			}
			wantDescription, _ := generic.Results[0]["description"].(string)
			if got.Description != wantDescription {
				t.Errorf("Description = %d bytes, want the fixture's %d", len(got.Description), len(wantDescription))
			}

			mu.Lock()
			q := query
			mu.Unlock()
			for _, kv := range [][2]string{
				{"term", tc.term}, {"entity", "software"}, {"country", "us"}, {"limit", "5"},
			} {
				if v := q.Get(kv[0]); v != kv[1] {
					t.Errorf("search query %s = %q, want %q", kv[0], v, kv[1])
				}
			}
		})
	}
}

func TestFindErrorOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	apps, err := New(srv.URL, "us").Find(context.Background(), "Roblox", 5)
	if err == nil {
		t.Fatal("Find: want an error for HTTP 500, got none")
	}
	if len(apps) != 0 {
		t.Fatalf("Find: got %d hits alongside the error, want none", len(apps))
	}
}

// wantReview is a review decoded straight from the fixtures by this test,
// independently of the package's own decoder, to compare against.
type wantReview struct {
	page    int
	id      string
	title   string
	body    string
	rating  int
	author  string
	version string
	updated time.Time
}

func loadWantReviews(t *testing.T, slug string, pages int) []wantReview {
	t.Helper()
	var want []wantReview
	for p := 1; p <= pages; p++ {
		var doc struct {
			Feed struct {
				Entry []struct {
					ID      struct{ Label string } `json:"id"`
					Title   struct{ Label string } `json:"title"`
					Content struct{ Label string } `json:"content"`
					Rating  struct{ Label string } `json:"im:rating"`
					Version struct{ Label string } `json:"im:version"`
					Updated struct{ Label string } `json:"updated"`
					Author  struct {
						Name struct{ Label string } `json:"name"`
					} `json:"author"`
				} `json:"entry"`
			} `json:"feed"`
		}
		data := mustFixture(t, slug, fmt.Sprintf("reviews-page-%02d.json", p))
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s page %d: %v", slug, p, err)
		}
		for _, e := range doc.Feed.Entry {
			if e.Content.Label == "" {
				continue // not a review
			}
			rating, _ := strconv.Atoi(strings.TrimSpace(e.Rating.Label))
			updated, _ := time.Parse(time.RFC3339, e.Updated.Label)
			want = append(want, wantReview{
				page:    p,
				id:      e.ID.Label,
				title:   e.Title.Label,
				body:    e.Content.Label,
				rating:  rating,
				author:  e.Author.Name.Label,
				version: e.Version.Label,
				updated: updated,
			})
		}
	}
	return want
}

func TestReviewsIndexAcrossPages(t *testing.T) {
	for _, tc := range []struct {
		slug  string
		appID int64
	}{
		{"khan-academy-kids", 1378467217},
		{"minecraft", 479516143},
		{"roblox", 431946152},
		{"toca-life-world", 1208138685},
	} {
		t.Run(tc.slug, func(t *testing.T) {
			fs := newFeedServer(t, tc.slug)
			got, err := New(fs.url(), "us").Reviews(context.Background(), tc.appID, MaxPages)
			if err != nil {
				t.Fatalf("Reviews: %v", err)
			}
			want := loadWantReviews(t, tc.slug, MaxPages)
			if len(want) != MaxPages*PageSize {
				t.Fatalf("fixtures hold %d reviews, want %d", len(want), MaxPages*PageSize)
			}
			if len(got) != len(want) {
				t.Fatalf("got %d reviews, want %d", len(got), len(want))
			}
			for i := range got {
				g, w := got[i], want[i]
				if g.Index != i+1 {
					t.Fatalf("review %d has index %d, want %d", i, g.Index, i+1)
				}
				if g.ID != w.id || g.Title != w.title || g.Body != w.body || g.Rating != w.rating ||
					g.Author != w.author || g.Version != w.version {
					t.Errorf("review %d (page %d) does not match the recorded entry:\ngot  %+v\nwant %+v",
						i, w.page, g, w)
				}
				if !g.Updated.Equal(w.updated) {
					t.Errorf("review %d updated = %s, want %s", i, g.Updated, w.updated)
				}
			}

			// The index runs on across page boundaries: page 2 opens at 51.
			for _, i := range []int{0, PageSize - 1, PageSize, 2*PageSize - 1, len(got) - 1} {
				if got[i].Index != i+1 {
					t.Errorf("review at %d: index %d, want %d", i, got[i].Index, i+1)
				}
			}

			// The client asked for exactly the recorded pages, in order.
			reqs := fs.requests()
			if len(reqs) != MaxPages {
				t.Fatalf("requested %d pages, want %d: %v", len(reqs), MaxPages, reqs)
			}
			for i, req := range reqs {
				wantPath := fmt.Sprintf("/us/rss/customerreviews/page=%d/id=%d/sortby=mostrecent/json", i+1, tc.appID)
				if req != wantPath {
					t.Errorf("request %d = %s, want %s", i, req, wantPath)
				}
			}
		})
	}
}

func TestReviewsPageTwoStartsAtFiftyOne(t *testing.T) {
	fs := newFeedServer(t, "roblox")
	got, err := New(fs.url(), "us").Reviews(context.Background(), 431946152, 2)
	if err != nil {
		t.Fatalf("Reviews: %v", err)
	}
	if len(got) != 2*PageSize {
		t.Fatalf("got %d reviews, want %d", len(got), 2*PageSize)
	}

	// Values read from testdata/titles/roblox/reviews-page-01.json.
	first := got[0]
	if first.Index != 1 {
		t.Errorf("first review: index %d, want 1", first.Index)
	}
	if first.ID != "14656908432" || first.Rating != 1 || first.Author != "A\u2019Miaya" ||
		first.Title != "Pls fix your Robux and pls give my Robux back" {
		t.Errorf("first review = %+v, does not match reviews-page-01.json entry 0", first)
	}
	if !first.Updated.Equal(time.Date(2026, time.October, 12, 1, 2, 13, 0, time.FixedZone("", -7*3600))) {
		t.Errorf("first review updated = %s, want 2026-10-12T01:02:13-07:00", first.Updated)
	}
	if !strings.HasPrefix(first.Text(), first.Title+"\n") || !strings.Contains(first.Text(), "Robux") {
		t.Errorf("first review text does not carry the recorded body: %q", first.Text())
	}

	// Values read from testdata/titles/roblox/reviews-page-02.json: its entry 0
	// is the 51st review of the feed.
	page2 := got[PageSize]
	if page2.Index != 51 {
		t.Errorf("page 2's first review: index %d, want 51", page2.Index)
	}
	if page2.ID != "14656371036" || page2.Rating != 5 || page2.Author != "Teagan\U0001F338" || page2.Title != "Idk" {
		t.Errorf("page 2's first review = %+v, does not match reviews-page-02.json entry 0", page2)
	}
	if !page2.Updated.Equal(time.Date(2026, time.October, 11, 20, 59, 5, 0, time.FixedZone("", -7*3600))) {
		t.Errorf("page 2's first review updated = %s, want 2026-10-11T20:59:05-07:00", page2.Updated)
	}

	if last := got[len(got)-1]; last.Index != 100 {
		t.Errorf("last review: index %d, want 100", last.Index)
	}
}

func TestReviewsHandlesSingleEntryPage(t *testing.T) {
	// Apple sends entry as a bare object, not a one-element array, when a page
	// holds a single review. Build such a page from a recorded entry.
	var doc struct {
		Feed struct {
			Entry []json.RawMessage `json:"entry"`
		} `json:"feed"`
	}
	data := mustFixture(t, "roblox", "reviews-page-01.json")
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if len(doc.Feed.Entry) == 0 {
		t.Fatal("fixture has no entries")
	}
	one := string(doc.Feed.Entry[0])

	for _, tc := range []struct{ name, page string }{
		{"bare object", `{"feed":{"entry":` + one + `}}`},
		{"one-element array", `{"feed":{"entry":[` + one + `]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFeedServer(t, "roblox")
			fs.replace(1, tc.page)

			got, err := New(fs.url(), "us").Reviews(context.Background(), 431946152, 1)
			if err != nil {
				t.Fatalf("Reviews: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("got %d reviews, want 1", len(got))
			}
			r := got[0]
			if r.Index != 1 {
				t.Errorf("index = %d, want 1", r.Index)
			}
			// Values read from reviews-page-01.json entry 0.
			if r.ID != "14656908432" || r.Rating != 1 || r.Author != "A\u2019Miaya" ||
				r.Title != "Pls fix your Robux and pls give my Robux back" {
				t.Errorf("review = %+v, does not match the recorded entry", r)
			}
			if !strings.Contains(r.Body, "Robux") {
				t.Errorf("body = %q, want the recorded review text", r.Body)
			}
			if !r.Updated.Equal(time.Date(2026, time.October, 12, 1, 2, 13, 0, time.FixedZone("", -7*3600))) {
				t.Errorf("updated = %s, want 2026-10-12T01:02:13-07:00", r.Updated)
			}
		})
	}
}

func TestDecodeEntries(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    int
		wantErr bool
	}{
		{"absent", "", 0, false},
		{"null", "null", 0, false},
		{"empty array", "[]", 0, false},
		{"array", `[{"id":{"label":"1"}},{"id":{"label":"2"}}]`, 2, false},
		{"bare object", `{"id":{"label":"7"}}`, 1, false},
		{"wrong shape", "42", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			es, err := decodeEntries(json.RawMessage(tc.raw))
			if tc.wantErr != (err != nil) {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if len(es) != tc.want {
				t.Fatalf("got %d entries, want %d", len(es), tc.want)
			}
		})
	}

	es, err := decodeEntries(json.RawMessage(`{"id":{"label":"7"},"author":{"name":{"label":"Ada"}}}`))
	if err != nil {
		t.Fatalf("decodeEntries: %v", err)
	}
	if es[0].ID.Label != "7" || es[0].Author.Name.Label != "Ada" {
		t.Errorf("bare object decoded to %+v, want id 7 by Ada", es[0])
	}
}

func TestReviewsEmptyPageStops(t *testing.T) {
	for _, tc := range []struct{ name, page string }{
		{"no entry member", `{"feed":{}}`},
		{"empty entry array", `{"feed":{"entry":[]}}`},
		{"null entry", `{"feed":{"entry":null}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFeedServer(t, "roblox")
			fs.replace(2, tc.page)

			got, err := New(fs.url(), "us").Reviews(context.Background(), 431946152, MaxPages)
			if err != nil {
				t.Fatalf("Reviews: %v", err)
			}
			if len(got) != PageSize {
				t.Fatalf("got %d reviews, want the %d read before the empty page", len(got), PageSize)
			}
			if last := got[len(got)-1]; last.Index != PageSize {
				t.Errorf("last review: index %d, want %d", last.Index, PageSize)
			}
			if reqs := fs.requests(); len(reqs) != 2 {
				t.Fatalf("requested %d pages (%v), want 2: the empty page ends the read", len(reqs), reqs)
			}
		})
	}
}

func TestReviewsEmptyFirstPageIsNotAnError(t *testing.T) {
	fs := newFeedServer(t, "roblox")
	fs.replace(1, `{"feed":{}}`)

	got, err := New(fs.url(), "us").Reviews(context.Background(), 431946152, MaxPages)
	if err != nil {
		t.Fatalf("Reviews: %v, want no error for an empty feed", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d reviews, want none", len(got))
	}
	if reqs := fs.requests(); len(reqs) != 1 {
		t.Fatalf("requested %d pages (%v), want 1", len(reqs), reqs)
	}
}

func TestReviewsLaterPageFailureEndsRead(t *testing.T) {
	for _, tc := range []struct {
		name string
		fail func(*feedServer)
	}{
		{"http 500", func(f *feedServer) { f.fail(3, http.StatusInternalServerError) }},
		{"http 404", func(f *feedServer) { f.fail(3, http.StatusNotFound) }},
		{"undecodable body", func(f *feedServer) { f.replace(3, `{"feed":{"entry":42}}`) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFeedServer(t, "roblox")
			tc.fail(fs)

			got, err := New(fs.url(), "us").Reviews(context.Background(), 431946152, MaxPages)
			if err != nil {
				t.Fatalf("Reviews: %v, want the reviews read before the failing page", err)
			}
			if len(got) != 2*PageSize {
				t.Fatalf("got %d reviews, want the %d read before page 3", len(got), 2*PageSize)
			}
			if got[0].Index != 1 || got[len(got)-1].Index != 2*PageSize {
				t.Errorf("indices run %d..%d, want 1..%d", got[0].Index, got[len(got)-1].Index, 2*PageSize)
			}
			if reqs := fs.requests(); len(reqs) != 3 {
				t.Fatalf("requested %d pages (%v), want 3: the read ends at the failing page", len(reqs), reqs)
			}
		})
	}
}

func TestReviewsFirstPageFailureIsError(t *testing.T) {
	for _, tc := range []struct {
		name string
		fail func(*feedServer)
	}{
		{"http 500", func(f *feedServer) { f.fail(1, http.StatusInternalServerError) }},
		{"undecodable body", func(f *feedServer) { f.replace(1, `{"feed":42}`) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFeedServer(t, "roblox")
			tc.fail(fs)

			got, err := New(fs.url(), "us").Reviews(context.Background(), 431946152, MaxPages)
			if err == nil {
				t.Fatal("Reviews: want an error for a failed first page, got none")
			}
			if len(got) != 0 {
				t.Fatalf("got %d reviews alongside the error, want none", len(got))
			}
			if reqs := fs.requests(); len(reqs) != 1 {
				t.Fatalf("requested %d pages (%v), want 1", len(reqs), reqs)
			}
		})
	}
}

func TestReviewsPageCap(t *testing.T) {
	for _, pages := range []int{0, -1, MaxPages + 5} {
		t.Run(fmt.Sprintf("pages=%d", pages), func(t *testing.T) {
			fs := newFeedServer(t, "roblox")
			got, err := New(fs.url(), "us").Reviews(context.Background(), 431946152, pages)
			if err != nil {
				t.Fatalf("Reviews: %v", err)
			}
			if len(got) != MaxPages*PageSize {
				t.Fatalf("got %d reviews, want %d", len(got), MaxPages*PageSize)
			}
			if reqs := fs.requests(); len(reqs) != MaxPages {
				t.Fatalf("requested %d pages, want the feed's %d-page cap", len(reqs), MaxPages)
			}
		})
	}
}

func TestReviewsUsesStorefrontCountry(t *testing.T) {
	fs := newFeedServer(t, "roblox")
	got, err := New(fs.url(), "gb").Reviews(context.Background(), 431946152, 1)
	if err != nil {
		t.Fatalf("Reviews: %v", err)
	}
	if len(got) != PageSize {
		t.Fatalf("got %d reviews, want %d", len(got), PageSize)
	}
	reqs := fs.requests()
	if len(reqs) != 1 {
		t.Fatalf("requested %d pages, want 1", len(reqs))
	}
	if want := "/gb/rss/customerreviews/page=1/id=431946152/sortby=mostrecent/json"; reqs[0] != want {
		t.Errorf("request = %s, want %s", reqs[0], want)
	}
}

func TestReviewsSkipsTheAppEntry(t *testing.T) {
	// The feed sometimes leads a page with the app itself, which carries no
	// review text and must not take an index.
	fs := newFeedServer(t, "roblox")
	fs.replace(1, `{"feed":{"entry":[`+
		`{"id":{"label":"app"},"content":{"label":""}},`+
		`{"id":{"label":"r1"},"title":{"label":"One"},"content":{"label":"First"},`+
		`"im:rating":{"label":"5"},"author":{"name":{"label":"Ada"}}}`+
		`]}}`)

	got, err := New(fs.url(), "us").Reviews(context.Background(), 431946152, 1)
	if err != nil {
		t.Fatalf("Reviews: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d reviews, want 1: the app entry is not a review", len(got))
	}
	if got[0].Index != 1 || got[0].ID != "r1" || got[0].Rating != 5 || got[0].Author != "Ada" {
		t.Errorf("review = %+v, want the single review at index 1", got[0])
	}
}

func TestReviewsStopsOnCancelledContext(t *testing.T) {
	fs := newFeedServer(t, "roblox")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := New(fs.url(), "us").Reviews(ctx, 431946152, MaxPages)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Reviews: error = %v, want context.Canceled", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d reviews, want none", len(got))
	}
	if reqs := fs.requests(); len(reqs) != 0 {
		t.Fatalf("requested %d pages, want none", len(reqs))
	}
}

func TestReviewText(t *testing.T) {
	r := Review{Title: "  Spends money  ", Body: "  Bought $99 of Robux. \n"}
	if got, want := r.Text(), "Spends money\nBought $99 of Robux."; got != want {
		t.Errorf("Text() = %q, want %q", got, want)
	}
}

func TestURLShape(t *testing.T) {
	c := New("", "")
	if c.BaseURL != DefaultBaseURL || c.Country != "us" {
		t.Fatalf("New(\"\", \"\") = %+v, want the default host and country", c)
	}
	// The URL recorded in testdata/titles/roblox/MANIFEST.md.
	const want = "https://itunes.apple.com/us/rss/customerreviews/page=2/id=431946152/sortby=mostrecent/json"
	if got := c.FeedURL(431946152, 2); got != want {
		t.Errorf("FeedURL = %s, want %s", got, want)
	}
	if got, want := New("https://example.test/", "gb").FeedURL(1, 1),
		"https://example.test/gb/rss/customerreviews/page=1/id=1/sortby=mostrecent/json"; got != want {
		t.Errorf("FeedURL = %s, want %s", got, want)
	}
}
