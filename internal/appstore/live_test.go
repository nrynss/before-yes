//go:build live

// Live probe for the App Store client (T2.3b).
//
// Run it with:
//
//	go test -tags live -v -count=1 -run TestLive ./internal/appstore/
//
// It searches for one demo title and reads that title's whole review feed in
// two storefronts, us and gb, with one search request and at most ten
// review-page requests per storefront. Nothing here runs in the gate or CI.
//
// The second storefront is gb. The brief does not place the parent in any
// country, so the probe measures a second English-language storefront instead
// of guessing at the parent's own. The storefront is one field on Client, so
// repointing this probe at the parent's own country, in included, is a
// one-line change. us is read too because the recorded fixtures are us, which
// lets the live feed be compared with the capture.
package appstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The probe reads Roblox: the demo title whose review volume the brief's "up
// to about 500 recent reviews per country" rests on. Its App Store id is the
// one in testdata/titles/roblox/appstore-search.json.
const (
	liveTitle = "Roblox"
	liveAppID = 431946152
)

// clientMembers are the feed members the client reads, for the field audit.
var clientMembers = []string{"id", "title", "content", "im:rating", "im:version", "updated", "author.name"}

// exchange is one HTTP request the probe made, with the body kept so every
// page is fetched once and inspected afterwards.
type exchange struct {
	url    string
	path   string
	ua     string
	status int
	body   []byte
	err    error
	took   time.Duration
}

// recorder wraps the real transport and keeps every exchange.
type recorder struct {
	inner http.RoundTripper

	mu   sync.Mutex
	seen []exchange
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := r.inner.RoundTrip(req)
	ex := exchange{url: req.URL.String(), path: req.URL.Path, ua: req.UserAgent(), took: time.Since(start)}
	if err != nil {
		ex.err = err
		r.add(ex)
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		ex.err = err
		r.add(ex)
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	ex.status, ex.body = resp.StatusCode, body
	r.add(ex)
	return resp, nil
}

func (r *recorder) add(ex exchange) {
	r.mu.Lock()
	r.seen = append(r.seen, ex)
	r.mu.Unlock()
}

func (r *recorder) exchanges() []exchange {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]exchange(nil), r.seen...)
}

// countryProbe is what one storefront answered.
type countryProbe struct {
	reviews     []Review
	pages       []exchange
	audit       fieldAudit
	ratingCount int64
	avgRating   float64
}

// TestLiveAppStore is the T2.3b probe.
func TestLiveAppStore(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	fixtureBaseline(t)

	probes := map[string]*countryProbe{}
	for _, country := range []string{"us", "gb"} {
		t.Run(country, func(t *testing.T) {
			probes[country] = probeCountry(t, ctx, country)
		})
	}
	compareStorefronts(t, probes["us"], probes["gb"])
}

// probeCountry searches for the title and reads the whole feed for it in one
// storefront. The client is the package's own, with a recording transport so
// the raw bodies can be audited without a second fetch.
func probeCountry(t *testing.T, ctx context.Context, country string) *countryProbe {
	t.Helper()
	t.Logf("=== live probe country=%s title=%q appID=%d ===", country, liveTitle, liveAppID)

	c := New("", country)
	rec := &recorder{inner: http.DefaultTransport}
	c.http.Transport = rec // New's timeout stands; the recorder keeps every body

	apps, err := c.Find(ctx, liveTitle, 5)
	if err != nil {
		t.Fatalf("live search: %v", err)
	}
	search := rec.exchanges()
	if len(search) != 1 {
		t.Fatalf("search took %d request(s), want 1", len(search))
	}
	hit, ok := findApp(apps, liveAppID)
	if !ok {
		t.Fatalf("live search: %d hit(s), none with trackId %d: %v", len(apps), liveAppID, appIDs(apps))
	}
	ratingCount, avgRating := logSearch(t, country, search[0], apps, hit)

	reviews, err := c.Reviews(ctx, liveAppID, 0)
	if err != nil {
		t.Fatalf("live reviews: %v", err)
	}
	// An empty first page is not an error: Apple's feed serves empty pages
	// intermittently for every app, and the probe records what it got rather
	// than failing on Apple's state.
	pages := rec.exchanges()[len(search):]
	audit := logReviews(t, country, pages, reviews)
	return &countryProbe{reviews: reviews, pages: pages, audit: audit, ratingCount: ratingCount, avgRating: avgRating}
}

