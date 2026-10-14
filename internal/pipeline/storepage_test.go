package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nrynss/before-yes/internal/appstore"
	"github.com/nrynss/before-yes/internal/finding"
	"github.com/nrynss/before-yes/internal/tavily"
)

func storePage(t *testing.T, slug string) tavily.Page {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "titles", slug, "tavily-extract-storepage.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ex struct {
		Results []tavily.Page `json:"results"`
	}
	if err := json.Unmarshal(b, &ex); err != nil || len(ex.Results) != 1 {
		t.Fatalf("%s: %v (%d results)", slug, err, len(ex.Results))
	}
	return ex.Results[0]
}

// The T3.8a pages, by title: how many reviews each shows, and the title of
// each in page order, read off the captures by hand.
var pageReviews = map[string][]string{
	"roblox":            {"Close friends suggestion!!", "This is great! I have a few suggestions that might make it even better!", "This game is the best!", "Love❤️ Roblox"},
	"minecraft":         {"Best game ever", "Great game but.. (Mojang you better read this!)", "Awesome game but I have a few important things to say"},
	"khan-academy-kids": {"Wow! Just what my kids needed!", "A+ app for kids!", "The Absolute Best"},
	"toca-life-world":   {"The bug is getting worse", "I can’t play the game", "Toca Boca this is an idea! ^.^"},
}

func TestParseStorePageFixtures(t *testing.T) {
	for slug, titles := range pageReviews {
		p := storePage(t, slug)
		got := ParseStorePage(p.RawContent)
		if len(got) != len(titles) {
			t.Fatalf("%s: %d reviews, want %d: %+v", slug, len(got), len(titles), got)
		}
		for i, r := range got {
			if r.Position != i+1 || r.Title != titles[i] {
				t.Errorf("%s review %d: position %d title %q, want %d %q", slug, i+1, r.Position, r.Title, i+1, titles[i])
			}
			// Verbatim: the body is one line of the page, as it stands.
			if !strings.Contains(p.RawContent, "\n"+r.Body+"\n") {
				t.Errorf("%s review %d: body is not a verbatim line of the page", slug, r.Position)
			}
			for _, bad := range []string{"Developer Response", "Editors’ Choice", "Ratings & Reviews", "What’s New"} {
				if strings.Contains(r.Title, bad) || strings.Contains(r.Body, bad) {
					t.Errorf("%s review %d carries %q", slug, r.Position, bad)
				}
			}
		}
	}
}

// Developer responses, labelled or not, never become or join a review.
func TestParseStorePageDropsResponses(t *testing.T) {
	for _, slug := range []string{"khan-academy-kids", "toca-life-world"} {
		for _, r := range ParseStorePage(storePage(t, slug).RawContent) {
			for _, resp := range []string{"We sincerely appreciate", "It's exciting to hear", "It's so exciting", "Hello there 👋", "Hello! We're here to assist"} {
				if strings.HasPrefix(r.Title, resp) || strings.HasPrefix(r.Body, resp) {
					t.Errorf("%s review %d is a developer response: %.40q", slug, r.Position, r.Title)
				}
			}
		}
	}
	// The editorial blurb is not a review either.
	for _, r := range ParseStorePage(storePage(t, "minecraft").RawContent) {
		if strings.HasPrefix(r.Body, "If you can imagine it") {
			t.Error("the Editors' Choice blurb became a review")
		}
	}
}

func TestParseStorePageEdges(t *testing.T) {
	for name, text := range map[string]string{
		"no section":    "Roblox\nGet\nWhat’s New\nBug fixes",
		"empty section": "Ratings & Reviews\n\nWhat’s New\nBug fixes",
		"only a blurb":  "Ratings & Reviews\nEditors’ Choice\nA fine game.\nEditors’ Choice\nA fine game.\nWhat's New",
	} {
		if got := ParseStorePage(text); len(got) != 0 {
			t.Errorf("%s: %+v", name, got)
		}
	}
	got := ParseStorePage("Ratings & Reviews\nA\nbody a\nA\nbody a\nB\nbody b\nB\nbody b\nA\nbody a\nWhat’s New\nX\ny")
	if len(got) != 2 || got[1].Position != 2 || got[1].Title != "B" {
		t.Errorf("dedupe and positions: %+v", got)
	}
}

// pageSearch answers an extract of the store URL with the T3.8a capture.
type pageSearch struct {
	stubSearch
	url, slug string
	t         *testing.T
	pageReads int
}

func (s *pageSearch) ExtractWithFailures(ctx context.Context, urls []string) (tavily.Extracted, error) {
	if len(urls) == 1 && urls[0] == s.url {
		s.pageReads++
		p := storePage(s.t, s.slug)
		p.URL = s.url
		return tavily.Extracted{Pages: []tavily.Page{p}, Credits: tavily.Credits{Used: 0, Reported: true}}, nil
	}
	return s.stubSearch.ExtractWithFailures(ctx, urls)
}

