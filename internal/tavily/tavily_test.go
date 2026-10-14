package tavily

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// readFixture loads one recorded T0.4 response.
func readFixture(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "titles", rel))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// stopWaits removes every real wait from a client's retry loop without
// touching its attempt budgets. Injecting Sleep also keeps the client off the
// process-wide pacer.
func stopWaits(c *Client) {
	c.Retry.Backoff = time.Millisecond
	c.Retry.Max = time.Millisecond
	c.Retry.PerMinute = 0
	c.Retry.Name = ""
	c.Retry.Sleep = func(context.Context, time.Duration) error { return nil }
}

// newTestClient points a client at base and removes every real wait.
func newTestClient(base string) *Client {
	c := New(base, func() (string, error) { return "test-key", nil })
	stopWaits(c)
	return c
}

// fixtureClient serves one recorded response as the API's answer.
func fixtureClient(t *testing.T, body []byte) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return newTestClient(srv.URL)
}

// recorded is what one request carried, read from outside the client.
type recorded struct {
	method string
	path   string
	auth   string
	ctype  string
	body   map[string]any
}

// stub is an httptest server that answers every request with the same status
// and body, and records what arrived.
type stub struct {
	*httptest.Server

	status int
	body   []byte

	mu   sync.Mutex
	reqs []recorded
}

func newStub(t *testing.T, status int, body []byte) *stub {
	t.Helper()
	s := &stub{status: status, body: body}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got map[string]any
		json.NewDecoder(r.Body).Decode(&got)
		s.mu.Lock()
		s.reqs = append(s.reqs, recorded{
			method: r.Method,
			path:   r.URL.Path,
			auth:   r.Header.Get("Authorization"),
			ctype:  r.Header.Get("Content-Type"),
			body:   got,
		})
		s.mu.Unlock()
		if s.status != 0 {
			w.WriteHeader(s.status)
		}
		if s.body != nil {
			w.Write(s.body)
		}
	}))
	t.Cleanup(s.Server.Close)
	return s
}

// calls is how many requests arrived.
func (s *stub) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reqs)
}

// last is the most recent request.
func (s *stub) last() recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reqs) == 0 {
		return recorded{}
	}
	return s.reqs[len(s.reqs)-1]
}

// searchCase pins what one recorded search response decodes to. The counts of
// hits with raw content and with a published date are the point of the table:
// those two fields are read downstream, and a wrong json tag would drop them
// while the first-hit values below pin the rest.
type searchCase struct {
	name       string
	file       string
	results    int
	rawContent int
	published  int
	firstTitle string
	firstURL   string
	firstScore float64
}