// logSearch records the live search result and compares its fields with the
// recorded us capture. It returns the title hit's live userRatingCount and
// averageUserRating, the search API's per-storefront reach numbers.
func logSearch(t *testing.T, country string, ex exchange, apps []App, hit App) (ratingCount int64, avgRating float64) {
	t.Helper()
	t.Logf("live search %s: GET %s", country, ex.url)
	t.Logf("live search %s: User-Agent %s, HTTP %d in %s, %d hit(s), resultCount %d",
		country, uaLabel(ex.ua), ex.status, ex.took.Round(time.Millisecond), len(apps), resultCount(ex.body))
	for i, a := range apps {
		t.Logf("live search %s: hit[%d] id=%d name=%q seller=%q sellerUrl=%q storeUrl=%q ageRating=%q",
			country, i, a.ID, a.Name, a.Seller, a.SellerURL, a.StoreURL, a.AgeRating)
	}
	t.Logf("live search %s: title hit id=%d description %d chars", country, hit.ID, len(hit.Description))

	raw := rawSearchHits(t, ex.body)
	var rawHit map[string]json.RawMessage
	for _, h := range raw {
		if trackID(h) == liveAppID {
			rawHit = h
			break
		}
	}
	if rawHit == nil {
		t.Fatalf("live search %s: raw results hold no trackId %d", country, liveAppID)
	}
	t.Logf("live search %s: raw title hit %d keys; sellerUrl %s; trackViewUrl %s",
		country, len(rawHit), rawKind(rawHit, "sellerUrl"), rawKind(rawHit, "trackViewUrl"))
	ratingCount, avgRating = rawInt64(rawHit, "userRatingCount"), rawFloat64(rawHit, "averageUserRating")
	t.Logf("live search %s: title hit ratings: userRatingCount %d, averageUserRating %.5f", country, ratingCount, avgRating)

	data := mustFixture(t, "roblox", "appstore-search.json")
	var rec struct {
		ResultCount int   `json:"resultCount"`
		Results     []App `json:"results"`
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("recorded search: %v", err)
	}
	recHit, ok := findApp(rec.Results, liveAppID)
	if !ok {
		t.Fatalf("recorded search holds no trackId %d", liveAppID)
	}
	var recRawHit map[string]json.RawMessage
	for _, h := range rawSearchHits(t, data) {
		if trackID(h) == liveAppID {
			recRawHit = h
			break
		}
	}

	var same, differ []string
	for _, f := range []struct {
		name      string
		live, rec string
	}{
		{"id", strconv.FormatInt(hit.ID, 10), strconv.FormatInt(recHit.ID, 10)},
		{"name", hit.Name, recHit.Name},
		{"seller", hit.Seller, recHit.Seller},
		{"sellerUrl", hit.SellerURL, recHit.SellerURL},
		{"storeUrl", hit.StoreURL, recHit.StoreURL},
		{"ageRating", hit.AgeRating, recHit.AgeRating},
	} {
		if f.live == f.rec {
			same = append(same, f.name)
		} else {
			differ = append(differ, fmt.Sprintf("%s: live %q, recorded %q", f.name, f.live, f.rec))
		}
	}
	t.Logf("live search %s: recorded us capture resultCount %d, %d hit(s)", country, rec.ResultCount, len(rec.Results))
	t.Logf("live search %s: fields equal to the recorded capture: %s", country, listOr(same, "none"))
	for _, d := range differ {
		t.Logf("live search %s: field differs from the recorded capture: %s", country, d)
	}
	onlyLive, onlyRec := keyDiff(rawHit, recRawHit)
	t.Logf("live search %s: raw title hit keys: live %d, recorded %d; live-only %v; recorded-only %v",
		country, len(rawHit), len(recRawHit), onlyLive, onlyRec)
	t.Logf("live search %s: recorded capture ratings: userRatingCount %d, averageUserRating %.5f",
		country, rawInt64(recRawHit, "userRatingCount"), rawFloat64(recRawHit, "averageUserRating"))
	return ratingCount, avgRating
}

