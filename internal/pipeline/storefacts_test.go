package pipeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nrynss/before-yes/internal/appstore"
	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/finding"
)

// capturedStorePage is the listing page text the T3.8a capture extracted for
// a demo title.
func capturedStorePage(t *testing.T, slug string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "titles", slug, "tavily-extract-storepage.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ex struct {
		Results []struct {
			URL        string `json:"url"`
			RawContent string `json:"raw_content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(b, &ex); err != nil || len(ex.Results) == 0 {
		t.Fatalf("%s: store page capture: %v", slug, err)
	}
	return ex.Results[0].RawContent
}

type factWant struct {
	area   checklist.Area
	stance finding.Stance
	quote  string
}

// factsByArea collects the verified findings of facts as want-shaped rows.
func verifiedFacts(t *testing.T, facts []StoreFacts) []factWant {
	t.Helper()
	fs, drops := storeFactFindings(facts)
	if drops != (finding.Drops{}) {
		t.Errorf("store facts dropped by the quote check: %+v", drops)
	}
	var out []factWant
	for _, f := range fs {
		out = append(out, factWant{f.Area, f.Stance, f.Quote})
	}
	return out
}

func wantFacts(t *testing.T, got, want []factWant) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("facts = %d, want %d:\n%+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("fact %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestStoreFactsFromCapturedPages pins the page facts on the four captured
// listing pages: the price line (Khan: free, no in-app purchase mark;
// Minecraft, Roblox and Toca: the mark) and the privacy label (Khan: only
// "Data Not Linked to You", a mitigation; Minecraft: "Data Used to Track
// You", a risk; Roblox, Toca: "Data Linked to You", neutral, and no
// mitigation beside it).
func TestStoreFactsFromCapturedPages(t *testing.T) {
	linked := "Data Linked to You\nThe following data, which may be collected and linked to your identity, may be used for the following purposes:"
	cases := []struct {
		slug string
		want []factWant
	}{
		{"khan-academy-kids", []factWant{
			{checklist.Money, finding.Mitigation, "Reading & Math Learning Games\nFree"},
			{checklist.Data, finding.Mitigation, "Data Not Linked to You\nThe following data, which may be collected but is not linked to your identity, may be used for the following purposes:"},
		}},
		{"minecraft", []factWant{
			{checklist.Money, finding.Risk, "Explore, build, craft together\n$6.99 · In‑App Purchases"},
			{checklist.Data, finding.Risk, "Data Used to Track You\nThe following data may be used to track you across apps and websites owned by other companies:"},
			{checklist.Data, finding.Neutral, linked},
		}},
		{"roblox", []factWant{
			{checklist.Money, finding.Risk, "Play Millions of Games\nFree · In‑App Purchases"},
			{checklist.Data, finding.Neutral, linked},
		}},
		{"toca-life-world", []factWant{
			{checklist.Money, finding.Risk, "Create & Roleplay Family Life\nFree · In‑App Purchases · Designed for iPad"},
			{checklist.Data, finding.Neutral, linked},
		}},
	}
	for _, c := range cases {
		t.Run(c.slug, func(t *testing.T) {
			f, ok := pageFacts("https://apps.apple.com/us/app/x/id1", capturedStorePage(t, c.slug), fixedNow)
			if !ok {
				t.Fatal("no page facts")
			}
			wantFacts(t, verifiedFacts(t, []StoreFacts{f}), c.want)
			// Every quote is a span of the excerpt the findings cite.
			for _, r := range f.Raws {
				if !strings.Contains(finding.Normalize(f.Doc.Text), finding.Normalize(r.Quote)) {
					t.Errorf("quote not in the excerpt: %q", r.Quote)
				}
			}
			// And of the page itself, after the check's own normalization.
			page := finding.Normalize(capturedStorePage(t, c.slug))
			for _, r := range f.Raws {
				if !strings.Contains(page, finding.Normalize(r.Quote)) {
					t.Errorf("quote not in the captured page: %q", r.Quote)
				}
			}
		})
	}
}

func TestAppTrackingForAdsIsAStoreFact(t *testing.T) {
	quote := "App Tracking: To deliver personalized ads and improve application experience"
	page := "Sample App\nThe developer, Example, indicated that the app's privacy practices may include handling of data as described below.\n" + quote + "\nAccessibility\n"
	f, ok := pageFacts("https://apps.apple.com/us/app/sample/id1", page, fixedNow)
	if !ok {
		t.Fatal("no App Tracking fact")
	}
	wantFacts(t, verifiedFacts(t, []StoreFacts{f}), []factWant{{checklist.Data, finding.Risk, quote}})
}

// TestStoreFactsFromTheLookup pins the lookup facts on the captured App Store
// search: the rating is a neutral content finding, each advisory a content
// risk, and Roblox's "Loot Boxes" advisory a money risk.
func TestStoreFactsFromTheLookup(t *testing.T) {
	want := map[string][]factWant{
		"khan-academy-kids": {{checklist.Content, finding.Neutral, "Age rating: 4+"}},
		"toca-life-world":   {{checklist.Content, finding.Neutral, "Age rating: 4+"}},
		"minecraft": {
			{checklist.Content, finding.Neutral, "Age rating: 9+"},
			{checklist.Content, finding.Risk, "Content advisory: Infrequent/Mild Cartoon or Fantasy Violence"},
			{checklist.Content, finding.Risk, "Content advisory: Infrequent/Mild Contests"},
		},
		"roblox": {
			{checklist.Content, finding.Neutral, "Age rating: 12+"},
			{checklist.Content, finding.Risk, "Content advisory: Frequent/Intense Cartoon or Fantasy Violence"},
			{checklist.Content, finding.Risk, "Content advisory: Infrequent/Mild Realistic Violence"},
			{checklist.Content, finding.Risk, "Content advisory: Infrequent/Mild Horror/Fear Themes"},
			{checklist.Money, finding.Risk, "Content advisory: Loot Boxes"},
		},
	}
	for _, d := range demos {
		t.Run(d.slug, func(t *testing.T) {
			got, _, _ := gatherFixture(t, d)
			var lookup []StoreFacts
			for _, f := range got.StoreFacts {
				if f.Doc.Source.Title == lookupFactsTitle {
					if f.Doc.Source.URL != got.App.StoreURL || f.Doc.Source.Kind != KindStore {
						t.Errorf("lookup facts source = %+v, want the listing %s", f.Doc.Source, got.App.StoreURL)
					}
					lookup = append(lookup, f)
				}
			}
			if len(lookup) != 1 {
				t.Fatalf("lookup fact documents = %d, want 1", len(lookup))
			}
			wantFacts(t, verifiedFacts(t, lookup), want[d.slug])
		})
	}
}

// TestStoreFactsOnTheKhanGather pins what the Khan Academy Kids gather
// produces. The gatherer extracts the listing page on every gather (the
// fixture answers with the captured US page text), so the price line gives
// the money mitigation and the label the data one, cited to the listing's own
// URL, with exactly one extract of that page.
func TestStoreFactsOnTheKhanGather(t *testing.T) {
	got, f, _ := gatherFixture(t, demos[2])
	var page *StoreFacts
	for i, sf := range got.StoreFacts {
		if sf.Doc.Source.Title == pageFactsTitle {
			page = &got.StoreFacts[i]
		}
	}
	if page == nil {
		t.Fatalf("no page facts in %d fact documents", len(got.StoreFacts))
	}
	if page.Doc.Source.URL != got.App.StoreURL {
		t.Errorf("page facts cite %s, want the listing %s", page.Doc.Source.URL, got.App.StoreURL)
	}
	wantFacts(t, verifiedFacts(t, []StoreFacts{*page}), []factWant{
		{checklist.Money, finding.Mitigation, "Reading & Math Learning Games\nFree"},
		{checklist.Data, finding.Mitigation, "Data Not Linked to You\nThe following data, which may be collected but is not linked to your identity, may be used for the following purposes:"},
	})
	if f.storePageReads != 1 {
		t.Errorf("listing page extracts = %d, want 1", f.storePageReads)
	}
}

// The markdown form of a listing page, as the rating search returns another
// storefront's copy, still gives the label fact (the fallback when the
// gatherer's own extract is unavailable is the document of the same app id).
func TestStoreFactsFromAnotherStorefrontCopy(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "titles", "khan-academy-kids", "tavily-search-rating.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		Results []struct {
			URL        string `json:"url"`
			RawContent string `json:"raw_content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	var docs []Doc
	for _, r := range rec.Results {
		docs = append(docs, Doc{Source: finding.Source{URL: r.URL, Kind: KindRating}, Text: r.RawContent})
	}
	a := &appstore.App{ID: 1378467217, Name: "Khan Academy Kids", StoreURL: "https://apps.apple.com/us/app/khan-academy-kids/id1378467217?uo=4"}
	facts := storeFacts(a, docs, "", fixedNow)
	if len(facts) != 2 || facts[0].Doc.Source.URL != "https://apps.apple.com/mx/app/khan-academy-kids/id1378467217?l=en-GB" {
		t.Fatalf("facts = %+v", facts)
	}
	wantFacts(t, verifiedFacts(t, facts), []factWant{
		{checklist.Data, finding.Mitigation, "Data Not Linked to You\nThe following data may be collected but it is not linked to your identity:"},
		// The listing's Kids category line, for the rule gate
		// (contact.kids_category_no_contact_report); the judge is not shown it.
		{checklist.Contact, finding.Neutral, "Made For Ages 0–5"},
	})
	fs, _ := storeFactFindings(facts)
	for _, f := range fs {
		if gateOnly(f) != (f.Area == checklist.Contact) {
			t.Errorf("gateOnly(%q) = %v", f.Quote, gateOnly(f))
		}
	}
}

// A listing page that cannot be read costs the facts, not the gather: the
// call is a failure (the evidence is not cached), the lookup facts remain,
// and no page fact is produced.
func TestFailedStorePageExtractDoesNotFailTheGather(t *testing.T) {
	a := robloxApp()
	a.AgeRating = "12+"
	s := &stubSearch{pages: map[string]string{}}
	g := &Gatherer{Store: feedStub{apps: []appstore.App{a}, reviews: []appstore.Review{{Index: 1, Title: "t", Body: "b"}}}, Search: s, Meter: openTestMeter(t)}
	got, err := g.Gather(context.Background(), "Roblox", "c")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(got.Failures, "store_page") {
		t.Errorf("failures = %v, want store_page", got.Failures)
	}
	if len(got.StoreFacts) != 1 || got.StoreFacts[0].Doc.Source.Title != lookupFactsTitle {
		t.Errorf("store facts = %+v, want the lookup's alone", got.StoreFacts)
	}
}

// TestStoreFactsFromTheFallbackExtract pins the page facts when the gatherer
// extracted the listing itself (the feed fallback): the extract's text, and
// the listing's own URL.
func TestStoreFactsFromTheFallbackExtract(t *testing.T) {
	got, _, _ := gatherFixture(t, demos[2])
	a := got.App
	facts := storeFacts(a, nil, capturedStorePage(t, "khan-academy-kids"), fixedNow)
	if len(facts) != 2 || facts[1].Doc.Source.URL != a.StoreURL || facts[1].Doc.Source.Title != pageFactsTitle {
		t.Fatalf("facts = %+v", facts)
	}
	wantFacts(t, verifiedFacts(t, facts[1:]), []factWant{
		{checklist.Money, finding.Mitigation, "Reading & Math Learning Games\nFree"},
		{checklist.Data, finding.Mitigation, "Data Not Linked to You\nThe following data, which may be collected but is not linked to your identity, may be used for the following purposes:"},
	})
}

// A fact whose quote is not in its document does not pass.
func TestStoreFactsGoThroughTheQuoteCheck(t *testing.T) {
	f, ok := lookupFacts(&appstore.App{Name: "Khan Academy Kids", StoreURL: "https://apps.apple.com/us/app/khan-academy-kids/id1378467217?uo=4", AgeRating: "4+"}, fixedNow)
	if !ok {
		t.Fatal("no lookup facts")
	}
	f.Raws = append(f.Raws, fact(checklist.Money, finding.Mitigation, "invented", "Always free with no ads or purchases"))
	fs, drops := storeFactFindings([]StoreFacts{f})
	if len(fs) != 1 || drops.Unquoted != 1 {
		t.Errorf("findings %d, drops %+v: the invented quote must be dropped as unquoted", len(fs), drops)
	}
}

// The facts reach the evidence and the judge's prompt, and are not graded
// from directly: only the judge grades.
func TestCollectAddsStoreFacts(t *testing.T) {
	e := newE2E(t, demos[2], map[string][]finding.Raw{})
	ev, _, err := e.p.Collect(context.Background(), "Khan Academy Kids", "c")
	if err != nil {
		t.Fatal(err)
	}
	var got []factWant
	for _, f := range ev.Findings {
		if f.Source.Kind == KindStore {
			got = append(got, factWant{f.Area, f.Stance, f.Quote})
		}
	}
	wantFacts(t, got, []factWant{
		{checklist.Content, finding.Neutral, "Age rating: 4+"},
		{checklist.Money, finding.Mitigation, "Reading & Math Learning Games\nFree"},
		{checklist.Data, finding.Mitigation, "Data Not Linked to You\nThe following data, which may be collected but is not linked to your identity, may be used for the following purposes:"},
		{checklist.Contact, finding.Neutral, "Made For Ages 0–5"},
	})
	user := judgeUser("Khan Academy Kids", 5, []checklist.Area{checklist.Data}, ev.Findings, nil)
	if !strings.Contains(user, "Data Not Linked to You") || !strings.Contains(user, "stance=mitigation source=store") {
		t.Errorf("the judge's prompt does not carry the label fact:\n%s", user)
	}
}

// Change 3: the data definition separates service providers from selling and
// advertising, and both the judge and the entailment step read it.
func TestDataDefinitionNamesServiceProviders(t *testing.T) {
	def := areaDefs[checklist.Data]
	for _, s := range []string{"service providers", "is not a flag", "selling personal data", "behavioural advertising", "others' marketing"} {
		if !strings.Contains(def, s) {
			t.Errorf("the data definition lacks %q", s)
		}
	}
	if !strings.Contains(judgeSystem(), def) {
		t.Error("the judge's prompt does not carry the data definition")
	}
	if !strings.Contains(entailUser("Khan Academy Kids", 9, checklist.Grade{Area: checklist.Data, Level: checklist.Flag}, nil), def) {
		t.Error("the entailment prompt does not carry the data definition")
	}
}
