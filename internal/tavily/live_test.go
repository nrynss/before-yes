//go:build live

// This file is the T2.2b live probe. It calls the real Tavily API, spends
// real credits and never runs in the gate or in CI. Export TAVILY_KEY from
// the main checkout's .env and run `go test -tags live -run TestLive
// ./internal/tavily` (see "Secrets and spend" in dev-diary/ORCHESTRATOR.md for
// the wrapper); the key is read from the environment and never printed.
//
// The probe makes one basic search without raw content, the same search with
// it, and one basic extract of three public URLs. It records whether
// usage.credits comes back and what it says against the price list (basic
// search 1, advanced 2, extract 1 per five successful URLs), whether
// include_raw_content costs extra, latency per call, and what the replies say
// about rate limits. It checks the client's live promise that a URL Tavily
// could not read is left out of the pages without an error, and it refuses to
// start a call whose price-list estimate would take the run past
// liveCreditCap.

package tavily

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// liveCreditCap is the probe's hard ceiling, in Tavily credits. The expected
// spend is three: two basic searches and one extract of three URLs.
const liveCreditCap float64 = 5

// liveQuery is the policy query recorded for Minecraft in
// testdata/titles/minecraft, so the live hits are comparable with the capture.
const liveQuery = "Minecraft privacy policy"

// The extract's URLs are all public material. The first is the recorded
// query's top hit; the second is a Reddit thread Tavily could not read in
// T0.4a and T0.4b; the third is a reserved-TLD host that cannot resolve.
const (
	livePageURL   = "https://www.minecraft.net/en-us/usage-guidelines"
	liveRedditURL = "https://www.reddit.com/r/Minecraft/comments/1k7lbl8/help_a_tech_challenged_parent"
	liveDeadURL   = "https://before-yes-probe.invalid/"
)