var searchCases = []searchCase{
	{name: "khan-academy-kids/csm", file: "khan-academy-kids/tavily-search-csm.json", results: 5, rawContent: 0, published: 0, firstTitle: "Khan Academy Kids - Parents' Guide & App Review | Common Sense Media", firstURL: "https://www.commonsensemedia.org/app-reviews/khan-academy-kids", firstScore: 0.89030397},
	{name: "khan-academy-kids/forum", file: "khan-academy-kids/tavily-search-forum.json", results: 8, rawContent: 5, published: 0, firstTitle: "Khan Academy Kids - Parents' Guide & App Review | Common Sense Media", firstURL: "https://www.commonsensemedia.org/app-reviews/khan-academy-kids", firstScore: 0.49231076},
	{name: "khan-academy-kids/news", file: "khan-academy-kids/tavily-search-news.json", results: 1, rawContent: 0, published: 1, firstTitle: "Weight Watchers diet app collected data on kids as young as 8, FTC says", firstURL: "https://www.cbsnews.com/news/weight-watchers-diet-kids-ftc", firstScore: 0.15870851},
	{name: "khan-academy-kids/policy", file: "khan-academy-kids/tavily-search-policy.json", results: 1, rawContent: 0, published: 0, firstTitle: "Free Learning App for Kids Ages 2-8", firstURL: "https://www.khankids.org", firstScore: 0.51442534},
	{name: "khan-academy-kids/rating", file: "khan-academy-kids/tavily-search-rating.json", results: 6, rawContent: 5, published: 0, firstTitle: "‎Khan Academy Kids App - App Store", firstURL: "https://apps.apple.com/mx/app/khan-academy-kids/id1378467217?l=en-GB", firstScore: 0.76826453},
	{name: "khan-academy-kids/reddit-community", file: "khan-academy-kids/tavily-search-reddit-community.json", results: 8, rawContent: 0, published: 0, firstTitle: "Khan Academy still has basic explanations on taxes in the ...", firstURL: "https://www.reddit.com/r/personalfinance/comments/5n4xhp/reminder_khan_academy_still_has_basic", firstScore: 0.19794047},
	{name: "khan-academy-kids/reddit-parents", file: "khan-academy-kids/tavily-search-reddit-parents.json", results: 8, rawContent: 0, published: 0, firstTitle: "Khan academy is struggling with rising demand during ...", firstURL: "https://www.reddit.com/r/videos/comments/got5ih/khan_academy_is_struggling_with_rising_demand", firstScore: 0.55362654},
	{name: "khan-academy-kids/terms", file: "khan-academy-kids/tavily-search-terms.json", results: 1, rawContent: 0, published: 0, firstTitle: "Free Learning App for Kids Ages 2-8", firstURL: "https://www.khankids.org", firstScore: 0.050578702},
	{name: "minecraft/csm", file: "minecraft/tavily-search-csm.json", results: 5, rawContent: 0, published: 0, firstTitle: "Minecraft: The Island - Parents' Guide & Book Review | Common Sense Media", firstURL: "https://www.commonsensemedia.org/book-reviews/minecraft-the-island", firstScore: 0.8252664},
	{name: "minecraft/forum", file: "minecraft/tavily-search-forum.json", results: 8, rawContent: 5, published: 0, firstTitle: "Parents' Ultimate Guide to Minecraft", firstURL: "https://www.commonsensemedia.org/articles/parents-ultimate-guide-to-minecraft", firstScore: 0.5183761},
	{name: "minecraft/news", file: "minecraft/tavily-search-news.json", results: 8, rawContent: 6, published: 6, firstTitle: "Instagram", firstURL: "https://www.instagram.com/p/DdsDQQnCo07", firstScore: 0.28557163},
	{name: "minecraft/policy", file: "minecraft/tavily-search-policy.json", results: 5, rawContent: 0, published: 0, firstTitle: "Minecraft Usage Guidelines for Fans and Creators | Minecraft | Minecraft", firstURL: "https://www.minecraft.net/en-us/usage-guidelines", firstScore: 0.8524574},
	{name: "minecraft/rating", file: "minecraft/tavily-search-rating.json", results: 6, rawContent: 6, published: 0, firstTitle: "Minecraft Parents’ Guide  | Minecraft", firstURL: "https://www.minecraft.net/en-us/article/parents--guide-minecraft", firstScore: 0.87113565},
	{name: "minecraft/reddit-community", file: "minecraft/tavily-search-reddit-community.json", results: 8, rawContent: 0, published: 0, firstTitle: "A security flaw allows any Minecraft account to be banned ...", firstURL: "https://www.reddit.com/r/PhoenixSC/comments/1uby2e2/a_security_flaw_allows_any_minecraft_account_to", firstScore: 0.7905882124732908},
	{name: "minecraft/reddit-parents", file: "minecraft/tavily-search-reddit-parents.json", results: 8, rawContent: 0, published: 0, firstTitle: "I am a parent who does not play anything. My 7 year old ...", firstURL: "https://www.reddit.com/r/Minecraft/comments/1qfz5go/i_am_a_parent_who_does_not_play_anything_my_7", firstScore: 0.5849357},
	{name: "minecraft/terms", file: "minecraft/tavily-search-terms.json", results: 5, rawContent: 0, published: 0, firstTitle: "Terms of Use | Official Minecraft Shop", firstURL: "https://minecraftshop.com/pages/terms-of-use", firstScore: 0.77893573},
	{name: "roblox/csm", file: "roblox/tavily-search-csm.json", results: 5, rawContent: 0, published: 0, firstTitle: "Parents' Ultimate Guide to Roblox | Common Sense Media", firstURL: "https://www.commonsensemedia.org/articles/parents-ultimate-guide-to-roblox", firstScore: 0.7591723},
	{name: "roblox/forum", file: "roblox/tavily-search-forum.json", results: 8, rawContent: 4, published: 0, firstTitle: "Parents' Ultimate Guide to Roblox | Common Sense Media", firstURL: "https://www.commonsensemedia.org/articles/parents-ultimate-guide-to-roblox", firstScore: 0.68344116},
	{name: "roblox/news", file: "roblox/tavily-search-news.json", results: 7, rawContent: 5, published: 7, firstTitle: "Lawsuit Alleges Roblox Tracks Children’s Data Without Parental Consent | Security Magazine", firstURL: "https://www.securitymagazine.com/articles/101628-lawsuit-alleges-roblox-tracks-childrens-data-without-parental-consent", firstScore: 0.73642004},
	{name: "roblox/policy", file: "roblox/tavily-search-policy.json", results: 3, rawContent: 0, published: 0, firstTitle: "Download Roblox", firstURL: "https://www.roblox.com/download", firstScore: 0.47066417},
	{name: "roblox/rating", file: "roblox/tavily-search-rating.json", results: 6, rawContent: 6, published: 0, firstTitle: "Parents' Guide: Roblox (PEGI 7 / ESRB 10+)", firstURL: "https://www.youtube.com/watch?v=kv6zvEf3XCA", firstScore: 0.9073207},
	{name: "roblox/reddit-community", file: "roblox/tavily-search-reddit-community.json", results: 8, rawContent: 0, published: 0, firstTitle: "Question regarding reports for scams. : r/ROBLOXBans - Reddit", firstURL: "https://www.reddit.com/r/ROBLOXBans/comments/1nn3l5j/question_regarding_reports_for_scams", firstScore: 0.7393665},
	{name: "roblox/reddit-parents", file: "roblox/tavily-search-reddit-parents.json", results: 8, rawContent: 0, published: 0, firstTitle: "Parents should monitor children 24/7 on Roblox, says developer : r/technology", firstURL: "https://www.reddit.com/r/technology/comments/1s43p87/parents_should_monitor_children_247_on_roblox", firstScore: 0.8156003},
	{name: "roblox/terms", file: "roblox/tavily-search-terms.json", results: 5, rawContent: 0, published: 0, firstTitle: "Roblox is Updating our Terms of Use – Roblox Support", firstURL: "https://en.help.roblox.com/hc/en-us/articles/14663412502676-Roblox-is-Updating-our-Terms-of-Use", firstScore: 0.83678174},
	{name: "toca-life-world/csm", file: "toca-life-world/tavily-search-csm.json", results: 5, rawContent: 0, published: 0, firstTitle: "Toca Life World: Build Stories - Parents' Guide & App Review | Common Sense Media", firstURL: "https://www.commonsensemedia.org/app-reviews/toca-life-world-build-stories", firstScore: 0.8464638},
	{name: "toca-life-world/forum", file: "toca-life-world/tavily-search-forum.json", results: 8, rawContent: 2, published: 0, firstTitle: "Parent reviews for Toca Life World: Build Stories | Common Sense Media", firstURL: "https://www.commonsensemedia.org/app-reviews/toca-life-world-build-stories/user-reviews/adult", firstScore: 0.60610276},
	{name: "toca-life-world/news", file: "toca-life-world/tavily-search-news.json", results: 8, rawContent: 8, published: 8, firstTitle: "FTC sues transgender care group, arguing it deceived parents and patients - Politico", firstURL: "https://www.politico.com/news/2026/06/17/ftc-sues-transgender-care-group-arguing-it-deceived-parents-and-patients-00965962", firstScore: 0.2570865},
	{name: "toca-life-world/policy", file: "toca-life-world/tavily-search-policy.json", results: 5, rawContent: 0, published: 0, firstTitle: "Privacy Policy - Toca Boca", firstURL: "https://www.tocaboca.com/docs/en/privacy-policy", firstScore: 0.76427686},
	{name: "toca-life-world/rating", file: "toca-life-world/tavily-search-rating.json", results: 6, rawContent: 6, published: 0, firstTitle: "About Ratings and Parental Controls (US)", firstURL: "https://www.playstation.com/en-us/legal/ratings", firstScore: 0.5325473},
	{name: "toca-life-world/reddit-community", file: "toca-life-world/tavily-search-reddit-community.json", results: 8, rawContent: 0, published: 0, firstTitle: "Attempted to be scammed on discord, or a real person ...", firstURL: "https://www.reddit.com/r/Scams/comments/1obci5t/attempted_to_be_scammed_on_discord_or_a_real", firstScore: 0.08943091},
	{name: "toca-life-world/reddit-parents", file: "toca-life-world/tavily-search-reddit-parents.json", results: 8, rawContent: 0, published: 0, firstTitle: "Toca World should have a monthly subscription with gifts", firstURL: "https://www.reddit.com/r/tocaboca/comments/1t2ue21/toca_world_should_have_a_monthly_subscription", firstScore: 0.6565047},
	{name: "toca-life-world/terms", file: "toca-life-world/tavily-search-terms.json", results: 5, rawContent: 0, published: 0, firstTitle: "Terms of Service - Toca Boca", firstURL: "https://www.tocaboca.com/docs/en/terms-of-service", firstScore: 0.72254705},
}

