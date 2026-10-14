package pipeline

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/nrynss/keel/cost"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/tavily"
)

// Safer alternatives (T3.5).
//
// A card that is no or yes with settings gets up to MaxAlternatives
// candidates in the same genre for the same age, found with one Tavily
// search. Each candidate is checked like any title, through Collect (so it
// is cached like any title) and Decide, under its own ref, after the main
// card is made. Only a candidate whose verdict beats the original's is
// kept. The meter can refuse any of these calls; a refusal stops the run,
// marks the candidates not yet checked as refused, and never touches the
// main card.

// MaxAlternatives bounds how many candidates are checked for one card.
const MaxAlternatives = 3

// Alternative statuses.
const (
	// AltPending: found, not checked yet. A card shown before its
	// alternatives finish carries these.
	AltPending = "pending"
	// AltChecked: checked, with a verdict that beats the original's.
	AltChecked = "checked"
	// AltRefused: not checked, because the meter refused a call.
	AltRefused = "refused"
)

// wantsAlternatives reports whether a verdict gets alternatives.
func wantsAlternatives(v checklist.Verdict) bool {
	return v == checklist.No || v == checklist.YesWithSettings
}

// rank orders verdicts for "beats". Unclear never beats anything: it says
// nothing is known, which is not safer.
func rank(v checklist.Verdict) int {
	switch v {
	case checklist.Yes:
		return 3
	case checklist.YesWithSettings:
		return 2
	case checklist.No:
		return 1
	}
	return 0
}

func beats(alt, orig checklist.Verdict) bool {
	return alt != checklist.Unclear && rank(alt) > rank(orig)
}

// alternativesSearch is the one Tavily search that finds candidates.
func alternativesSearch(card Card) tavily.Search {
	genre := "app"
	switch {
	case card.Kind == KindYouTube:
		genre = "YouTube channel"
	case card.Kind == KindConsole:
		genre = "video game"
	case card.App != nil && card.App.Genre != "":
		genre = strings.ToLower(card.App.Genre)
	}
	return tavily.Search{
		Query:      fmt.Sprintf("safer %s like %s for a %d year old", genre, card.Title, card.Age),
		MaxResults: 8, IncludeRaw: true,
	}
}

var (
	listicle = regexp.MustCompile(`(?i)^(the )?(\d+|best|top|safe|safer|good|great|apps?|games?|channels?)\b|\b(like|alternatives?|vs\.?)\b`)
	// siteSuffix is the tail a site puts on its own page titles. Only these
	// are cut; a ": " is kept, so "Animal Crossing: New Horizons" stays
	// whole.
	siteSuffix = regexp.MustCompile(`(?i)(\s+[|–—-]\s+.*|\s+on the app store|\s+on steam|\s+(app|game|tv|website|youtube)?\s*review)$`)
	// rankedEntry is a list page's numbered entry, bare or as a heading:
	// "1. Minecraft", "2) **Toca Life World**", "## 3. PBS Kids Games".
	rankedEntry = regexp.MustCompile(`(?m)^[ \t]*(?:#{1,4}[ \t]*)?\d{1,2}[.)][ \t]+(.+?)[ \t]*$`)
	// markedHeading is an unnumbered heading that is only a link or bold
	// text, the way list pages mark an entry: "## [Lego Builder](url)".
	markedHeading = regexp.MustCompile(`(?m)^[ \t]*#{2,4}[ \t]+(\*\*[^*]+\*\*|\[[^\]]+\]\([^)]*\))[ \t]*$`)
	// markedLead is a name marked as a title at the start of an entry: a
	// bold span or a link, as list pages mark each pick ("**Minecraft** -
	// great for builders", "[Toca Life World](url)", "[**Toca**](url)").
	markedLead = regexp.MustCompile(`^(?:\*\*([^*]+)\*\*|__([^_]+)__|\[([^\]]+)\]\([^)]*\))`)
	// mdLink, mdEmph and mdCode are markdown around an entry's name.
	mdLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	mdEmph = regexp.MustCompile(`\*{1,2}([^*]+)\*{1,2}|__([^_]+)__`)
	mdCode = regexp.MustCompile("`([^`]*)`")
)

// maxListEntries bounds how many names one list page can add.
const maxListEntries = MaxAlternatives