// TestLiveTavilySearchAndExtract is the whole probe: two searches and one
// extract, run in order, with a running credit total.
func TestLiveTavilySearchAndExtract(t *testing.T) {
	if strings.TrimSpace(os.Getenv("TAVILY_KEY")) == "" {
		t.Skip("TAVILY_KEY is not set; export it from .env and run go test -tags live -run TestLive ./internal/tavily")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	rec := &recorder{base: http.DefaultTransport}
	client := New("", func() (string, error) {
		key := strings.TrimSpace(os.Getenv("TAVILY_KEY"))
		if key == "" {
			return "", fmt.Errorf("TAVILY_KEY is empty")
		}
		return key, nil
	})
	client.http.Transport = rec

	b := &budget{cap: liveCreditCap}
	began := time.Now()
	defer func() {
		statuses := make([]string, 0, len(rec.exchanges()))
		for _, ex := range rec.exchanges() {
			statuses = append(statuses, fmt.Sprintf("%d", ex.Status))
		}
		t.Logf("summary: %d HTTP exchange(s) [%s] for 3 calls, %s wall time, %g of %g credits spent",
			len(statuses), strings.Join(statuses, ", "), time.Since(began).Round(time.Millisecond), b.spent, b.cap)
		logRateLimits(t, rec)
		if b.spent > b.cap {
			t.Errorf("budget exceeded: spent %g credits against a %g cap", b.spent, b.cap)
		}
	}()

	t.Logf("live Tavily probe (T2.2b) against %s; cap %g credits; pace %d calls/minute", client.BaseURL, liveCreditCap, client.Retry.PerMinute)
	t.Logf("price list for comparison: basic search 1, advanced 2, extract 1 per 5 successful URLs")

	// Call 1: the gather plan's basic search shape, without raw content.
	mark := len(rec.exchanges())
	first := Search{Query: liveQuery, MaxResults: 5}
	b.allow(t, "search 1 (no raw content)", SearchCredits(first.SearchDepth))
	start := time.Now()
	hits, credits, err := client.Search(ctx, first)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("search 1 (no raw content): %v", err)
	}
	if len(hits) == 0 {
		t.Errorf("search 1 returned no hits")
	}
	logSearch(t, 1, first, hits, credits, elapsed)
	logExchanges(t, rec, mark)
	b.charge(t, credits)

	// Call 2: the same search with each hit's whole page text, so the two
	// reported numbers answer whether include_raw_content costs extra.
	mark = len(rec.exchanges())
	second := Search{Query: liveQuery, MaxResults: 5, IncludeRaw: true}
	b.allow(t, "search 2 (raw content)", SearchCredits(second.SearchDepth))
	start = time.Now()
	rawHits, rawCredits, err := client.Search(ctx, second)
	elapsed = time.Since(start)
	if err != nil {
		t.Fatalf("search 2 (raw content): %v", err)
	}
	logSearch(t, 2, second, rawHits, rawCredits, elapsed)
	logExchanges(t, rec, mark)
	b.charge(t, rawCredits)
	t.Logf("include_raw_content: off reported %s, on reported %s; difference %g",
		creditsNumber(credits), creditsNumber(rawCredits), rawCredits.Used-credits.Used)

	// Call 3: one basic extract of three public URLs.
	mark = len(rec.exchanges())
	urls := []string{livePageURL, liveRedditURL, liveDeadURL}
	b.allow(t, "extract", ExtractCredits(len(urls)))
	start = time.Now()
	ex, err := client.ExtractWithFailures(ctx, urls)
	elapsed = time.Since(start)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	t.Logf("call 3: extract depth=%s format=%s urls=%d", DepthBasic, FormatText, len(urls))
	for _, u := range urls {
		t.Logf("  asked for: %s", u)
	}
	t.Logf("  latency: %s", elapsed.Round(time.Millisecond))
	t.Logf("  credits: %s (price list for %d successful URL(s): %g)",
		creditsNumber(ex.Credits), len(ex.Pages), ExtractCredits(len(ex.Pages)))
	for _, p := range ex.Pages {
		t.Logf("  page: %s (%q), %d bytes of text", p.URL, p.Title, len(p.RawContent))
	}
	for _, f := range ex.Failed {
		t.Logf("  failed: %s: %s", f.URL, f.Error)
	}
	logExchanges(t, rec, mark)
	b.charge(t, ex.Credits)

	// The client's live promise: a URL Tavily could not read is left out of
	// the pages, and the call is not an error.
	failed := map[string]bool{}
	for _, f := range ex.Failed {
		failed[f.URL] = true
	}
	for _, p := range ex.Pages {
		if failed[p.URL] {
			t.Errorf("promise broken: %s came back as both a page and a failure", p.URL)
		}
	}
	if len(ex.Failed) == 0 {
		t.Errorf("no URL failed, so the failed-URL promise was not exercised; %s and %s were expected to fail", liveDeadURL, liveRedditURL)
	} else {
		t.Logf("promise held: %d failed URL(s) left out of %d page(s), and the call returned no error", len(ex.Failed), len(ex.Pages))
	}
}

// budget tracks the probe's running credit total against its cap.
type budget struct {
	cap   float64
	spent float64
}

// allow refuses to start a call whose price-list estimate would take the run
// past the cap. It runs before every call, so the cap holds even if an earlier
// call reported more than its estimate.
func (b *budget) allow(t *testing.T, what string, estimate float64) {
	t.Helper()
	t.Logf("before %s: spent %g, price-list estimate %g, cap %g", what, b.spent, estimate, b.cap)
	if b.spent+estimate > b.cap {
		t.Fatalf("budget guard: refusing %s: %g spent + %g estimated exceeds the %g-credit cap", what, b.spent, estimate, b.cap)
	}
}

// charge adds one call's measured credits to the running total.
func (b *budget) charge(t *testing.T, c Credits) {
	t.Helper()
	b.spent += c.Used
	t.Logf("  running total: %g of %g credits", b.spent, b.cap)
}

// creditsNumber describes one call's credits: Tavily's number when it sent
// one, the price list's fallback when it did not.
func creditsNumber(c Credits) string {
	if c.Reported {
		return fmt.Sprintf("%g (usage.credits reported by Tavily)", c.Used)
	}
	return fmt.Sprintf("%g (usage.credits absent; price-list fallback)", c.Used)
}

