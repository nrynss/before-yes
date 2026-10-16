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
	// devResponse is the label line of a developer response, exactly: the
	// words and a date, as every capture shows ("Developer Response
	// 01/30/2023", "Developer Response Jun 22"). A review titled
	// "Developer Response is slow" is not a label.
	devResponse  = regexp.MustCompile(`^Developer Response (\d{1,2}/\d{1,2}/\d{2,4}|[A-Z][a-z]{2,8} \d{1,2}(, \d{4})?)$`)
	reviewsStart = "Ratings & Reviews"
	reviewsEnd   = regexp.MustCompile(`^What[’']s New$`)
	editorsPick  = regexp.MustCompile(`^Editors[’']? Choice$`)
)

// ParseStorePage reads the reviews off an extracted App Store page. The
// T3.8a captures fix the shape:
//
//   - reviews sit between the "Ratings & Reviews" heading and "What's New";
//   - a review is a title line and a body, with nothing else marking it;
//   - each block appears twice in a row;
//   - a developer response appears in the first copy as bare lines at the
//     end, and in the repeat after a "Developer Response <date>" line;
//   - an "Editors' Choice" block is an editorial blurb.
//
// Block boundaries come from the doubling, not from counting lines: at each
// line the parser looks for the shortest run of lines that is followed at
// once by its own repeat, where the repeat may carry one response label
// the first copy lacks. That run is one block: its first line is the
// title, the lines before the label's place are the body, and the lines
// after it are the response, which is dropped. A line that starts no
// doubled block (a rating summary, "See All", other page chrome) is
// skipped. An Editors' Choice block is dropped whole. Blocks are kept once
// by content and numbered from 1 after the drops. A page with no such
// section, or none left, has no reviews.
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
	var sec []string
	for _, l := range lines[start:] {
		l = strings.TrimSpace(l)
		if reviewsEnd.MatchString(l) {
			break
		}
		if l != "" {
			sec = append(sec, l)
		}
	}

	var out []PageReview
	seen := map[string]bool{}
	for i := 0; i < len(sec); {
		n, resp, used := doubledBlock(sec, i)
		if n == 0 {
			i++ // chrome: no block starts here
			continue
		}
		block := sec[i : i+n]
		i += used
		if editorsPick.MatchString(block[0]) {
			continue
		}
		title, body := block[0], strings.Join(block[1:resp], "\n")
		key := title + "\n" + body
		if body == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, PageReview{Position: len(out) + 1, Title: title, Body: body})
	}
	return out
}

// doubledBlock finds the shortest block of at least two lines starting at
// i that is repeated straight after itself. It returns the block's length
// n, the index within the block where a developer response begins (n when
// there is none), and how many lines the block and its repeat use. n is 0
// when no block starts at i.
func doubledBlock(sec []string, i int) (n, resp, used int) {
	for k := 2; i+2*k <= len(sec)+1; k++ {
		if i+k >= len(sec) || sec[i+k] != sec[i] {
			continue
		}
		first := sec[i : i+k]
		p, q, label := 0, i+k, -1
		ok := true
		for p < k {
			if q >= len(sec) {
				ok = false
				break
			}
			if label < 0 && p > 1 && devResponse.MatchString(sec[q]) && !devResponse.MatchString(first[p]) {
				label = p
				q++
				continue
			}
			if sec[q] != first[p] {
				ok = false
				break
			}
			p++
			q++
		}
		if !ok {
			continue
		}
		if label < 0 {
			label = k
		}
		return k, label, q - i
	}
	return 0, 0, 0
}

// readStorePage reads the listing's web page with one basic extract, metered
// under ref like any Tavily call. It returns the page text and the credits
// used. When the page could not be read it returns an empty text, why, and
// failed true for an errored call (the evidence is then not cached) or false
// when there is simply no page URL.
func (g *Gatherer) readStorePage(ctx context.Context, ref string, a *appstore.App) (page string, used float64, why string, failed bool) {
	if a.StoreURL == "" {
		return "", 0, "no App Store page URL to read reviews from", false
	}
	ex, used, err := g.extract(ctx, ref, []string{a.StoreURL})
	if err != nil {
		return "", used, "App Store page could not be read: " + err.Error(), true
	}
	for _, p := range ex.Pages {
		if p.URL == a.StoreURL {
			page = p.RawContent
		}
	}
	if page == "" {
		return "", used, "App Store page could not be read: " + a.StoreURL, true
	}
	return page, used, "", false
}

// storePageDocs turns the reviews on the listing's page text into documents,
// kind review, with the page URL as the source and the page position as the
// index. It returns the documents and, when there are none, why.
func storePageDocs(page string, a *appstore.App, at time.Time) ([]Doc, string) {
	reviews := ParseStorePage(page)
	if len(reviews) == 0 {
		return nil, "App Store page shows no reviews"
	}
	docs := make([]Doc, 0, len(reviews))
	for _, r := range reviews {
		docs = append(docs, Doc{
			Source: finding.Source{URL: a.StoreURL, Kind: KindReview, Title: r.Title, Index: r.Position, FetchedAt: at},
			Text:   r.Title + "\n" + r.Body,
		})
	}
	return docs, ""
}