// logReviews records what the feed served, page by page, and pins the client's
// read against the raw bodies. It returns the field audit.
func logReviews(t *testing.T, country string, pages []exchange, reviews []Review) fieldAudit {
	t.Helper()
	var (
		audit       fieldAudit
		entries     []map[string]json.RawMessage
		times       []time.Time
		ids         []string
		shapes      []string
		singles     []string
		empties     []string
		leadApp     []string
		failed      []string
		lastPage    int
		lastShape   string
		lastReviews int
		total       time.Duration
	)
	if len(pages) > 0 {
		t.Logf("live reviews %s: first page GET %s (User-Agent %s)", country, pages[0].url, uaLabel(pages[0].ua))
	}
	for i, ex := range pages {
		m := feedPathRE.FindStringSubmatch(ex.path)
		if m == nil {
			t.Fatalf("page request %d: unexpected path %s", i+1, ex.path)
		}
		page, _ := strconv.Atoi(m[2])
		if m[1] != country {
			t.Fatalf("page %d went to storefront %q, want %q", page, m[1], country)
		}
		if m[3] != strconv.FormatInt(liveAppID, 10) {
			t.Fatalf("page %d asked for app %s, want %d", page, m[3], liveAppID)
		}
		total += ex.took
		if ex.err != nil {
			failed = append(failed, fmt.Sprintf("page %d: %v", page, ex.err))
			continue
		}
		shape := decodeShape(t, ex.body)
		shapes = append(shapes, fmt.Sprintf("%d:%s", page, shape.shape))
		lastPage, lastShape = page, shape.shape
		onPage := 0
		for j, e := range shape.entries {
			if j == 0 && leaf(e, "content") == "" {
				leadApp = append(leadApp, fmt.Sprintf("page %d", page))
			}
			audit.addEntry(e)
			if leaf(e, "content") == "" {
				continue
			}
			entries = append(entries, e)
			times = append(times, parseTime(leaf(e, "updated")))
			ids = append(ids, leaf(e, "id"))
			onPage++
		}
		lastReviews = onPage
		if len(shape.entries) == 1 {
			singles = append(singles, fmt.Sprintf("page %d (%s)", page, shape.shape))
		}
		if len(shape.entries) == 0 {
			empties = append(empties, fmt.Sprintf("page %d (%s)", page, shape.shape))
		}
		t.Logf("live reviews %s: page %d: entry %s, %d review(s), HTTP %d, %s, %s, feed updated %s",
			country, page, shape.shape, onPage, ex.status, ex.took.Round(time.Millisecond), bytesLabel(len(ex.body)), feedUpdated(ex.body))
	}
	t.Logf("live reviews %s: %d page request(s) in %s total: %s", country, len(pages), total.Round(time.Millisecond), strings.Join(shapes, " "))
	t.Logf("live reviews %s: feed served %d entries, %d reviews; client returned %d reviews",
		country, audit.entries, audit.reviews, len(reviews))
	if audit.reviews == 0 {
		t.Logf("live reviews %s: the feed served no entries; the client returned no error and no reviews, its documented empty-page behaviour",
			country)
	}
	t.Logf("live reviews %s: read stopped after %d page(s); the client's cap is %d", country, len(pages), MaxPages)
	t.Logf("live reviews %s: empty page before page 10: %s", country, listOr(empties, "none"))
	t.Logf("live reviews %s: single-entry page: %s", country, listOr(singles, "none"))
	t.Logf("live reviews %s: page led by a content-less (app) entry: %s", country, listOr(leadApp, "none"))
	t.Logf("live reviews %s: page that failed: %s", country, listOr(failed, "none"))
	if lastPage > 0 {
		note := "full"
		switch {
		case len(failed) > 0:
			note = "the read stopped at the failed page"
		case lastShape == "absent" || lastShape == "null" || lastShape == "array(0)":
			note = "empty: the feed ran out here"
		case lastReviews < PageSize:
			note = "partial: the feed ran out here"
		case len(pages) == MaxPages:
			note = "full: the client's 10-page cap ended the read, not an empty page"
		}
		t.Logf("live reviews %s: last page served: page %d, %s, %d review(s), %s", country, lastPage, lastShape, lastReviews, note)
	}
	t.Logf("live reviews %s: %s", country, audit.summary())
	t.Logf("live reviews %s: entry members seen: %s", country, memberList(audit.members))
	t.Logf("live reviews %s: %d review ids read, %d unique", country, len(ids), unique(ids))
	t.Logf("live reviews %s: sortby=mostrecent: %d out-of-order adjacent pair(s) in %d",
		country, inversions(times), len(times)-1)
	if len(reviews) > 0 {
		first, last := reviews[0], reviews[len(reviews)-1]
		t.Logf("live reviews %s: index %d first: id %s, rating %d, updated %s", country, first.Index, first.ID, first.Rating, stamp(first.Updated))
		t.Logf("live reviews %s: index %d last: id %s, rating %d, updated %s", country, last.Index, last.ID, last.Rating, stamp(last.Updated))
	}
	checkAgainstRaw(t, reviews, entries)
	return audit
}