type feedStub struct {
	apps    []appstore.App
	reviews []appstore.Review
	err     error
}

func (s feedStub) Find(context.Context, string, int) ([]appstore.App, error) { return s.apps, nil }
func (s feedStub) Reviews(context.Context, int64, int) ([]appstore.Review, error) {
	return s.reviews, s.err
}
func (s feedStub) FeedURL(id int64, page int) string { return "https://itunes.apple.com/feed" }

func robloxApp() appstore.App {
	return appstore.App{ID: 431946152, Name: "Roblox", Seller: "Roblox Corporation", SellerURL: "https://www.roblox.com",
		StoreURL: "https://apps.apple.com/us/app/roblox/id431946152?uo=4"}
}

// An empty feed falls back to the page: one review document per review,
// kind review, the page URL as source and the position as index, and a
// finding quoted from one passes finding.Verify.
func TestGatherFallsBackToStorePage(t *testing.T) {
	a := robloxApp()
	s := &pageSearch{url: a.StoreURL, slug: "roblox", t: t}
	g := &Gatherer{Store: feedStub{apps: []appstore.App{a}}, Search: s, Meter: openTestMeter(t)}
	got, err := g.Gather(context.Background(), "Roblox", "c")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReviewSource != ReviewsFromPage || s.pageReads != 1 {
		t.Fatalf("review source %q, page reads %d", got.ReviewSource, s.pageReads)
	}
	var reviews []Doc
	for _, d := range got.Docs {
		if d.Source.Kind == KindReview {
			reviews = append(reviews, d)
		}
	}
	if len(reviews) != 4 {
		t.Fatalf("%d review documents, want 4", len(reviews))
	}
	for i, d := range reviews {
		if d.Source.URL != a.StoreURL || d.Source.Index != i+1 || d.Source.Title != pageReviews["roblox"][i] {
			t.Errorf("doc %d source %+v", i+1, d.Source)
		}
	}
	if contains(got.Missing, KindReview) {
		t.Errorf("missing %v", got.Missing)
	}
	segs := map[string]finding.Segment{"R4": {Text: reviews[3].Text, Source: reviews[3].Source}}
	fs, _ := finding.Verify([]finding.Raw{{Ref: "R4", Area: "money", Stance: "risk", Claim: "Robux cost real money",
		Quote: "you have to get Robux for real money and it’s unfair that you have to pay for Robux"}}, segs)
	if len(fs) != 1 || fs[0].Source.Index != 4 || fs[0].Source.URL != a.StoreURL {
		t.Errorf("verified %+v", fs)
	}
}

// User decision: the page stands in when the feed is empty or errors, and
// only then.
func TestStorePageFallbackOnEmptyOrError(t *testing.T) {
	a := robloxApp()
	// A feed with reviews: no page read, source feed.
	s := &pageSearch{url: a.StoreURL, slug: "roblox", t: t}
	g := &Gatherer{Store: feedStub{apps: []appstore.App{a}, reviews: []appstore.Review{{Index: 1, Title: "t", Body: "b"}}}, Search: s, Meter: openTestMeter(t)}
	got, _ := g.Gather(context.Background(), "Roblox", "c")
	if s.pageReads != 0 || got.ReviewSource != ReviewsFromFeed {
		t.Errorf("feed with reviews: page reads %d, source %q", s.pageReads, got.ReviewSource)
	}
	// A feed error: the page is read and gives the reviews, source page;
	// the failed call is still recorded, so the evidence is not cached.
	s = &pageSearch{url: a.StoreURL, slug: "roblox", t: t}
	g = &Gatherer{Store: feedStub{apps: []appstore.App{a}, err: errors.New("503")}, Search: s, Meter: openTestMeter(t)}
	got, _ = g.Gather(context.Background(), "Roblox", "c")
	if s.pageReads != 1 || got.ReviewSource != ReviewsFromPage || !contains(got.Failures, "review_feed") || contains(got.Missing, KindReview) {
		t.Errorf("feed error: page reads %d, source %q, failures %v, missing %v", s.pageReads, got.ReviewSource, got.Failures, got.Missing)
	}
	n := 0
	for _, d := range got.Docs {
		if d.Source.Kind == KindReview && d.Source.URL == a.StoreURL {
			n++
		}
	}
	if n != 4 || !strings.Contains(strings.Join(got.Notes, "\n"), "review feed failed (503); reading the App Store page") {
		t.Errorf("feed error: %d page reviews, notes %q", n, got.Notes)
	}
}