// cleanName strips invisible format characters (the App Store puts U+200E
// before its page titles) and spaces.
func cleanName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// candidateName is the title a search hit names, or "" when the hit is a
// list rather than one title: "Toca Life World App Review | Common Sense
// Media" names "Toca Life World"; "10 Best Games Like Roblox" names none.
func candidateName(hitTitle string) string {
	name := siteName(hitTitle)
	if name == "" || listicle.MatchString(name) {
		return ""
	}
	return name
}

// siteName is a page title with invisible characters and known site
// suffixes removed. A list entry uses it without the list-title check, so a
// marked "Top Eleven" stays a name.
func siteName(hitTitle string) string {
	name := cleanName(hitTitle)
	for {
		cut := strings.TrimSpace(siteSuffix.ReplaceAllString(name, ""))
		if cut == name {
			break
		}
		name = cut
	}
	return name
}

// stripMarkdown takes the name out of an entry's markdown: links, bold,
// italics, code, leading heading marks and trailing punctuation.
func stripMarkdown(s string) string {
	s = mdLink.ReplaceAllString(s, "$1")
	s = mdEmph.ReplaceAllString(s, "$1$2")
	s = mdCode.ReplaceAllString(s, "$1")
	s = strings.TrimLeft(strings.TrimSpace(s), "# ")
	return strings.TrimRight(strings.TrimSpace(s), ".,;:!*_ ")
}

// listNames are the titles a list page names, in page order. An entry is a
// ranked line ("1. X", "## 1. X") or a heading that is only a link or bold
// text; a leading bold or link span is taken as its name. It counts only
// when corroborated: the same search response has a hit titled with that
// name (known). Markup alone is no evidence, since pages bold advice steps
// and section headings too. Questions and names over six words are dropped.
// Repeats on the page and names skip rejects (the title itself, names
// already taken) are dropped before the bound is applied, so they use no
// slot; at most maxListEntries are returned.
func listNames(text string, skip, known func(string) bool) []string {
	type at struct {
		pos  int
		text string
	}
	var entries []at
	for _, re := range []*regexp.Regexp{rankedEntry, markedHeading} {
		for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
			entries = append(entries, at{m[0], text[m[2]:m[3]]})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].pos < entries[j].pos })
	var out []string
	own := map[string]bool{}
	for _, e := range entries {
		raw := strings.TrimSpace(e.text)
		if m := markedLead.FindStringSubmatch(raw); m != nil {
			raw = m[1] + m[2] + m[3]
		}
		entry := stripMarkdown(raw)
		entry, _, _ = strings.Cut(entry, " - ")
		entry, _, _ = strings.Cut(entry, " – ")
		entry, _, _ = strings.Cut(entry, " (")
		name := stripMarkdown(siteName(entry))
		if !plausibleName(name) {
			continue
		}
		if known == nil || !known(name) {
			continue
		}
		if own[appName(name)] || (skip != nil && skip(name)) {
			continue
		}
		own[appName(name)] = true
		out = append(out, name)
		if len(out) == maxListEntries {
			break
		}
	}
	return out
}

// plausibleName is the cheap check, with no call, that a name could be a
// title before it costs a Collect: not empty, not a question, at most six
// words, and starting with a capital letter or a digit.
func plausibleName(name string) bool {
	if name == "" || strings.Contains(name, "?") || len(strings.Fields(name)) > 6 {
		return false
	}
	r := []rune(name)[0]
	return unicode.IsUpper(r) || unicode.IsDigit(r)
}

// sameTitle reports whether a hit is about the title itself in any form:
// its words, in order, inside the hit's ("Roblox on the App Store",
// "Roblox Parents Guide", "Is Roblox Safe for Kids?").
func sameTitle(hitText, title string) bool {
	words := func(s string) string {
		return " " + strings.Join(strings.FieldsFunc(strings.ToLower(cleanName(s)), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}), " ") + " "
	}
	t := words(title)
	return strings.TrimSpace(t) != "" && strings.Contains(words(hitText), t)
}