// fixtureBaseline reports what the ten recorded Roblox pages show, so the
// live transcript can be read against them. It makes no requests.
func fixtureBaseline(t *testing.T) {
	t.Helper()
	var (
		audit  fieldAudit
		shapes []string
		times  []time.Time
		ids    []string
	)
	for p := 1; p <= MaxPages; p++ {
		data := mustFixture(t, "roblox", fmt.Sprintf("reviews-page-%02d.json", p))
		shape := decodeShape(t, data)
		shapes = append(shapes, fmt.Sprintf("%d:%s", p, shape.shape))
		for _, e := range shape.entries {
			audit.addEntry(e)
			if leaf(e, "content") == "" {
				continue
			}
			times = append(times, parseTime(leaf(e, "updated")))
			ids = append(ids, leaf(e, "id"))
		}
	}
	t.Logf("recorded fixture roblox/us: %d page(s): %s", MaxPages, strings.Join(shapes, " "))
	t.Logf("recorded fixture roblox/us: %s", audit.summary())
	t.Logf("recorded fixture roblox/us: entry members seen: %s", memberList(audit.members))
	t.Logf("recorded fixture roblox/us: %d review ids read, %d unique; %d out-of-order adjacent pair(s) in %d",
		len(ids), unique(ids), inversions(times), len(times)-1)
}

// compareStorefronts puts the two storefronts' numbers side by side.
func compareStorefronts(t *testing.T, us, gb *countryProbe) {
	t.Helper()
	if us == nil || gb == nil {
		t.Logf("storefront comparison skipped: a probe did not finish")
		return
	}
	t.Logf("=== storefronts us and gb ===")
	t.Logf("reviews served: us %d in %d page(s); gb %d in %d page(s)", len(us.reviews), len(us.pages), len(gb.reviews), len(gb.pages))
	t.Logf("field audit: us %s", us.audit.summary())
	t.Logf("field audit: gb %s", gb.audit.summary())
	t.Logf("storefront reach (search API, same title): us userRatingCount %d, averageUserRating %.5f; gb userRatingCount %d, averageUserRating %.5f",
		us.ratingCount, us.avgRating, gb.ratingCount, gb.avgRating)
	if us.ratingCount > 0 {
		t.Logf("gb's userRatingCount is %.1f%% of us's", float64(gb.ratingCount)*100/float64(us.ratingCount))
	}
	if len(us.reviews) == 0 && len(gb.reviews) == 0 {
		t.Logf("both storefronts served no reviews: Apple's feed is empty right now, so this run does not answer the country question; the recorded us fixture holds 500 reviews")
	}
	seen := map[string]bool{}
	for _, r := range us.reviews {
		seen[r.ID] = true
	}
	shared := 0
	for _, r := range gb.reviews {
		if seen[r.ID] {
			shared++
		}
	}
	pct := 0.0
	if len(gb.reviews) > 0 {
		pct = float64(shared) * 100 / float64(len(gb.reviews))
	}
	t.Logf("review ids in both us and gb: %d of %d gb reviews (%.1f%%), of %d us reviews",
		shared, len(gb.reviews), pct, len(us.reviews))
	t.Logf("review window: us %s; gb %s", span(us.reviews), span(gb.reviews))
}

