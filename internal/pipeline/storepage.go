package pipeline

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/nrynss/before-yes/internal/appstore"
	"github.com/nrynss/before-yes/internal/finding"
)

// Review sources, for the card's small print.
const (
	ReviewsFromFeed = "feed"
	ReviewsFromPage = "page"
)

// PageReview is one review read off an App Store web page. The page carries
// only a title and a body: no rating, date or reviewer name survives the
// extract, so none is kept and none is ever inferred.
type PageReview struct {
	// Position is the review's place on the page, from 1, counted after
	// duplicates are dropped.
	Position int
	Title    string
	Body     string
}

var (
	devResponse  = regexp.MustCompile(`^Developer Response\b`)
	reviewsStart = "Ratings & Reviews"
	reviewsEnd   = regexp.MustCompile(`^What[’']s New$`)
	editorsPick  = regexp.MustCompile(`^Editors[’']? Choice$`)
)

// ParseStorePage reads the reviews off an extracted App Store page. The
// T3.8a captures fix the shape:
//
//   - reviews sit between the "Ratings & Reviews" heading and "What's New";
//   - a review is a title line and a body line, with nothing else marking it;
//   - each block appears twice in a row;
//   - a developer response appears first as a bare line with no label, and
//     only in the repeat after a "Developer Response <date>" line;
//   - "Editors' Choice" and the line after it are an editorial blurb.
//
// Responses and blurbs are dropped, the labelled response bodies are used
// to drop their unlabelled first copies, and blocks are deduplicated by
// content. A page with no such section, or none left after the drops, has
// no reviews.
func ParseStorePage(text string) []PageReview {
	lines := strings.Split(text, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == reviewsStart {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil
	}
	var section []string
	for _, l := range lines[start:] {
		l = strings.TrimSpace(l)
		if reviewsEnd.MatchString(l) {
			break
		}
		if l != "" {
			section = append(section, l)
		}
	}

	// Pass 1: the labelled responses name the bodies to drop, and the
	// editorial blurbs are dropped with their text.
	responses := map[string]bool{}
	var kept []string
	for i := 0; i < len(section); i++ {
		l := section[i]
		if devResponse.MatchString(l) || editorsPick.MatchString(l) {
			if devResponse.MatchString(l) && i+1 < len(section) {
				responses[section[i+1]] = true
			}
			i++ // the label's own text
			continue
		}
		kept = append(kept, l)
	}
	// Pass 2: the unlabelled first copy of each response goes too.
	var rest []string
	for _, l := range kept {
		if !responses[l] {
			rest = append(rest, l)
		}
	}

	// Pass 3: title and body pairs, each once, in page order.
	var out []PageReview
	seen := map[string]bool{}
	for i := 0; i+1 < len(rest); i += 2 {
		title, body := rest[i], rest[i+1]
		key := title + "\n" + body
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, PageReview{Position: len(out) + 1, Title: title, Body: body})
	}
	return out
}

// storePageDocs reads the listing's web page with one basic extract and
// turns its reviews into documents, kind review, with the page URL as the
// source and the page position as the index. The extract is metered under
// ref like any Tavily call. It returns the documents and, when there are
// none, why.
func (g *Gatherer) storePageDocs(ctx context.Context, ref string, a *appstore.App, at time.Time) ([]Doc, float64, string, bool) {
	if a.StoreURL == "" {
		return nil, 0, "no App Store page URL to read reviews from", false
	}
	ex, used, err := g.extract(ctx, ref, []string{a.StoreURL})
	if err != nil {
		return nil, used, "App Store page could not be read: " + err.Error(), true
	}
	var page string
	for _, p := range ex.Pages {
		if p.URL == a.StoreURL {
			page = p.RawContent
		}
	}
	if page == "" {
		return nil, used, "App Store page could not be read: " + a.StoreURL, true
	}
	reviews := ParseStorePage(page)
	if len(reviews) == 0 {
		return nil, used, "App Store page shows no reviews", false
	}
	docs := make([]Doc, 0, len(reviews))
	for _, r := range reviews {
		docs = append(docs, Doc{
			Source: finding.Source{URL: a.StoreURL, Kind: KindReview, Title: r.Title, Index: r.Position, FetchedAt: at},
			Text:   r.Title + "\n" + r.Body,
		})
	}
	return docs, used, "", false
}