// extractCase pins what one recorded extract response decodes to.
type extractCase struct {
	name        string
	file        string
	pages       int
	rawPages    int
	failed      int
	failedError string
	credits     float64
	firstURL    string
	firstTitle  string
}

var extractCases = []extractCase{
	{name: "khan-academy-kids/extract-csm-advanced", file: "khan-academy-kids/tavily-extract-csm-advanced.json", pages: 2, rawPages: 2, failed: 0, failedError: "", credits: 1.0, firstURL: "https://www.commonsensemedia.org/app-reviews/khan-academy-kids/user-reviews/adult", firstTitle: "Parent reviews for Khan Academy Kids | Common Sense Media"},
	{name: "khan-academy-kids/extract-csm", file: "khan-academy-kids/tavily-extract-csm.json", pages: 2, rawPages: 2, failed: 0, failedError: "", credits: 0.0, firstURL: "https://www.commonsensemedia.org/app-reviews/khan-academy-kids/user-reviews/child", firstTitle: "Kid reviews for Khan Academy Kids | Common Sense Media"},
	{name: "khan-academy-kids/extract-reddit-advanced", file: "khan-academy-kids/tavily-extract-reddit-advanced.json", pages: 0, rawPages: 0, failed: 2, failedError: "Failed to fetch url", credits: 1.0, firstURL: "", firstTitle: ""},
	{name: "khan-academy-kids/extract", file: "khan-academy-kids/tavily-extract.json", pages: 0, rawPages: 0, failed: 2, failedError: "Error fetching content", credits: 0.0, firstURL: "", firstTitle: ""},
	{name: "minecraft/extract-csm-advanced", file: "minecraft/tavily-extract-csm-advanced.json", pages: 2, rawPages: 2, failed: 0, failedError: "", credits: 1.0, firstURL: "https://www.commonsensemedia.org/app-reviews/minecraft/user-reviews/child", firstTitle: "Page Not Found | Common Sense Media"},
	{name: "minecraft/extract-reddit-advanced", file: "minecraft/tavily-extract-reddit-advanced.json", pages: 0, rawPages: 0, failed: 2, failedError: "Failed to fetch url", credits: 1.0, firstURL: "", firstTitle: ""},
	{name: "minecraft/extract", file: "minecraft/tavily-extract.json", pages: 2, rawPages: 2, failed: 0, failedError: "", credits: 0.0, firstURL: "https://www.minecraft.net/en-us/usage-guidelines", firstTitle: "Minecraft Usage Guidelines for Fans and Creators | Minecraft | Minecraft"},
	{name: "roblox/extract-csm-advanced", file: "roblox/tavily-extract-csm-advanced.json", pages: 2, rawPages: 2, failed: 0, failedError: "", credits: 1.0, firstURL: "https://www.commonsensemedia.org/website-reviews/roblox/user-reviews/child", firstTitle: "Page Not Found | Common Sense Media"},
	{name: "roblox/extract-reddit-advanced", file: "roblox/tavily-extract-reddit-advanced.json", pages: 0, rawPages: 0, failed: 2, failedError: "Failed to fetch url", credits: 1.0, firstURL: "", firstTitle: ""},
	{name: "roblox/extract", file: "roblox/tavily-extract.json", pages: 2, rawPages: 2, failed: 0, failedError: "", credits: 1.0, firstURL: "https://www.roblox.com/download", firstTitle: "Download Roblox"},
	{name: "toca-life-world/extract-csm-advanced", file: "toca-life-world/tavily-extract-csm-advanced.json", pages: 2, rawPages: 2, failed: 0, failedError: "", credits: 1.0, firstURL: "https://www.commonsensemedia.org/app-reviews/toca-life-world-build-stories/user-reviews/adult", firstTitle: "Parent reviews for Toca Life World: Build Stories | Common Sense Media"},
	{name: "toca-life-world/extract-reddit-advanced", file: "toca-life-world/tavily-extract-reddit-advanced.json", pages: 0, rawPages: 0, failed: 2, failedError: "Failed to fetch url", credits: 0.0, firstURL: "", firstTitle: ""},
	{name: "toca-life-world/extract", file: "toca-life-world/tavily-extract.json", pages: 2, rawPages: 2, failed: 0, failedError: "", credits: 1.0, firstURL: "https://www.tocaboca.com/docs/en/privacy-policy", firstTitle: "Privacy Policy - Toca Boca"},
}