// checkAgainstRaw pins the client's read against the raw bodies the feed
// served: same reviews, same order, same fields. It never prints review text.
func checkAgainstRaw(t *testing.T, reviews []Review, entries []map[string]json.RawMessage) {
	t.Helper()
	if len(reviews) != len(entries) {
		t.Fatalf("client returned %d reviews, the feed served %d review entries", len(reviews), len(entries))
	}
	for i, e := range entries {
		r := reviews[i]
		author, _ := nestedLeaf(e, "author", "name")
		want := Review{
			Index:   i + 1,
			ID:      leaf(e, "id"),
			Rating:  atoiTrim(leaf(e, "im:rating")),
			Title:   leaf(e, "title"),
			Body:    leaf(e, "content"),
			Author:  author,
			Version: leaf(e, "im:version"),
			Updated: parseTime(leaf(e, "updated")),
		}
		switch {
		case r.Index != want.Index:
			t.Fatalf("review %d: Index %d, want %d", i, r.Index, want.Index)
		case r.ID != want.ID:
			t.Fatalf("review %d: ID %q, the feed served %q", i, r.ID, want.ID)
		case r.Rating != want.Rating:
			t.Fatalf("review %s: Rating %d, the feed served %d", r.ID, r.Rating, want.Rating)
		case r.Title != want.Title:
			t.Fatalf("review %s: Title differs from the feed (client %d chars, feed %d)", r.ID, len(r.Title), len(want.Title))
		case r.Body != want.Body:
			t.Fatalf("review %s: Body differs from the feed (client %d chars, feed %d)", r.ID, len(r.Body), len(want.Body))
		case r.Author != want.Author:
			t.Fatalf("review %s: Author differs from the feed", r.ID)
		case r.Version != want.Version:
			t.Fatalf("review %s: Version %q, the feed served %q", r.ID, r.Version, want.Version)
		case !r.Updated.Equal(want.Updated):
			t.Fatalf("review %s: Updated %s, the feed served %s", r.ID, stamp(r.Updated), stamp(want.Updated))
		}
	}
	t.Logf("client read pinned against the raw feed bodies: %d review(s), same order, same fields", len(reviews))
}

// fieldAudit counts what the feed's entries hold, over review entries only.
type fieldAudit struct {
	entries    int
	reviews    int
	appEntries int

	missing map[string]int
	empty   map[string]int
	members map[string]bool

	badRating  int
	badUpdated int
	ratings    [6]int
}

