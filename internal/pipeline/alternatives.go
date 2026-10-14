package pipeline

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

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
		MaxResults: 8,
	}
}

var (
	listicle   = regexp.MustCompile(`(?i)^(the )?(\d+|best|top|safe|safer|good|great|apps?|games?|channels?)\b|\b(like|alternatives?|vs\.?)\b`)
	reviewTail = regexp.MustCompile(`(?i)\s+(app|game|tv|website|youtube)?\s*review$`)
	titleCut   = regexp.MustCompile(`\s+[|–—-]\s+|:\s+`)
)

// candidateName is the title a search hit names, or "" when the hit is a
// list rather than one title: "Toca Life World App Review | Common Sense
// Media" names "Toca Life World"; "10 Best Games Like Roblox" names none.
func candidateName(hitTitle string) string {
	name := strings.TrimSpace(titleCut.Split(hitTitle, 2)[0])
	name = strings.TrimSpace(reviewTail.ReplaceAllString(name, ""))
	if name == "" || listicle.MatchString(name) {
		return ""
	}
	return name
}

// FindAlternatives runs the search and returns up to MaxAlternatives
// candidates, pending. A card that does not want alternatives gets none and
// no search. The search is metered under ref.
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
	self := appName(card.Title)
	seen := map[string]bool{self: true}
	var out []Alternative
	for _, h := range hits {
		name := candidateName(h.Title)
		key := appName(name)
		if name == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Alternative{Title: name, Reason: "Suggested by " + registrable(h.URL) + ": " + strings.TrimSpace(h.Title), Status: AltPending})
		if len(out) == MaxAlternatives {
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
