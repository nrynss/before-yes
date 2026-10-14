package pipeline

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/nrynss/before-yes/internal/appstore"
	"github.com/nrynss/before-yes/internal/tavily"
)

// kindSearch is a fixture for one kind: canned hits by query suffix, every
// search recorded, every extract failing (no page bodies are needed to
// tell the plan apart).
type kindSearch struct {
	mu       sync.Mutex
	hits     map[string][]tavily.Result // by plan query name
	plan     []Query
	searches []tavily.Search
}

func (s *kindSearch) Search(_ context.Context, q tavily.Search) ([]tavily.Result, tavily.Credits, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.searches = append(s.searches, q)
	for _, p := range s.plan {
		if p.Search.Query == q.Query {
			return s.hits[p.Name], tavily.Credits{Used: 1, Reported: true}, nil
		}
	}
	return nil, tavily.Credits{Used: 1, Reported: true}, nil
}

func (s *kindSearch) ExtractWithFailures(_ context.Context, urls []string) (tavily.Extracted, error) {
	var ex tavily.Extracted
	for _, u := range urls {
		ex.Failed = append(ex.Failed, tavily.Failed{URL: u, Error: "offline"})
	}
	return ex, nil
}

func (s *kindSearch) sent() []string {
	var out []string
	for _, q := range s.searches {
		for _, p := range s.plan {
			if p.Search.Query == q.Query {
				out = append(out, p.Name)
				break
			}
		}
	}
	slices.Sort(out)
	return out
}

func (feedStub) Storefront() string { return "us" }

func hit(u, title, content string) tavily.Result {
	return tavily.Result{URL: u, Title: title, Content: content}
}

// One fixture per kind. Each names the hits that decide it and the plan
// the gatherer must send.
var kindFixtures = []struct {
	kind  Kind
	title string
	app   *appstore.App
	hits  map[string][]tavily.Result
	want  []string
}{
	{KindIOS, "Khan Academy Kids", &appstore.App{ID: 1, Name: "Khan Academy Kids", Seller: "Khan Academy", SellerURL: "https://khankids.org", StoreURL: "https://apps.apple.com/us/app/id1"},
		nil, []string{"forum", "news", "policy", "rating", "reddit-community", "reddit-parents", "terms"}},
	{KindAndroid, "Sample Android Game", nil, map[string][]tavily.Result{
		"rating": {hit("https://play.google.com/store/apps/details?id=com.sample", "Sample Android Game - Apps on Google Play", "Rated for 3+ on Google Play. Android only."),
			hit("https://www.example-reviews.com/sample", "Sample Android Game review", "An Android game for kids.")},
	}, []string{"forum", "news", "policy", "rating", "reddit-community", "reddit-parents", "terms"}},
	{KindConsole, "Sample Console Game", nil, map[string][]tavily.Result{
		"rating": {hit("https://www.esrb.org/ratings/40000/sample-console-game/", "Sample Console Game | ESRB", "Rated E10+. Platforms: PlayStation 5, Xbox Series, Nintendo Switch. In-Game Purchases, Users Interact."),
			hit("https://pegi.info/search-pegi?q=sample", "PEGI 7", "PEGI 7 for PC game and console.")},
	}, []string{"forum", "news", "policy", "rating", "rating-boards", "reddit-community", "reddit-parents", "terms"}},
	{KindYouTube, "Sample Kids Channel", nil, map[string][]tavily.Result{
		"rating": {hit("https://www.youtube.com/@SampleKidsChannel", "Sample Kids Channel - YouTube", "12M subscribers. A YouTube channel for kids."),
			hit("https://www.example-news.com/youtuber", "The YouTuber behind Sample Kids Channel", "The channel has millions of subscribers.")},
	}, []string{"channel", "forum", "news", "policy", "rating", "reddit-community", "reddit-parents"}},
}

func TestKindsGatherTheirPlan(t *testing.T) {
	for _, c := range kindFixtures {
		t.Run(string(c.kind), func(t *testing.T) {
			s := &kindSearch{hits: c.hits, plan: append(append(PlanFor(c.kind, c.title, ""), PlanFor(c.kind, c.title, "khankids.org")...), Plan(c.title, "")...)}
			var apps []appstore.App
			if c.app != nil {
				apps = []appstore.App{*c.app}
			}
			g := &Gatherer{Store: feedStub{apps: apps}, Search: s, Meter: openTestMeter(t)}
			got, err := g.Gather(context.Background(), c.title, "c")
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != c.kind {
				t.Fatalf("kind %q, want %q", got.Kind, c.kind)
			}
			if sent := s.sent(); strings.Join(sent, ",") != strings.Join(c.want, ",") {
				t.Errorf("searches %v, want %v", sent, c.want)
			}
			for _, q := range s.searches {
				switch c.kind {
				case KindAndroid:
					if len(q.IncludeDomains) == 0 && !slices.Contains(q.ExcludeDomains, "play.google.com") && !strings.HasSuffix(q.Query, "privacy policy") && !strings.HasSuffix(q.Query, "parental controls") {
						// The policy and rating searches run before the kind
						// is known; their Play hits are dropped afterwards.
						t.Errorf("Android search %q does not exclude Google Play", q.Query)
					}
				case KindConsole:
					if strings.Contains(q.Query, "content descriptors") && strings.Join(q.IncludeDomains, ",") != "esrb.org,pegi.info" {
						t.Errorf("rating-board search domains %v", q.IncludeDomains)
					}
				}
			}
			for _, d := range got.Docs {
				if strings.Contains(d.Source.URL, "play.google.com") {
					t.Errorf("a Play Store page was read: %s", d.Source.URL)
				}
			}
			if c.kind == KindYouTube && (contains(got.Missing, KindPolicy) || contains(got.Missing, KindTerms)) {
				t.Errorf("a channel's policy and terms are not its own, so not missing: %v", got.Missing)
			}
		})
	}
}