// addEntry records one raw feed entry. A content-less entry is the app itself;
// the client skips those, so they are counted apart and left out of the field
// audit.
func (a *fieldAudit) addEntry(e map[string]json.RawMessage) {
	if a.members == nil {
		a.members = map[string]bool{}
		a.missing = map[string]int{}
		a.empty = map[string]int{}
	}
	a.entries++
	for k := range e {
		a.members[k] = true
	}
	if raw, ok := e["author"]; ok {
		var author map[string]json.RawMessage
		if json.Unmarshal(raw, &author) == nil {
			for k := range author {
				a.members["author."+k] = true
			}
		}
	}
	if leaf(e, "content") == "" {
		a.appEntries++
		return
	}
	a.reviews++
	for _, name := range clientMembers {
		var v string
		var ok bool
		if name == "author.name" {
			v, ok = nestedLeaf(e, "author", "name")
		} else {
			_, ok = e[name]
			v = leaf(e, name)
		}
		if !ok {
			a.missing[name]++
			continue
		}
		if strings.TrimSpace(v) == "" {
			a.empty[name]++
		}
	}
	if _, err := strconv.Atoi(strings.TrimSpace(leaf(e, "im:rating"))); err != nil {
		a.badRating++
	} else if n := atoiTrim(leaf(e, "im:rating")); n >= 1 && n <= 5 {
		a.ratings[n]++
	}
	if parseTime(leaf(e, "updated")).IsZero() {
		a.badUpdated++
	}
}

// summary is one line of the audit, for the transcript.
func (a fieldAudit) summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "entries %d, reviews %d, content-less entries %d", a.entries, a.reviews, a.appEntries)
	b.WriteString("; missing:")
	for _, m := range clientMembers {
		fmt.Fprintf(&b, " %s %d", m, a.missing[m])
	}
	b.WriteString("; empty:")
	for _, m := range clientMembers {
		fmt.Fprintf(&b, " %s %d", m, a.empty[m])
	}
	fmt.Fprintf(&b, "; unparsable rating %d, unparsable updated %d", a.badRating, a.badUpdated)
	fmt.Fprintf(&b, "; ratings 1..5: %d %d %d %d %d",
		a.ratings[1], a.ratings[2], a.ratings[3], a.ratings[4], a.ratings[5])
	return b.String()
}

// pageShape is the raw shape of one feed page's entry member.
type pageShape struct {
	shape   string
	entries []map[string]json.RawMessage
}

// decodeShape accepts the feed's entry member as an array, a bare object, null
// or absent, and reports which it was.
func decodeShape(t *testing.T, body []byte) pageShape {
	t.Helper()
	var feed struct {
		Feed struct {
			Entry json.RawMessage `json:"entry"`
		} `json:"feed"`
	}
	if err := json.Unmarshal(body, &feed); err != nil {
		t.Fatalf("decode feed body: %v", err)
	}
	s := strings.TrimSpace(string(feed.Feed.Entry))
	switch {
	case s == "":
		return pageShape{shape: "absent"}
	case s == "null":
		return pageShape{shape: "null"}
	case strings.HasPrefix(s, "["):
		var es []map[string]json.RawMessage
		if err := json.Unmarshal(feed.Feed.Entry, &es); err != nil {
			t.Fatalf("decode entry array: %v", err)
		}
		return pageShape{shape: fmt.Sprintf("array(%d)", len(es)), entries: es}
	case strings.HasPrefix(s, "{"):
		var e map[string]json.RawMessage
		if err := json.Unmarshal(feed.Feed.Entry, &e); err != nil {
			t.Fatalf("decode entry object: %v", err)
		}
		return pageShape{shape: "object(1)", entries: []map[string]json.RawMessage{e}}
	default:
		return pageShape{shape: "not an entry: " + first(s, 24)}
	}
}

func rawSearchHits(t *testing.T, body []byte) []map[string]json.RawMessage {
	t.Helper()
	var out struct {
		Results []map[string]json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode search body: %v", err)
	}
	return out.Results
}

func resultCount(body []byte) int {
	var out struct {
		ResultCount int `json:"resultCount"`
	}
	json.Unmarshal(body, &out)
	return out.ResultCount
}

// uaLabel names the User-Agent the request carried. The client sets no header,
// so the request's own is empty while net/http sends Go's default.
func uaLabel(ua string) string {
	if ua == "" {
		return "unset, net/http sends Go-http-client/1.1"
	}
	return strconv.Quote(ua)
}

