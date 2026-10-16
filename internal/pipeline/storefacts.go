package pipeline

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nrynss/before-yes/internal/appstore"
	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/finding"
)

// StoreFacts is what Apple's own listing states about a title, as a stored
// document and the findings that quote it. No model is in the loop: the
// findings are written by code from fixed strings the listing carries, and
// they pass the same verification as the reader's (finding.Verify, a string
// match of the quote against the document), so a fact reaches the judge only
// as a verified quote. The judge grades from them like from any other
// finding; nothing here grades.
//
// The reassuring facts (free, no in-app purchases, rated 4+, a privacy label
// of "Data Not Linked to You") are the ones a reader rarely quotes, and a
// grade of "ok" needs a citation, so without them a clean title stays
// "unknown". The facts that cut the other way (in-app purchases, content
// advisories, "Data Used to Track You") are risk findings and are never
// dropped.
type StoreFacts struct {
	// Doc is the excerpt of the listing the quotes are spans of. Its URL is
	// the listing's page, Kind is KindStore.
	Doc Doc
	// Raws are the findings, as the reader would write them, each with Ref
	// StoreFactRef.
	Raws []finding.Raw
}

// StoreFactRef is the segment ref the fact findings carry.
const StoreFactRef = "S"

// factTitle titles the excerpt documents.
const (
	lookupFactsTitle = "App Store listing: price, rating and advisories"
	pageFactsTitle   = "App Store listing: price line and privacy label"
	kidsFactsTitle   = "App Store listing: Kids category"
)

var (
	// priceLine is the line under a listing's name and subtitle: the price
	// and the in-app purchase mark, as the captures show them ("Free",
	// "$6.99 · In‑App Purchases", "Free · In‑App Purchases · Designed for
	// iPad").
	priceLine = regexp.MustCompile(`^(Free|[$€£¥]\s?\d[\d.,]*)(\s+·\s+.*)?$`)
	idInURL   = regexp.MustCompile(`/id(\d+)(?:[/?#]|$)`)
	// madeForAges is the Kids category line: "Made For Ages 0–5".
	madeForAges = regexp.MustCompile(`(?i)^made for ages (\d+)\s*[–-]\s*(\d+)$`)
)

// storeFacts reads the facts of app a. The lookup's own fields always give
// the rating and advisories. The page facts (the in-app purchase mark and the
// privacy label) need the listing's page text, which is not fetched for them
// (that would be a paid call on every title): it is taken from the page the
// gatherer already extracted for the reviews (page), or else from a gathered
// document that is a store page of the same app id, which the rating search
// returns. A page with neither mark nor label gives no page facts.
func storeFacts(a *appstore.App, docs []Doc, page string, at time.Time) []StoreFacts {
	var out []StoreFacts
	if f, ok := lookupFacts(a, at); ok {
		out = append(out, f)
	}
	pageURL := a.StoreURL
	if page == "" {
		for _, d := range docs {
			if d.Source.Kind != KindReview && d.Source.Kind != KindStore && isListingOf(d.Source.URL, a.ID) {
				page, pageURL = d.Text, d.Source.URL
				break
			}
		}
	}
	if page != "" {
		if f, ok := pageFacts(pageURL, page, at); ok {
			out = append(out, f)
		}
	}
	// The Kids category line is read from the page, and failing that from any
	// gathered listing of the same app, whatever the storefront: the category
	// does not change with the country.
	if f, ok := kidsFacts(pageURL, page, at); ok {
		out = append(out, f)
	} else {
		for _, d := range docs {
			if d.Source.Kind != KindReview && d.Source.Kind != KindStore && isListingOf(d.Source.URL, a.ID) {
				if f, ok := kidsFacts(d.Source.URL, d.Text, at); ok {
					out = append(out, f)
					break
				}
			}
		}
	}
	return out
}

// isListingOf reports whether raw is an apps.apple.com listing page of the
// app with this id, in any storefront.
func isListingOf(raw string, id int64) bool {
	u, err := url.Parse(raw)
	if err != nil || strings.ToLower(u.Hostname()) != "apps.apple.com" {
		return false
	}
	m := idInURL.FindStringSubmatch(u.Path)
	return m != nil && m[1] == strconv.FormatInt(id, 10)
}

func fact(area checklist.Area, st finding.Stance, claim, quote string) finding.Raw {
	return finding.Raw{Ref: StoreFactRef, Area: string(area), Stance: string(st), Claim: claim, Quote: quote}
}