// TestSearchDecodesRecordedResponses runs the client's own decoding over
// every recorded search, one natural kind per row: policy, terms, reddit,
// forum, news, rating and csm.
func TestSearchDecodesRecordedResponses(t *testing.T) {
	for _, tc := range searchCases {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureClient(t, readFixture(t, tc.file))
			res, cr, err := c.Search(context.Background(), Search{Query: "title privacy policy", MaxResults: 5})
			if err != nil {
				t.Fatal(err)
			}
			if len(res) != tc.results {
				t.Fatalf("decoded %d hits, want %d", len(res), tc.results)
			}
			if cr.Used != 1 || !cr.Reported {
				t.Errorf("credits %+v, want 1 reported", cr)
			}
			raw, published := 0, 0
			for i, r := range res {
				if r.Title == "" || r.URL == "" || r.Content == "" {
					t.Errorf("hit %d is missing title, url or content: %+v", i, r)
				}
				if r.RawContent != "" {
					raw++
				}
				if r.PublishedDate != "" {
					published++
				}
			}
			if raw != tc.rawContent {
				t.Errorf("%d hits carry raw content, want %d", raw, tc.rawContent)
			}
			if published != tc.published {
				t.Errorf("%d hits carry a published date, want %d", published, tc.published)
			}
			first := res[0]
			if first.Title != tc.firstTitle || first.URL != tc.firstURL {
				t.Errorf("first hit %q %q, want %q %q", first.Title, first.URL, tc.firstTitle, tc.firstURL)
			}
			if math.Abs(first.Score-tc.firstScore) > 1e-9 {
				t.Errorf("first hit score %v, want %v", first.Score, tc.firstScore)
			}
		})
	}
}

