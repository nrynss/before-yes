// Package tavily is a small client for Tavily's search and extract
// endpoints.
//
// Both methods ask for include_usage, so a reply usually says what the call
// cost. When it does not, the price list is the fallback: SearchCredits and
// ExtractCredits. Tavily's reported number is authoritative when present,
// because the recorded captures show it does not follow the price list
// (feedback.md, "Extract credits do not follow the price list").
//
// A URL Tavily could not read is a fact about the source, not a failure of
// the call: the call succeeds and the URL is left out of the pages.
// ExtractWithFailures names every such URL and Tavily's reason, so a caller
// can say which source is missing and why.
//
// The key is read through the injected func on every attempt, so a rotated
// key takes effect without a restart. Call starts are paced under the free
// plan's per-minute cap with Keel's throttle, which also retries a 429 or a
// 5xx and honours Retry-After.
package tavily

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nrynss/keel/throttle"
)

// DefaultBaseURL is Tavily's API.
const DefaultBaseURL = "https://api.tavily.com"

// Depths for Search.SearchDepth. The price list charges one credit for a
// basic search and two for an advanced one, when Tavily reports nothing.
const (
	DepthBasic    = "basic"
	DepthAdvanced = "advanced"
)

// Topics for Search.Topic. News hits carry a published date.
const (
	TopicGeneral = "general"
	TopicNews    = "news"
)

// FormatText is the extract format: plain text, with the page's markup
// stripped.
const FormatText = "text"

// FreePerMinute is the free plan's requests-per-minute cap. The client paces
// a tenth under it.
const FreePerMinute = 100

// maxBody caps one reply. An extract of raw page text runs to megabytes.
const maxBody = 32 << 20

// Client calls Tavily. Build one with New; the zero value has no base URL.
type Client struct {
	// BaseURL is the API root, without a trailing slash.
	BaseURL string
	// Retry paces and retries calls: the per-minute cap, the attempt
	// budgets, the backoff and Retry-After. New fills it; tests shorten it.
	Retry throttle.Config

	key  func() (string, error)
	http *http.Client
}

// New returns a client that reads its key through key on every attempt, so a
// rotated key takes effect on the next call. An empty baseURL means
// DefaultBaseURL.
func New(baseURL string, key func() (string, error)) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		key:     key,
		http:    &http.Client{Timeout: 90 * time.Second},
		Retry: throttle.Config{
			Attempts: 4,
			// Every 5xx retry may be another billed call, so it draws on
			// the smaller budget. A rate limit is a free refusal a wait
			// clears, so it draws on the larger one.
			TransientAttempts: 2,
			Backoff:           5 * time.Second,
			Max:               60 * time.Second,
			WaitBudget:        3 * time.Minute,
			// A tenth under the free plan's cap.
			PerMinute: FreePerMinute - FreePerMinute/10,
			Name:      "tavily",
		},
	}
}

// Search is one search request. Zero fields are left out of the body, so
// Tavily's defaults apply.
type Search struct {
	Query string `json:"query"`
	// SearchDepth is DepthBasic or DepthAdvanced. Empty means Tavily's
	// default, basic.
	SearchDepth string `json:"search_depth,omitempty"`
	// Topic is TopicGeneral or TopicNews.
	Topic string `json:"topic,omitempty"`
	// MaxResults caps the hits. Tavily's own cap is 20.
	MaxResults int `json:"max_results,omitempty"`
	// IncludeDomains and ExcludeDomains narrow the search to or away from
	// whole domains, without a scheme or path.
	IncludeDomains []string `json:"include_domains,omitempty"`
	ExcludeDomains []string `json:"exclude_domains,omitempty"`
	// IncludeRaw asks for each hit's whole page text in Result.RawContent.
	IncludeRaw bool `json:"include_raw_content,omitempty"`
}

// Result is one search hit.
type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
	// RawContent is the whole page text, present only when the search asked
	// for it and Tavily could read the page. Reddit hits never carry it.
	RawContent string  `json:"raw_content"`
	Score      float64 `json:"score"`
	// PublishedDate is set on news hits, in RFC 1123 form, such as
	// "Thu, 15 May 2025 00:00:00 GMT".
	PublishedDate string `json:"published_date"`
}

// Page is one page Tavily read.
type Page struct {
	URL        string `json:"url"`
	Title      string `json:"title"`
	RawContent string `json:"raw_content"`
}

// Failed is a URL Tavily could not read, with its reason.
type Failed struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

// Extracted is one extract call: the pages read, the URLs that failed with
// their reasons, and the credits the call cost.
type Extracted struct {
	Pages   []Page
	Failed  []Failed
	Credits Credits
}

// Credits is what one call cost in Tavily credits. Used is Tavily's reported
// number when Reported is true, and the price list's fallback when it is not.
// A reported zero is a real measurement: Tavily's extract charges nothing for
// some calls it answers.
type Credits struct {
	Used     float64
	Reported bool
}

// SearchCredits is the price list's charge for one search: one credit at
// basic depth, two at advanced. It is the fallback for a reply that carries
// no usage.
func SearchCredits(depth string) float64 {
	if depth == DepthAdvanced {
		return 2
	}
	return 1
}