// lookupFacts turns the lookup record's age rating and advisories into a
// document and findings.
//
// Area. Apple's rating and advisories describe the material in the title, so
// they go to content (areaDefs: unsuitable material a child may meet). They
// do not go to age_gating: that area is whether the gate fails in practice,
// and areaDefs says the rating is "a claim, not proof the gate works".
//
// An advisory naming loot boxes goes to money instead (see below).
//
// Stance. The rating on its own is neutral: it is a fixed label and whether
// it suits a child depends on the child's age, which the judge knows and the
// evidence does not. Each advisory is a risk: Apple names a kind of material
// the title contains, and a parent should see it whatever the age.
func lookupFacts(a *appstore.App, at time.Time) (StoreFacts, bool) {
	var lines []string
	var raws []finding.Raw
	if r := strings.TrimSpace(a.AgeRating); r != "" {
		line := "Age rating: " + r
		lines = append(lines, line)
		raws = append(raws, fact(checklist.Content, finding.Neutral,
			"Apple's App Store rates the app "+r+".", line))
	}
	for _, adv := range a.Advisories {
		adv = strings.TrimSpace(adv)
		if adv == "" {
			continue
		}
		line := "Content advisory: " + adv
		lines = append(lines, line)
		// "Loot Boxes" is the one advisory about money, not material, and
		// loot boxes are the first thing areaDefs names for the money area.
		area := checklist.Content
		if strings.Contains(strings.ToLower(adv), "loot box") {
			area = checklist.Money
		}
		raws = append(raws, fact(area, finding.Risk,
			"Apple's App Store lists a content advisory: "+adv+".", line))
	}
	if len(raws) == 0 || strings.TrimSpace(a.StoreURL) == "" {
		return StoreFacts{}, false
	}
	return StoreFacts{
		Doc: Doc{
			Source: finding.Source{URL: a.StoreURL, Kind: KindStore, Title: lookupFactsTitle, FetchedAt: at},
			Text:   strings.Join(lines, "\n"),
		},
		Raws: raws,
	}, true
}

// pageFacts reads the price line and the privacy label off a listing page's
// text. The document is an excerpt: the two lines that carry the price, and
// the first copy of the label's headings, each heading with the line under
// it, so every quote is a contiguous span of the excerpt as it is of the page.
//
// Money (areaDefs: purchases pushed at a child; a price on its own is not
// that). The price line says whether the listing carries Apple's in-app
// purchase mark. With the mark, the app sells inside itself, which is the
// ground of the money area, so it is a risk. Without it, the listing offers
// nothing to buy inside the app, a protection for the area, so it is a
// mitigation. A price alone, with no mark, is not purchase pressure; the claim
// says what the price line shows and no more. No "Contains Ads" line exists on
// the listing page in any capture, so no ad fact is read; advertising is
// covered by the privacy label below.
//
// Data (areaDefs: ad tracking, third-party sharing or selling). The label is
// the developer's own statement of what the app collects:
//   - "Data Used to Track You" is cross-app tracking for advertising, the
//     definition's ad tracking: a risk.
//   - "Data Linked to You" is collection tied to the child's identity. The
//     definition does not make collection a flag (a service provider holding
//     data to run the app is not), so it is neutral: shown, not weighed
//     against the title.
//   - "Data Not Linked to You" alone, with neither of the other headings, says
//     the developer reports nothing linked to the child or used to track
//     them: a mitigation. Beside the other headings it says nothing new and
//     gives no finding.
//
// The label is the developer's self-report and Apple says it has not verified
// it; the claims say "Apple's label shows the developer reports".
func pageFacts(pageURL, page string, at time.Time) (StoreFacts, bool) {
	lines := strings.Split(strings.ReplaceAll(page, "\r", ""), "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	// plain is a line without the list and heading marks of the page's
	// markdown form ("* ### Data Not Linked to You", as the rating search
	// returns another storefront's listing).
	plain := func(l string) string { return strings.TrimSpace(strings.TrimLeft(l, "*#+ \t")) }
	var blocks []string
	var raws []finding.Raw

	// The price line: within the first lines, under the subtitle.
	for i := 1; i < len(lines) && i < 8; i++ {
		if lines[i-1] == "" || !priceLine.MatchString(lines[i]) {
			continue
		}
		quote := lines[i-1] + "\n" + lines[i]
		blocks = append(blocks, quote)
		if strings.Contains(finding.Normalize(lines[i]), "in-app purchases") {
			raws = append(raws, fact(checklist.Money, finding.Risk,
				"The App Store listing's price line carries the in-app purchases mark: "+lines[i]+".", quote))
		} else {
			raws = append(raws, fact(checklist.Money, finding.Mitigation,
				"The App Store listing's price line ("+lines[i]+") shows no in-app purchases.", quote))
		}
		break
	}

	// The label: the first copy, from the "The developer, X, indicated that
	// the app's privacy practices ..." line to the second such line (the page
	// repeats the label) or the next section.
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(plain(l), "The developer, ") {
			start = i
			break
		}
	}
	if start >= 0 {
		var label []string
		seenDev := 0
		for _, l := range lines[start:] {
			if pl := plain(l); pl == "Accessibility" || pl == "Information" {
				break
			}
			if strings.HasPrefix(plain(l), "The developer, ") {
				seenDev++
				if seenDev == 2 {
					break
				}
			}
			label = append(label, l)
		}
		heading := func(h string) (string, bool) {
			for i, l := range label {
				if plain(l) != h {
					continue
				}
				for _, next := range label[i+1:] {
					if next != "" {
						return h + "\n" + plain(next), true
					}
				}
			}
			return "", false
		}
		track, hasTrack := heading("Data Used to Track You")
		linked, hasLinked := heading("Data Linked to You")
		notLinked, hasNotLinked := heading("Data Not Linked to You")
		if hasTrack {
			blocks = append(blocks, track)
			raws = append(raws, fact(checklist.Data, finding.Risk,
				"Apple's privacy label shows the developer reports that data may be used to track the user across apps and websites owned by other companies.", track))
		}
		for _, l := range label {
			pl := plain(l)
			lw := gateWords(pl)
			if strings.HasPrefix(strings.ToLower(pl), "app tracking:") && trackingLabel(lw) {
				blocks = append(blocks, l)
				raws = append(raws, fact(checklist.Data, finding.Risk,
					"The App Store privacy label shows the developer reports app tracking for ads.", l))
				break
			}
		}
		if hasLinked {
			blocks = append(blocks, linked)
			raws = append(raws, fact(checklist.Data, finding.Neutral,
				"Apple's privacy label shows the developer reports collecting data linked to the user's identity.", linked))
		}
		if hasNotLinked && !hasTrack && !hasLinked {
			blocks = append(blocks, notLinked)
			raws = append(raws, fact(checklist.Data, finding.Mitigation,
				"Apple's privacy label shows the developer reports only data not linked to the user's identity, and none used to track the user.", notLinked))
		}
	}

	if len(raws) == 0 {
		return StoreFacts{}, false
	}
	return StoreFacts{
		Doc: Doc{
			Source: finding.Source{URL: pageURL, Kind: KindStore, Title: pageFactsTitle, FetchedAt: at},
			Text:   strings.Join(blocks, "\n\n"),
		},
		Raws: raws,
	}, true
}