// TestNewsPublishedDateIsRFC1123 pins the date field's form, because the
// pipeline may parse it rather than only store it.
func TestNewsPublishedDateIsRFC1123(t *testing.T) {
	c := fixtureClient(t, readFixture(t, "roblox/tavily-search-news.json"))
	res, _, err := c.Search(context.Background(), Search{Query: "roblox safety lawsuit", Topic: TopicNews})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 {
		t.Fatal("no hits")
	}
	const want = "Thu, 15 May 2025 00:00:00 GMT"
	if res[0].PublishedDate != want {
		t.Fatalf("published date %q, want %q", res[0].PublishedDate, want)
	}
	if _, err := http.ParseTime(res[0].PublishedDate); err != nil {
		t.Fatalf("published date does not parse: %v", err)
	}
}

// TestExtractDecodesRecordedResponses runs the client's own decoding over
// every recorded extract, including the ones whose every URL failed.
func TestExtractDecodesRecordedResponses(t *testing.T) {
	for _, tc := range extractCases {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureClient(t, readFixture(t, tc.file))
			urls := []string{"https://one.example/a", "https://two.example/b"}
			got, err := c.ExtractWithFailures(context.Background(), urls)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Pages) != tc.pages {
				t.Fatalf("decoded %d pages, want %d", len(got.Pages), tc.pages)
			}
			if len(got.Failed) != tc.failed {
				t.Fatalf("decoded %d failed URLs, want %d", len(got.Failed), tc.failed)
			}
			if tc.failed > 0 && got.Failed[0].Error != tc.failedError {
				t.Errorf("first failure reason %q, want %q", got.Failed[0].Error, tc.failedError)
			}
			if got.Credits.Used != tc.credits || !got.Credits.Reported {
				t.Errorf("credits %+v, want %g reported", got.Credits, tc.credits)
			}
			raw := 0
			for i, p := range got.Pages {
				if p.URL == "" || p.Title == "" || p.RawContent == "" {
					t.Errorf("page %d is missing url, title or raw content: %+v", i, p)
				}
				if p.RawContent != "" {
					raw++
				}
			}
			if raw != tc.rawPages {
				t.Errorf("%d pages carry raw content, want %d", raw, tc.rawPages)
			}
			if tc.pages > 0 {
				if got.Pages[0].URL != tc.firstURL || got.Pages[0].Title != tc.firstTitle {
					t.Errorf("first page %q %q, want %q %q", got.Pages[0].URL, got.Pages[0].Title, tc.firstURL, tc.firstTitle)
				}
			}

			// The simple view keeps the pages and the credits and drops the
			// failures: a URL Tavily could not read is not an error.
			pages, credits, err := c.Extract(context.Background(), urls)
			if err != nil {
				t.Fatalf("Extract raised a failure: %v", err)
			}
			if len(pages) != tc.pages || credits != got.Credits {
				t.Errorf("Extract returned %d pages and %+v, want %d and %+v", len(pages), credits, tc.pages, got.Credits)
			}
		})
	}
}