// ExtractCredits is the price list's charge for one extract that read n URLs:
// one credit per five, rounded up. It is the fallback for a reply that
// carries no usage, and it counts the URLs that succeeded, because a failed
// URL was not read.
func ExtractCredits(n int) float64 {
	if n <= 0 {
		return 0
	}
	return float64((n + 4) / 5)
}

// usage is Tavily's usage member, present when the call asked for it and the
// plan reports it.
type usage struct {
	Credits float64 `json:"credits"`
}

// usageCredits is what a call cost: Tavily's number when it sent one, the
// price list's figure otherwise. Reported is true whenever the usage member
// came back, including a reported zero.
func usageCredits(u *usage, fallback float64) Credits {
	if u == nil {
		return Credits{Used: fallback}
	}
	return Credits{Used: u.Credits, Reported: true}
}

// Search runs one search and returns its hits in Tavily's relevance order.
func (c *Client) Search(ctx context.Context, s Search) ([]Result, Credits, error) {
	var out struct {
		Results []Result `json:"results"`
		Usage   *usage   `json:"usage"`
	}
	body := struct {
		Search
		IncludeUsage bool `json:"include_usage"`
	}{Search: s, IncludeUsage: true}
	if err := c.post(ctx, "/search", body, &out); err != nil {
		return nil, Credits{}, err
	}
	return out.Results, usageCredits(out.Usage, SearchCredits(s.SearchDepth)), nil
}

// Extract fetches the readable text of urls at basic depth, as text. A URL
// Tavily could not read is left out of the pages and is not an error.
func (c *Client) Extract(ctx context.Context, urls []string) ([]Page, Credits, error) {
	ex, err := c.ExtractWithFailures(ctx, urls)
	if err != nil {
		return nil, Credits{}, err
	}
	return ex.Pages, ex.Credits, nil
}

// ExtractWithFailures is Extract with the URLs Tavily could not read kept,
// each with its reason, so a caller can report which sources are missing. No
// URLs means no call at all.
func (c *Client) ExtractWithFailures(ctx context.Context, urls []string) (Extracted, error) {
	if len(urls) == 0 {
		return Extracted{}, nil
	}
	var out struct {
		Results []Page   `json:"results"`
		Failed  []Failed `json:"failed_results"`
		Usage   *usage   `json:"usage"`
	}
	body := struct {
		URLs         []string `json:"urls"`
		ExtractDepth string   `json:"extract_depth"`
		Format       string   `json:"format"`
		IncludeUsage bool     `json:"include_usage"`
	}{URLs: urls, ExtractDepth: DepthBasic, Format: FormatText, IncludeUsage: true}
	if err := c.post(ctx, "/extract", body, &out); err != nil {
		return Extracted{}, err
	}
	return Extracted{
		Pages:   out.Results,
		Failed:  out.Failed,
		Credits: usageCredits(out.Usage, ExtractCredits(len(out.Results))),
	}, nil
}

// Error is a non-2xx answer.
type Error struct {
	Status     int
	RetryAfter time.Duration
	Body       string
}

func (e *Error) Error() string {
	return fmt.Sprintf("tavily: HTTP %d: %s", e.Status, e.Body)
}

// Retryable reports whether waiting can clear err: a rate limit, a failing
// host, or a dropped connection. Any other 4xx cannot.
func Retryable(err error) bool {
	var te *Error
	if errors.As(err, &te) {
		return te.Status == http.StatusTooManyRequests || te.Status >= 500
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, errPermanent)
}

// transient claims host faults, which draw on the smaller attempt budget
// because each retry may be another billed call.
func transient(err error) bool {
	var te *Error
	if errors.As(err, &te) {
		return te.Status >= 500
	}
	return true
}

func retryAfter(err error) (time.Duration, bool) {
	var te *Error
	if errors.As(err, &te) && te.RetryAfter > 0 {
		return te.RetryAfter, true
	}
	return 0, false
}

var errPermanent = errors.New("tavily: permanent")

// post sends one JSON body and decodes the 2xx answer into out, retrying per
// the client's Retry config.
func (c *Client) post(ctx context.Context, path string, in, out any) error {
	payload, err := json.Marshal(in)
	if err != nil {
		return err
	}
	cfg := c.Retry
	cfg.Transient = transient
	cfg.RetryAfter = retryAfter
	err = throttle.Retry(ctx, cfg, Retryable, func() error {
		return c.attempt(ctx, path, payload, out)
	})
	if err != nil {
		return throttle.Note(err, cfg, Retryable)
	}
	return nil
}

// attempt is one try: read the key, send the body, and either decode the
// answer or classify the failure.
func (c *Client) attempt(ctx context.Context, path string, payload []byte, out any) error {
	key, err := c.key()
	if err != nil {
		return fmt.Errorf("%w: key: %v", errPermanent, err)
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("%w: empty API key", errPermanent)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("%w: %v", errPermanent, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return &Error{
			Status:     resp.StatusCode,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
			Body:       snippet(raw),
		}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		// A 200 may have been billed; asking again would pay twice for the
		// same fault.
		return fmt.Errorf("%w: decode: %v", errPermanent, err)
	}
	return nil
}

// parseRetryAfter reads the Retry-After header, in seconds or as an HTTP
// date. A value in the past, or one that does not parse, is no hint.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if s, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// snippet is a short body for an error message.
func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