// feedUpdated reads the feed skeleton's updated stamp. A stamp near now shows
// the response is generated live, not a stale cached page.
func feedUpdated(body []byte) string {
	var out struct {
		Feed struct {
			Updated struct {
				Label string `json:"label"`
			} `json:"updated"`
		} `json:"feed"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Feed.Updated.Label == "" {
		return "unknown"
	}
	return out.Feed.Updated.Label
}

func trackID(hit map[string]json.RawMessage) int64 {
	var id int64
	json.Unmarshal(hit["trackId"], &id)
	return id
}

func rawInt64(hit map[string]json.RawMessage, name string) int64 {
	var n int64
	json.Unmarshal(hit[name], &n)
	return n
}

func rawFloat64(hit map[string]json.RawMessage, name string) float64 {
	var f float64
	json.Unmarshal(hit[name], &f)
	return f
}

// rawKind says whether a raw member is absent, null or a string.
func rawKind(hit map[string]json.RawMessage, name string) string {
	raw, ok := hit[name]
	if !ok {
		return "absent"
	}
	s := strings.TrimSpace(string(raw))
	switch {
	case s == "null":
		return "null"
	case strings.HasPrefix(s, `"`):
		return "string"
	default:
		return "not a string"
	}
}

// keyDiff names the keys each of two raw hits holds and the other does not.
func keyDiff(a, b map[string]json.RawMessage) (onlyA, onlyB []string) {
	for k := range a {
		if _, ok := b[k]; !ok {
			onlyA = append(onlyA, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			onlyB = append(onlyB, k)
		}
	}
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	return onlyA, onlyB
}

func findApp(apps []App, id int64) (App, bool) {
	for _, a := range apps {
		if a.ID == id {
			return a, true
		}
	}
	return App{}, false
}

func appIDs(apps []App) []string {
	ids := make([]string, 0, len(apps))
	for _, a := range apps {
		ids = append(ids, strconv.FormatInt(a.ID, 10))
	}
	return ids
}

// leaf reads the "label" of one member of a raw feed entry.
func leaf(m map[string]json.RawMessage, name string) string {
	raw, ok := m[name]
	if !ok {
		return ""
	}
	var l struct {
		Label string `json:"label"`
	}
	json.Unmarshal(raw, &l)
	return l.Label
}

// nestedLeaf reads the "label" of a nested member, such as author.name.
func nestedLeaf(m map[string]json.RawMessage, parent, child string) (string, bool) {
	raw, ok := m[parent]
	if !ok {
		return "", false
	}
	var p map[string]json.RawMessage
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", false
	}
	raw, ok = p[child]
	if !ok {
		return "", false
	}
	var l struct {
		Label string `json:"label"`
	}
	if err := json.Unmarshal(raw, &l); err != nil {
		return "", false
	}
	return l.Label, true
}

func atoiTrim(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func memberList(m map[string]bool) string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func unique(ids []string) int {
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	return len(seen)
}

// inversions counts adjacent pairs that are newer than the pair before them.
// Zero times are malformed and are skipped.
func inversions(times []time.Time) int {
	n := 0
	for i := 1; i < len(times); i++ {
		if times[i-1].IsZero() || times[i].IsZero() {
			continue
		}
		if times[i].After(times[i-1]) {
			n++
		}
	}
	return n
}

// span reports the newest and oldest parsable updated stamps.
func span(reviews []Review) string {
	var newest, oldest time.Time
	for _, r := range reviews {
		if r.Updated.IsZero() {
			continue
		}
		if newest.IsZero() || r.Updated.After(newest) {
			newest = r.Updated
		}
		if oldest.IsZero() || r.Updated.Before(oldest) {
			oldest = r.Updated
		}
	}
	if newest.IsZero() {
		return "no parsable dates"
	}
	return fmt.Sprintf("%s .. %s", stamp(newest), stamp(oldest))
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return "zero"
	}
	return t.UTC().Format(time.RFC3339)
}

func listOr(items []string, empty string) string {
	if len(items) == 0 {
		return empty
	}
	return strings.Join(items, ", ")
}

func bytesLabel(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f KiB", float64(n)/1024)
}

func first(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