// A feed error and a page with no reviews: reviews are missing, the reason
// names both, and no source is claimed.
func TestFeedErrorAndEmptyPageIsMissing(t *testing.T) {
	a := robloxApp()
	s := &stubSearch{pages: map[string]string{a.StoreURL: "Roblox\nGet\nWhat’s New\nBug fixes"}}
	g := &Gatherer{Store: feedStub{apps: []appstore.App{a}, err: errors.New("503")}, Search: s, Meter: openTestMeter(t)}
	got, err := g.Gather(context.Background(), "Roblox", "c")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReviewSource != "" || !contains(got.Missing, KindReview) || !contains(got.Failures, "review_feed") {
		t.Errorf("source %q missing %v failures %v", got.ReviewSource, got.Missing, got.Failures)
	}
	if !strings.Contains(strings.Join(got.Notes, "\n"), "App Store review feed failed: 503; ") {
		t.Errorf("notes %q", got.Notes)
	}
}

// A page with no reviews yields no documents and a missing source.
func TestStorePageWithoutReviewsIsMissing(t *testing.T) {
	a := robloxApp()
	s := &stubSearch{pages: map[string]string{a.StoreURL: "Roblox\nGet\nWhat’s New\nBug fixes"}}
	g := &Gatherer{Store: feedStub{apps: []appstore.App{a}}, Search: s, Meter: openTestMeter(t)}
	got, err := g.Gather(context.Background(), "Roblox", "c")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range got.Docs {
		if d.Source.Kind == KindReview {
			t.Errorf("review doc from a page with none: %+v", d.Source)
		}
	}
	if !contains(got.Missing, KindReview) || !strings.Contains(strings.Join(got.Notes, "\n"), "App Store page shows no reviews") || got.ReviewSource != "" {
		t.Errorf("missing %v notes %q source %q", got.Missing, got.Notes, got.ReviewSource)
	}
	if len(got.Failures) != 0 && contains(got.Failures, "store_page") {
		t.Errorf("an absent review section is not a failure: %v", got.Failures)
	}
	// An unreadable page is a failure.
	s = &stubSearch{pages: map[string]string{}}
	g = &Gatherer{Store: feedStub{apps: []appstore.App{a}}, Search: s, Meter: openTestMeter(t)}
	got, _ = g.Gather(context.Background(), "Roblox", "c")
	if !contains(got.Failures, "store_page") {
		t.Errorf("unreadable page failures %v", got.Failures)
	}
}

// The round-1 attack cases: page chrome, multi-line bodies, responses and
// blurbs, and a review titled like a label.
func TestParseStorePageStructure(t *testing.T) {
	page := func(lines ...string) string {
		return "Roblox\nRatings & Reviews\n" + strings.Join(lines, "\n") + "\nWhat’s New\nBug fixes"
	}
	type rv struct{ title, body string }
	for name, c := range map[string]struct {
		text string
		want []rv
	}{
		"rating summary": {page("4.5 out of 5", "1.2M Ratings", "A", "body a", "A", "body a", "B", "body b", "B", "body b"),
			[]rv{{"A", "body a"}, {"B", "body b"}}},
		"one aggregate line": {page("4.5 out of 5", "A", "body a", "A", "body a", "B", "body b", "B", "body b"),
			[]rv{{"A", "body a"}, {"B", "body b"}}},
		"two-line body": {page("A", "body a1", "body a2", "A", "body a1", "body a2", "B", "body b", "B", "body b"),
			[]rv{{"A", "body a1\nbody a2"}, {"B", "body b"}}},
		"two-line response": {page("A", "body a", "thanks", "more thanks", "A", "body a", "Developer Response 01/30/2023", "thanks", "more thanks", "B", "body b", "B", "body b"),
			[]rv{{"A", "body a"}, {"B", "body b"}}},
		"two-line blurb": {page("Editors’ Choice", "blurb1", "blurb2", "Editors’ Choice", "blurb1", "blurb2", "A", "body a", "A", "body a"),
			[]rv{{"A", "body a"}}},
		"see all": {page("A", "body a", "A", "body a", "See All", "B", "body b", "B", "body b", "See All"),
			[]rv{{"A", "body a"}, {"B", "body b"}}},
		"review titled like a label": {page("Developer Response is slow", "they never answer my kid was contacted", "Developer Response is slow", "they never answer my kid was contacted"),
			[]rv{{"Developer Response is slow", "they never answer my kid was contacted"}}},
		"response dated by month": {page("A", "body a", "hi", "A", "body a", "Developer Response Jun 22", "hi"),
			[]rv{{"A", "body a"}}},
	} {
		got := ParseStorePage(c.text)
		var have []rv
		for i, r := range got {
			if r.Position != i+1 {
				t.Errorf("%s: position %d at %d", name, r.Position, i+1)
			}
			have = append(have, rv{r.Title, r.Body})
		}
		if fmt.Sprint(have) != fmt.Sprint(c.want) {
			t.Errorf("%s: got %q, want %q", name, have, c.want)
		}
	}
}