// FindAlternatives runs the search and returns up to MaxAlternatives
// candidates, pending. A card that does not want alternatives gets none and
// no search. The search is metered under ref, and asks for page text so a
// list page's entries can be read without another call.
func (p *Pipeline) FindAlternatives(ctx context.Context, card Card, ref string) ([]Alternative, error) {
	if !wantsAlternatives(card.Verdict) {
		return nil, nil
	}
	if p.Gatherer == nil {
		return nil, errors.New("pipeline: a gatherer is required")
	}
	hits, _, err := p.Gatherer.search(ctx, ref, alternativesSearch(card))
	if err != nil {
		return nil, fmt.Errorf("alternatives: search: %w", err)
	}
	seen := map[string]bool{}
	var out []Alternative
	taken := func(name string) bool {
		key := appName(name)
		return name == "" || key == "" || seen[key] || sameTitle(name, card.Title)
	}
	add := func(name, reason string) bool {
		if taken(name) {
			return false
		}
		key := appName(name)
		seen[key] = true
		out = append(out, Alternative{Title: name, Reason: reason, Status: AltPending})
		return len(out) == MaxAlternatives
	}
	// titled is every single-title name in this response, for list entries
	// with no markup to be corroborated by.
	titled := map[string]bool{}
	for _, h := range hits {
		if name := candidateName(h.Title); name != "" {
			titled[appName(name)] = true
		}
	}
	known := func(name string) bool { return titled[appName(name)] }
	for _, h := range hits {
		site := registrable(h.URL)
		if name := candidateName(h.Title); name != "" {
			if sameTitle(h.Title, card.Title) {
				continue // the title's own page, guide or safety article
			}
			if add(name, "Suggested by "+site+": "+cleanName(h.Title)) {
				break
			}
			continue
		}
		text := h.RawContent
		if text == "" {
			text = h.Content
		}
		full := false
		for _, name := range listNames(text, taken, known) {
			if full = add(name, "Listed by "+site+": "+cleanName(h.Title)); full {
				break
			}
		}
		if full {
			break
		}
	}
	return out, nil
}

// AltResult is the outcome of checking a card's candidates.
type AltResult struct {
	// Alternatives are the candidates that beat the original (checked),
	// then any the meter kept from being checked (refused).
	Alternatives []Alternative
	// Cards are the kept alternatives' own cards, to store beside the
	// main card so each alternative links to its card.
	Cards []Card
	// Refused reports that the meter refused a call and the run stopped.
	Refused bool
}

// CheckAlternatives checks each pending candidate for the card's age, in
// order, under ref + "/alt<n>". A candidate that fails for any other
// reason is dropped and the run goes on; a budget refusal stops it.
func (p *Pipeline) CheckAlternatives(ctx context.Context, card Card, pending []Alternative, ref string) AltResult {
	var res AltResult
	for i, alt := range pending {
		if res.Refused {
			alt.Status = AltRefused
			res.Alternatives = append(res.Alternatives, alt)
			continue
		}
		altRef := fmt.Sprintf("%s/alt%d", ref, i+1)
		ev, _, err := p.Collect(ctx, alt.Title, altRef)
		var c Card
		if err == nil {
			c, err = p.Decide(ctx, ev, card.Age, altRef)
		}
		if errors.Is(err, cost.ErrOverBudget) {
			res.Refused = true
			alt.Status = AltRefused
			res.Alternatives = append(res.Alternatives, alt)
			continue
		}
		if err != nil || !beats(c.Verdict, card.Verdict) {
			continue
		}
		alt.CardID, alt.Verdict, alt.Status = c.ID, c.Verdict, AltChecked
		res.Alternatives = append(res.Alternatives, alt)
		res.Cards = append(res.Cards, c)
	}
	return res
}

// WithAlternatives finds and checks a card's alternatives and returns a
// copy of the card carrying them, with the alternatives' own cards. The
// main card is made before this runs and is complete without it: on any
// error, including a refused search, the card comes back as it was, with
// no alternatives.
func (p *Pipeline) WithAlternatives(ctx context.Context, card Card, ref string) (Card, []Card, error) {
	pending, err := p.FindAlternatives(ctx, card, ref)
	if err != nil || len(pending) == 0 {
		return card, nil, err
	}
	res := p.CheckAlternatives(ctx, card, pending, ref)
	out := card
	out.Alternatives = append([]Alternative{}, res.Alternatives...)
	return out, res.Cards, nil
}
