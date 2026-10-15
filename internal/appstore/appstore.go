// Package appstore reads Apple's public iTunes search API and customer-review
// feed. Both are keyless JSON endpoints Apple publishes for this purpose. The
// review feed serves up to ten pages of fifty recent reviews per storefront
// country.
package appstore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the iTunes host.
	DefaultBaseURL = "https://itunes.apple.com"
	// MaxPages is the deepest page the review feed serves.
	MaxPages = 10
	// PageSize is the number of reviews a full feed page holds, so the review
	// numbered n was read from page (n-1)/PageSize+1.
	PageSize = 50
)

// Client reads the search API and the review feed for one storefront country.
type Client struct {
	BaseURL string
	Country string
	http    *http.Client
}

// New returns a client for one storefront country, such as "us". An empty
// baseURL takes the default host and an empty country takes "us". The trailing
// slash on baseURL is dropped.
func New(baseURL, country string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if country == "" {
		country = "us"
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Country: country,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// App is one hit from the search API.
type App struct {
	ID          int64  `json:"trackId"`
	Name        string `json:"trackName"`
	Seller      string `json:"sellerName"`
	SellerURL   string `json:"sellerUrl"`
	StoreURL    string `json:"trackViewUrl"`
	AgeRating   string `json:"trackContentRating"`
	Description string `json:"description"`
	Genre       string `json:"primaryGenreName"`
}

// Find searches the storefront for title and returns the hits, best first.
// limit caps the number of hits.
func (c *Client) Find(ctx context.Context, title string, limit int) ([]App, error) {
	q := url.Values{"term": {title}, "entity": {"software"}, "country": {c.Country}, "limit": {strconv.Itoa(limit)}}
	var out struct {
		Results []App `json:"results"`
	}
	if err := c.get(ctx, c.BaseURL+"/search?"+q.Encode(), &out); err != nil {
		return nil, fmt.Errorf("appstore: search %q: %w", title, err)
	}
	return out.Results, nil
}

// Review is one customer review.
type Review struct {
	// Index is the review's position in the read feed, starting at 1, so a
	// card can say "review 312". It counts reviews, not feed entries.
	Index int `json:"index"`
	// ID is Apple's review id.
	ID      string `json:"id"`
	Rating  int    `json:"rating"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Author  string `json:"author"`
	Version string `json:"version"`
	// Updated is when the review was posted or last edited. It is the zero
	// time when the feed omits or mangles the date.
	Updated time.Time `json:"updated"`
}

// Text is the review's title and body, as the reader sees it, with the
// surrounding whitespace Apple pads the feed members with trimmed off.
func (r Review) Text() string {
	return strings.TrimSpace(r.Title) + "\n" + strings.TrimSpace(r.Body)
}

// FeedURL is the public page of the review feed, used as the finding's
// source URL.
func (c *Client) FeedURL(appID int64, page int) string {
	return fmt.Sprintf("%s/%s/rss/customerreviews/page=%d/id=%d/sortby=mostrecent/json", c.BaseURL, c.Country, page, appID)
}

// Reviews reads up to pages pages of the most recent reviews, indexed from 1
// across the pages. Reviews with no text are skipped, so the index counts
// reviews a card could quote.
//
// A page with no entries ends the read. A later page that fails for any
// reason also ends the read, and the reviews read so far are returned: the
// feed's deeper pages are flaky and a partial feed is still evidence. A first
// page that fails is an error, because it means no reviews at all.
func (c *Client) Reviews(ctx context.Context, appID int64, pages int) ([]Review, error) {
	if pages <= 0 || pages > MaxPages {
		pages = MaxPages
	}
	var all []Review
	for p := 1; p <= pages; p++ {
		if err := ctx.Err(); err != nil {
			return all, err
		}
		entries, err := c.page(ctx, appID, p)
		if err != nil {
			if p == 1 || ctx.Err() != nil {
				return all, fmt.Errorf("appstore: reviews page %d: %w", p, err)
			}
			return all, nil
		}
		if len(entries) == 0 {
			return all, nil
		}
		for _, e := range entries {
			if e.Content.Label == "" {
				continue // the feed can lead with the app itself
			}
			rating, _ := strconv.Atoi(strings.TrimSpace(e.Rating.Label))
			all = append(all, Review{
				Index:   len(all) + 1,
				ID:      e.ID.Label,
				Rating:  rating,
				Title:   e.Title.Label,
				Body:    e.Content.Label,
				Author:  e.Author.Name.Label,
				Version: e.Version.Label,
				Updated: parseTime(e.Updated.Label),
			})
		}
	}
	return all, nil
}

// page reads one feed page.
func (c *Client) page(ctx context.Context, appID int64, page int) ([]entry, error) {
	var feed struct {
		Feed struct {
			Entry json.RawMessage `json:"entry"`
		} `json:"feed"`
	}
	if err := c.get(ctx, c.FeedURL(appID, page), &feed); err != nil {
		return nil, err
	}
	return decodeEntries(feed.Feed.Entry)
}

// label is the feed's leaf member, with the text in "label".
type label struct {
	Label string `json:"label"`
}

// entry is one member of the feed's entry member.
type entry struct {
	ID      label `json:"id"`
	Title   label `json:"title"`
	Content label `json:"content"`
	Rating  label `json:"im:rating"`
	Version label `json:"im:version"`
	Updated label `json:"updated"`
	Author  struct {
		Name label `json:"name"`
	} `json:"author"`
}

// decodeEntries accepts the feed's entry member as an array or, when the page
// holds a single review, as a bare object. An absent or null entry means the
// page is empty.
func decodeEntries(raw json.RawMessage) ([]entry, error) {
	s := strings.TrimSpace(string(raw))
	switch {
	case s == "" || s == "null":
		return nil, nil
	case strings.HasPrefix(s, "["):
		var es []entry
		if err := json.Unmarshal(raw, &es); err != nil {
			return nil, err
		}
		return es, nil
	default:
		var e entry
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		return []entry{e}, nil
	}
}

// parseTime reads the feed's RFC3339 date, or returns the zero time when the
// field is absent or malformed.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (c *Client) get(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, u)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