// TestExtractLeavesFailedURLsOut is the synthetic half of the same rule, with
// one readable and one unreadable URL in the same call.
func TestExtractLeavesFailedURLsOut(t *testing.T) {
	body := []byte(`{"results":[{"url":"https://ok.example/a","title":"ok","raw_content":"text"}],"failed_results":[{"url":"https://bad.example/b","error":"Failed to fetch url"}]}`)
	c := fixtureClient(t, body)
	got, err := c.ExtractWithFailures(context.Background(), []string{"https://ok.example/a", "https://bad.example/b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Pages) != 1 || got.Pages[0].URL != "https://ok.example/a" {
		t.Fatalf("pages %+v, want the readable URL only", got.Pages)
	}
	if len(got.Failed) != 1 || got.Failed[0].URL != "https://bad.example/b" || got.Failed[0].Error != "Failed to fetch url" {
		t.Fatalf("failures %+v, want the unreadable URL with its reason", got.Failed)
	}
	if got.Credits.Used != 1 || got.Credits.Reported {
		t.Fatalf("credits %+v, want the price list's 1 unreported", got.Credits)
	}
}

// TestReportedCreditsBeatThePriceList uses the recorded replies that disagree
// with the price list, which is why Tavily's number wins.
func TestReportedCreditsBeatThePriceList(t *testing.T) {
	// Two pages read, Tavily reported zero. The price list says one.
	c := fixtureClient(t, readFixture(t, "khan-academy-kids/tavily-extract-csm.json"))
	pages, cr, err := c.Extract(context.Background(), []string{"https://www.commonsensemedia.org/app-reviews/khan-academy-kids/user-reviews/adult", "https://www.commonsensemedia.org/app-reviews/khan-academy-kids/user-reviews/child"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 || ExtractCredits(len(pages)) != 1 {
		t.Fatalf("pages %d, fallback %g; want 2 and 1", len(pages), ExtractCredits(len(pages)))
	}
	if cr.Used != 0 || !cr.Reported {
		t.Fatalf("credits %+v, want Tavily's reported 0", cr)
	}

	// A reported number that disagrees the other way wins too.
	c = fixtureClient(t, []byte(`{"results":[],"usage":{"credits":3}}`))
	_, cr, err = c.Search(context.Background(), Search{Query: "q", SearchDepth: DepthAdvanced})
	if err != nil {
		t.Fatal(err)
	}
	if cr.Used != 3 || !cr.Reported {
		t.Fatalf("credits %+v, want Tavily's reported 3 over the price list's 2", cr)
	}
}

// TestSearchCreditsFallBackToThePriceList covers the search half: one credit
// at basic, two at advanced, only when usage is absent.
func TestSearchCreditsFallBackToThePriceList(t *testing.T) {
	for _, tc := range []struct {
		name  string
		depth string
		want  float64
	}{
		{"default depth", "", 1},
		{"basic", DepthBasic, 1},
		{"advanced", DepthAdvanced, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureClient(t, []byte(`{"results":[]}`))
			_, cr, err := c.Search(context.Background(), Search{Query: "q", SearchDepth: tc.depth})
			if err != nil {
				t.Fatal(err)
			}
			if cr.Used != tc.want || cr.Reported {
				t.Fatalf("credits %+v, want %g unreported", cr, tc.want)
			}
		})
	}
}

// TestExtractCreditsFallBackToThePriceList covers the extract half: one credit
// per five successful URLs, rounded up, only when usage is absent.
func TestExtractCreditsFallBackToThePriceList(t *testing.T) {
	for _, tc := range []struct {
		urls int
		want float64
	}{
		{0, 0},
		{1, 1},
		{4, 1},
		{5, 1},
		{6, 2},
		{10, 2},
		{11, 3},
		{20, 4},
	} {
		t.Run(fmt.Sprintf("%d_urls", tc.urls), func(t *testing.T) {
			c := fixtureClient(t, resultsBody(tc.urls))
			got, err := c.ExtractWithFailures(context.Background(), []string{"https://one.example/a", "https://two.example/b"})
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Pages) != tc.urls {
				t.Fatalf("decoded %d pages, want %d", len(got.Pages), tc.urls)
			}
			if got.Credits.Used != tc.want || got.Credits.Reported {
				t.Fatalf("credits %+v, want %g unreported", got.Credits, tc.want)
			}
		})
	}
}

// resultsBody is n successful extract results and no usage member.
func resultsBody(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"results":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"url":"https://example.com/%d","title":"t","raw_content":"text"}`, i)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

// TestPriceList pins the two fallback functions directly.
func TestPriceList(t *testing.T) {
	for _, tc := range []struct {
		depth string
		want  float64
	}{
		{DepthBasic, 1},
		{DepthAdvanced, 2},
		{"", 1},
		{"fast", 1},
	} {
		if got := SearchCredits(tc.depth); got != tc.want {
			t.Errorf("SearchCredits(%q) = %g, want %g", tc.depth, got, tc.want)
		}
	}
	for _, tc := range []struct {
		urls int
		want float64
	}{
		{-1, 0}, {0, 0}, {1, 1}, {5, 1}, {6, 2}, {25, 5}, {26, 6}, {100, 20},
	} {
		if got := ExtractCredits(tc.urls); got != tc.want {
			t.Errorf("ExtractCredits(%d) = %g, want %g", tc.urls, got, tc.want)
		}
	}
}

// TestSearchRequestShape pins what goes on the wire and that zero fields are
// omitted.
func TestSearchRequestShape(t *testing.T) {
	s := newStub(t, http.StatusOK, []byte(`{"results":[]}`))
	c := newTestClient(s.URL)
	_, _, err := c.Search(context.Background(), Search{
		Query:          "roblox privacy policy",
		SearchDepth:    DepthAdvanced,
		Topic:          TopicNews,
		MaxResults:     8,
		IncludeDomains: []string{"roblox.com"},
		ExcludeDomains: []string{"reddit.com"},
		IncludeRaw:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := s.last()
	if req.method != http.MethodPost || req.path != "/search" {
		t.Errorf("request %s %s, want POST /search", req.method, req.path)
	}
	if req.auth != "Bearer test-key" || req.ctype != "application/json" {
		t.Errorf("headers auth %q content-type %q", req.auth, req.ctype)
	}
	for k, want := range map[string]any{
		"query":               "roblox privacy policy",
		"search_depth":        "advanced",
		"topic":               "news",
		"max_results":         float64(8),
		"include_raw_content": true,
		"include_usage":       true,
	} {
		if got := req.body[k]; got != want {
			t.Errorf("body[%q] = %v, want %v", k, got, want)
		}
	}
	if got := fmt.Sprint(req.body["include_domains"]); got != "[roblox.com]" {
		t.Errorf("include_domains %v", got)
	}
	if got := fmt.Sprint(req.body["exclude_domains"]); got != "[reddit.com]" {
		t.Errorf("exclude_domains %v", got)
	}

	// A bare search sends the query and include_usage and nothing else.
	if _, _, err := c.Search(context.Background(), Search{Query: "q"}); err != nil {
		t.Fatal(err)
	}
	bare := s.last().body
	if len(bare) != 2 || bare["query"] != "q" || bare["include_usage"] != true {
		t.Errorf("bare search body %v, want only query and include_usage", bare)
	}
}

// TestExtractRequestShape pins the extract body: replying URLs, basic depth,
// text format and usage.
func TestExtractRequestShape(t *testing.T) {
	s := newStub(t, http.StatusOK, []byte(`{"results":[]}`))
	c := newTestClient(s.URL)
	urls := []string{"https://one.example/a", "https://two.example/b"}
	if _, _, err := c.Extract(context.Background(), urls); err != nil {
		t.Fatal(err)
	}
	req := s.last()
	if req.method != http.MethodPost || req.path != "/extract" {
		t.Errorf("request %s %s, want POST /extract", req.method, req.path)
	}
	if len(req.body) != 4 {
		t.Errorf("extract body %v, want exactly urls, extract_depth, format and include_usage", req.body)
	}
	if got := fmt.Sprint(req.body["urls"]); got != "[https://one.example/a https://two.example/b]" {
		t.Errorf("urls %v", got)
	}
	for k, want := range map[string]any{
		"extract_depth": DepthBasic,
		"format":        FormatText,
		"include_usage": true,
	} {
		if got := req.body[k]; got != want {
			t.Errorf("body[%q] = %v, want %v", k, got, want)
		}
	}
}

// TestKeyIsReadOnEveryAttempt pins rotation: both calls, and every retry
// inside a call, read the key again through the injected func.
func TestKeyIsReadOnEveryAttempt(t *testing.T) {
	var requests, keys atomic.Int32
	var mu sync.Mutex
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = append(auth, r.Header.Get("Authorization"))
		mu.Unlock()
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, func() (string, error) {
		return fmt.Sprintf("key-%d", keys.Add(1)), nil
	})
	stopWaits(c)
	if _, _, err := c.Search(context.Background(), Search{Query: "q"}); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("%d requests, want 2", got)
	}
	if got := keys.Load(); got != 2 {
		t.Fatalf("the key was read %d times, want once per attempt", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(auth) != "[Bearer key-1 Bearer key-2]" {
		t.Fatalf("authorization headers %v, want the rotated key on the retry", auth)
	}
}

// TestKeyFailuresAreNotRetried: a missing key is a configuration fault, not a
// condition a wait clears, and no request goes out.
func TestKeyFailuresAreNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, func() (string, error) { return "", errors.New("no TAVILY_KEY") })
	stopWaits(c)
	_, _, err := c.Search(context.Background(), Search{Query: "q"})
	if err == nil || !strings.Contains(err.Error(), "no TAVILY_KEY") {
		t.Fatalf("err %v, want the key func's error", err)
	}
	if Retryable(err) {
		t.Errorf("Retryable(key failure) is true")
	}
	if calls.Load() != 0 {
		t.Errorf("%d requests were sent without a key", calls.Load())
	}

	c = New(srv.URL, func() (string, error) { return "   ", nil })
	stopWaits(c)
	if _, _, err := c.Search(context.Background(), Search{Query: "q"}); err == nil {
		t.Fatal("an empty key was accepted")
	}
	if calls.Load() != 0 {
		t.Errorf("%d requests were sent with an empty key", calls.Load())
	}
}

// TestRetryBudgetsDifferBetweenRateLimitsAndHostFaults: a rate limit is a
// free refusal that draws the larger budget, while every 5xx retry may be
// another billed call and so draws the smaller one.
func TestRetryBudgetsDifferBetweenRateLimitsAndHostFaults(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   int
	}{
		{"429 draws the rate-limit budget", http.StatusTooManyRequests, 4},
		{"500 draws the smaller fault budget", http.StatusInternalServerError, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			c := newTestClient(srv.URL)
			_, _, err := c.Search(context.Background(), Search{Query: "q"})
			var te *Error
			if !errors.As(err, &te) || te.Status != tc.status {
				t.Fatalf("err %v, want a %d", err, tc.status)
			}
			if got := int(calls.Load()); got != tc.want {
				t.Fatalf("%d attempts, want %d", got, tc.want)
			}
		})
	}
}

// TestRetriesRateLimitThenSucceeds is the Done-when's first retry pin.
func TestRetriesRateLimitThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"results":[{"url":"https://example.com","title":"t","content":"c","score":1}]}`))
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	res, _, err := c.Search(context.Background(), Search{Query: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || len(res) != 1 {
		t.Fatalf("%d attempts and %d hits, want 2 and 1", calls.Load(), len(res))
	}
}

// TestDoesNotRetryBadRequest is the Done-when's second retry pin.
func TestDoesNotRetryBadRequest(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"detail":{"error":"Invalid request"}}`))
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	_, _, err := c.Search(context.Background(), Search{Query: ""})
	var te *Error
	if !errors.As(err, &te) || te.Status != http.StatusBadRequest {
		t.Fatalf("err %v, want a 400", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("%d attempts, want 1", calls.Load())
	}
	if Retryable(err) {
		t.Errorf("Retryable(400) is true")
	}
	if !strings.Contains(err.Error(), "Invalid request") {
		t.Errorf("error %q does not carry the body", err)
	}
}

// TestRetryAfterSetsTheWait: the provider's hint replaces the backoff.
func TestRetryAfterSetsTheWait(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	c.Retry.Max = 10 * time.Second
	var mu sync.Mutex
	var waits []time.Duration
	c.Retry.Sleep = func(_ context.Context, d time.Duration) error {
		mu.Lock()
		waits = append(waits, d)
		mu.Unlock()
		return nil
	}
	if _, _, err := c.Search(context.Background(), Search{Query: "q"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(waits) != 1 {
		t.Fatalf("%d waits, want 1", len(waits))
	}
	// The hint plus a spread of up to a second.
	if waits[0] < 2*time.Second || waits[0] > 3*time.Second {
		t.Fatalf("waited %v, want the 2s hint and its spread", waits[0])
	}
}

// TestCancelledContextStopsRetrying: a dead context ends the loop instead of
// spending the attempts.
func TestCancelledContextStopsRetrying(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		cancel()
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	_, _, err := c.Search(ctx, Search{Query: "q"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v, want a cancellation", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("%d attempts, want 1", calls.Load())
	}
}

// TestUnreadableOKIsNotRetried: a 200 may have been billed, so a body that
// will not decode is permanent.
func TestUnreadableOKIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte("<html>not json</html>"))
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	if _, _, err := c.Search(context.Background(), Search{Query: "q"}); err == nil {
		t.Fatal("a body that will not decode was accepted")
	}
	if calls.Load() != 1 {
		t.Fatalf("%d attempts, want 1", calls.Load())
	}
}

// TestExtractWithNoURLsMakesNoCall: nothing to read is no call at all.
func TestExtractWithNoURLsMakesNoCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("extract with no URLs called the API")
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	pages, cr, err := c.Extract(context.Background(), nil)
	if err != nil || pages != nil || cr != (Credits{}) {
		t.Fatalf("Extract(nil) = %v, %+v, %v; want nothing and no error", pages, cr, err)
	}
}

func TestNewFillsDefaults(t *testing.T) {
	c := New("https://api.tavily.com/", func() (string, error) { return "k", nil })
	if c.BaseURL != DefaultBaseURL {
		t.Errorf("base url %q, want %q", c.BaseURL, DefaultBaseURL)
	}
	if c.Retry.PerMinute != 90 || c.Retry.PerMinute >= FreePerMinute {
		t.Errorf("per-minute %d, want 90 under the free cap of %d", c.Retry.PerMinute, FreePerMinute)
	}
	if c.Retry.Name != "tavily" {
		t.Errorf("pacer name %q, want tavily", c.Retry.Name)
	}
	if c.Retry.Attempts != 4 || c.Retry.TransientAttempts != 2 {
		t.Errorf("attempt budgets %d and %d, want 4 and 2", c.Retry.Attempts, c.Retry.TransientAttempts)
	}
	if got := New("", func() (string, error) { return "k", nil }).BaseURL; got != DefaultBaseURL {
		t.Errorf("empty base url %q, want %q", got, DefaultBaseURL)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter(""); got != 0 {
		t.Errorf("empty header %v, want no hint", got)
	}
	if got := parseRetryAfter("3"); got != 3*time.Second {
		t.Errorf("seconds header %v, want 3s", got)
	}
	if got := parseRetryAfter("nonsense"); got != 0 {
		t.Errorf("garbage header %v, want no hint", got)
	}
	if got := parseRetryAfter("-1"); got != 0 {
		t.Errorf("negative header %v, want no hint", got)
	}
	future := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(future); got <= 0 || got > 31*time.Second {
		t.Errorf("date header %v, want a hint near 30s", got)
	}
	past := time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(past); got != 0 {
		t.Errorf("past date header %v, want no hint", got)
	}
}