func TestDetectKind(t *testing.T) {
	if k := DetectKind("Nothing Known", nil); k != KindConsole {
		t.Errorf("no signal: %q, want console_pc", k)
	}
	if k := DetectKind("Sample YouTube Kids", nil); k != KindYouTube {
		t.Errorf("YouTube in the title: %q", k)
	}
	// An ESRB page that lists a mobile platform stays a console title.
	if k := DetectKind("X", []tavily.Result{hit("https://www.esrb.org/ratings/1/x/", "X | ESRB", "Platforms: PlayStation 4, Xbox One, Android")}); k != KindConsole {
		t.Errorf("ESRB page: %q", k)
	}
}

// The card shows the kind the gatherer found.
func TestCardShowsKind(t *testing.T) {
	c := kindFixtures[3]
	c.hits["news"] = []tavily.Result{{URL: "https://www.example-news.com/a", Title: "Channel news", RawContent: "Parents worry about the Sample Kids Channel comments section and its sponsored toy videos aimed at kids."}}
	s := &kindSearch{hits: c.hits, plan: append(PlanFor(c.kind, c.title, ""), Plan(c.title, "")...)}
	m := openTestMeter(t)
	p := &Pipeline{
		Gatherer: &Gatherer{Store: feedStub{}, Search: s, Meter: m},
		Reader:   &Reader{Model: &recorder{price: 1}, Meter: m},
		Judge:    &Judge{Model: &ruleJudge{price: 1}, Meter: m},
	}
	ev, _, err := p.Collect(context.Background(), c.title, "c")
	if err != nil {
		t.Fatal(err)
	}
	card, err := p.Decide(context.Background(), ev, 9, "c")
	if err != nil {
		t.Fatal(err)
	}
	if ev.Kind != KindYouTube || card.Kind != KindYouTube {
		t.Errorf("evidence kind %q, card kind %q", ev.Kind, card.Kind)
	}
}

// The four captured titles are all on the App Store, Toca Life World by
// its policy hit, so all four are iOS and gather Plan unchanged.
func TestFixtureTitlesAreIOS(t *testing.T) {
	for _, d := range demos {
		_, srv := newFixture(t, d)
		store, tc := fixtureClients(t, srv)
		g := &Gatherer{Store: store, Search: tc, Meter: openTestMeter(t)}
		got, err := g.Gather(context.Background(), d.title, "c")
		if err != nil {
			t.Fatal(err)
		}
		if got.Kind != KindIOS {
			t.Errorf("%s: kind %q", d.title, got.Kind)
		}
	}
}

// The round-1 attack cases: whole words only, and a direct listing beats
// mentions on a review page; video pages count for YouTube.
func TestDetectKindStructured(t *testing.T) {
	review := hit("https://www.example-reviews.com/some-app", "Some App review",
		"Not on console, PC game stores, Xbox, PlayStation, Steam or Nintendo Switch.")
	for name, c := range map[string]struct {
		title string
		hits  []tavily.Result
		want  Kind
	}{
		"Play listing beats a platform list": {"Some App", []tavily.Result{
			hit("https://play.google.com/store/apps/details?id=x", "Some App - Apps on Google Play", "Android"), review}, KindAndroid},
		"steam inside other words": {"Some App", []tavily.Result{
			hit("https://news.example.com/a", "Some App livestream", "mainstream streamers love the Android app")}, KindAndroid},
		"video pages are YouTube": {"MrBeast", []tavily.Result{hit("https://www.youtube.com/watch?v=1", "MrBeast video", "")}, KindYouTube},
		"- YouTube titles":        {"Sample", []tavily.Result{hit("https://example.com/x", "Sample Kids Songs - YouTube", "")}, KindYouTube},
		"Steam listing":           {"Some Game", []tavily.Result{hit("https://store.steampowered.com/app/123/Some_Game/", "Some Game on Steam", ""), hit("https://example.com", "Some Game for Android fans", "android")}, KindConsole},
		"Nintendo store":          {"Some Game", []tavily.Result{hit("https://www.nintendo.com/us/store/products/some-game-switch/", "Some Game", "")}, KindConsole},
		"PlayStation store":       {"Some Game", []tavily.Result{hit("https://store.playstation.com/en-us/concept/1", "Some Game", "")}, KindConsole},
	} {
		if got := DetectKind(c.title, c.hits); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

// The evidence key moves with any kind's plan, not just the iOS one.
func TestGatherVersionCoversEveryKind(t *testing.T) {
	saved := ratingBoards
	defer func() { ratingBoards = saved }()
	before := GatherVersion()
	ratingBoards = []string{"esrb.org"}
	if GatherVersion() == before {
		t.Error("a console plan change did not change GatherVersion")
	}
}