// logSearch prints one search's outcome.
func logSearch(t *testing.T, n int, s Search, hits []Result, c Credits, elapsed time.Duration) {
	t.Helper()
	depth := s.SearchDepth
	if depth == "" {
		depth = DepthBasic + " (Tavily's default)"
	}
	t.Logf("call %d: search query=%q depth=%s max_results=%d include_raw_content=%t", n, s.Query, depth, s.MaxResults, s.IncludeRaw)
	t.Logf("  latency: %s", elapsed.Round(time.Millisecond))
	t.Logf("  credits: %s (price list for this search: %g)", creditsNumber(c), SearchCredits(s.SearchDepth))
	raw, rawBytes := 0, 0
	for _, h := range hits {
		if strings.TrimSpace(h.RawContent) != "" {
			raw++
			rawBytes += len(h.RawContent)
		}
	}
	t.Logf("  hits: %d; raw_content non-empty on %d (%d bytes)", len(hits), raw, rawBytes)
	if len(hits) > 0 {
		t.Logf("  top hit: %s (%q)", hits[0].URL, hits[0].Title)
	}
}

// exchange is one HTTP request and its reply's status and headers.
type exchange struct {
	Method  string
	URL     string
	Status  int
	Headers http.Header
	Start   time.Time
	Elapsed time.Duration
}

// recorder is an http.RoundTripper that keeps every exchange, so the probe can
// report status codes and rate-limit headers from the calls it already makes.
type recorder struct {
	base http.RoundTripper
	mu   sync.Mutex
	all  []exchange
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := r.base.RoundTrip(req)
	ex := exchange{Method: req.Method, URL: req.URL.String(), Start: start, Elapsed: time.Since(start), Status: -1}
	if resp != nil {
		ex.Status = resp.StatusCode
		ex.Headers = resp.Header.Clone()
	}
	r.mu.Lock()
	r.all = append(r.all, ex)
	r.mu.Unlock()
	return resp, err
}

// exchanges returns a copy of every exchange so far.
func (r *recorder) exchanges() []exchange {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]exchange(nil), r.all...)
}

// logExchanges prints the wire exchanges added since mark: status, time and
// every reply header.
func logExchanges(t *testing.T, rec *recorder, mark int) {
	t.Helper()
	for i, ex := range rec.exchanges()[mark:] {
		t.Logf("  wire %d: %s %s -> %d in %s", i+1, ex.Method, ex.URL, ex.Status, ex.Elapsed.Round(time.Millisecond))
		if ex.Headers == nil {
			t.Logf("  wire %d: no reply headers", i+1)
			continue
		}
		names := make([]string, 0, len(ex.Headers))
		for name := range ex.Headers {
			if strings.EqualFold(name, "Set-Cookie") {
				continue
			}
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			t.Logf("  wire %d header %s: %s", i+1, name, strings.Join(ex.Headers.Values(name), " | "))
		}
	}
}

// logRateLimits reports what the replies said about rate limits: the client
// paces at 90 calls a minute under Tavily's documented 100, and any header the
// API sends about the window or the remaining allowance is printed here.
func logRateLimits(t *testing.T, rec *recorder) {
	t.Helper()
	found := false
	for i, ex := range rec.exchanges() {
		for name, values := range ex.Headers {
			if !rateLimitName(name) {
				continue
			}
			found = true
			t.Logf("rate limit: exchange %d: %s: %s", i+1, name, strings.Join(values, " | "))
		}
	}
	if !found {
		t.Logf("rate limits: no rate-limit headers in any reply, so the API reported no window or remaining allowance")
	}
}

// rateLimitName reports whether a header name could carry rate-limit
// information.
func rateLimitName(name string) bool {
	n := strings.ToLower(name)
	for _, part := range []string{"rate", "limit", "retry", "quota", "credit"} {
		if strings.Contains(n, part) {
			return true
		}
	}
	return false
}