// storeFactFindings verifies the facts' quotes against their own documents
// with the same check as the reader's findings, and returns the findings that
// pass, in order. A fact whose quote is not in its document, or is shorter
// than finding.MinQuote, is dropped and counted in the returned Drops.
func storeFactFindings(facts []StoreFacts) ([]finding.Finding, finding.Drops) {
	var out []finding.Finding
	var drops finding.Drops
	for i, f := range facts {
		ref := fmt.Sprintf("%s%d", StoreFactRef, i+1)
		raws := make([]finding.Raw, len(f.Raws))
		for j, r := range f.Raws {
			r.Ref = ref
			raws[j] = r
		}
		fs, d := finding.Verify(raws, map[string]finding.Segment{ref: {Text: f.Doc.Text, Source: f.Doc.Source}})
		out = append(out, fs...)
		drops.Add(d)
	}
	return out, drops
}

// kidsCategoryClaim opens the claim of the Kids category fact. gateOnly keys
// on it.
const kidsCategoryClaim = "The App Store lists the title in its Kids category"

// gateOnly reports whether a finding is evidence for a rule of the rule gate
// and is not shown to the judge, so the judge's prompt, and the recorded
// replies keyed on it, do not change when such a fact is added.
func gateOnly(f finding.Finding) bool {
	return f.Source.Kind == KindStore && f.Area == checklist.Contact && strings.HasPrefix(f.Claim, kidsCategoryClaim)
}

// kidsFacts reads Apple's Kids category line off a listing page: "Made For
// Ages 0–5" under Age Rating. The rule gate reads it
// (contact.kids_category_no_contact_report); the judge is not shown it
// (gateOnly), because it is evidence for that rule and not a point for the
// judge to weigh. Neutral: it says where the store lists the title, not what
// happens in it. The quote is the line itself, a span of the excerpt.
func kidsFacts(pageURL, page string, at time.Time) (StoreFacts, bool) {
	for _, l := range strings.Split(strings.ReplaceAll(page, "\r", ""), "\n") {
		line := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "*#+ \t"))
		m := madeForAges.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		return StoreFacts{
			Doc: Doc{
				Source: finding.Source{URL: pageURL, Kind: KindStore, Title: kidsFactsTitle, FetchedAt: at},
				Text:   line,
			},
			Raws: []finding.Raw{fact(checklist.Contact, finding.Neutral, kidsCategoryClaim+", made for ages "+m[1]+"–"+m[2]+".", line)},
		}, true
	}
	return StoreFacts{}, false
}
