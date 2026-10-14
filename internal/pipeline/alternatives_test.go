package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/nrynss/keel/cost"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/finding"
	"github.com/nrynss/before-yes/internal/model"
	"github.com/nrynss/before-yes/internal/spend"
	"github.com/nrynss/before-yes/internal/tavily"
)

// The candidates' fake sources. Calm Builder answers every area with a
// setting, Chatty World has strangers in open chat, and Blank Slate says
// nothing an area can use.
var altPages = map[string]string{
	"Calm Builder": "Chat is turned off by default and a parent PIN is needed to turn it on. " +
		"There is no violent or scary content in any building level. " +
		"Purchases are blocked unless a parent approves them in settings. " +
		"No personal data is collected and there are no ads at all. " +
		"Child accounts are created by a parent with the real age. " +
		"The studio has no history of fines or lawsuits over children.",
	"Chatty World": "Strangers can message children in open chat with no filter at all.",
	"Blank Slate":  "A building game with bright colours.",
}

var altFindings = map[string][]finding.Raw{
	"Calm Builder": {
		{Area: "contact", Stance: "mitigation", Claim: "Chat is off by default", Quote: "Chat is turned off by default and a parent PIN is needed to turn it on"},
		{Area: "content", Stance: "mitigation", Claim: "No violent content", Quote: "There is no violent or scary content in any building level"},
		{Area: "money", Stance: "mitigation", Claim: "Purchases need a parent", Quote: "Purchases are blocked unless a parent approves them in settings"},
		{Area: "data", Stance: "mitigation", Claim: "No data collected", Quote: "No personal data is collected and there are no ads at all"},
		{Area: "age_gating", Stance: "mitigation", Claim: "Parent creates the account", Quote: "Child accounts are created by a parent with the real age"},
		{Area: "developer_record", Stance: "mitigation", Claim: "Clean record", Quote: "The studio has no history of fines or lawsuits over children"},
	},
	"Chatty World": {{Area: "contact", Stance: "risk", Claim: "Strangers can message children", Quote: "Strangers can message children in open chat with no filter at all"}},
}

// altSearch sends the main title's searches to the capture server and
// answers the alternatives search and the candidates' own searches itself.
type altSearch struct {
	inner    Searcher
	main     string
	hits     []tavily.Result
	altCalls int
}

func (s *altSearch) Search(ctx context.Context, q tavily.Search) ([]tavily.Result, tavily.Credits, error) {
	if strings.HasPrefix(q.Query, s.main+" ") {
		return s.inner.Search(ctx, q)
	}
	one := tavily.Credits{Used: 1, Reported: true}
	if strings.HasPrefix(q.Query, "safer ") {
		s.altCalls++
		return s.hits, one, nil
	}
	for name, text := range altPages {
		if strings.HasPrefix(q.Query, name+" ") && strings.HasSuffix(q.Query, "data breach") {
			return []tavily.Result{{URL: "https://news.example.com/" + strings.ReplaceAll(strings.ToLower(name), " ", "-"), Title: name + " for parents", Content: text, RawContent: text}}, one, nil
		}
	}
	return nil, one, nil
}

func (s *altSearch) ExtractWithFailures(ctx context.Context, urls []string) (tavily.Extracted, error) {
	return s.inner.ExtractWithFailures(ctx, urls)
}

// altReader reads the main title with the recorder and a candidate with
// its canned findings.
type altReader struct {
	main  *recorder
	reads map[string]int
}

var titleLine = regexp.MustCompile(`(?m)^Title: (.+)$`)

func (r *altReader) Chat(ctx context.Context, msgs []model.Message, o model.Options) (model.Reply, error) {
	user := msgs[len(msgs)-1].Content
	title := titleLine.FindStringSubmatch(user)[1]
	fs, ok := altFindings[title]
	if _, page := altPages[title]; !page && !ok {
		return r.main.Chat(ctx, msgs, o)
	}
	r.reads[title]++
	var out []finding.Raw
	for _, m := range segRef.FindAllStringSubmatch(user, -1) {
		for _, f := range fs {
			f.Ref = m[1]
			out = append(out, f)
		}
	}
	if out == nil {
		out = []finding.Raw{}
	}
	b, _ := json.Marshal(map[string]any{"findings": out})
	return model.Reply{Text: string(b), Price: 1000, Measured: true}, nil
}
func (r *altReader) Estimate([]model.Message, model.Options) cost.Price { return 2000 }
func (r *altReader) Settings() model.Settings {
	return model.Settings{Provider: "recorded", Model: "nano-recorded"}
}

func commonSense(name, kind string) tavily.Result {
	slug := strings.ReplaceAll(strings.ToLower(name), " ", "-")
	return tavily.Result{URL: "https://www.commonsensemedia.org/" + kind + "-reviews/" + slug, Title: name + " App Review | Common Sense Media"}
}

func altE2E(t *testing.T) (*e2e, *altSearch, *altReader, Card) {
	t.Helper()
	e := newE2E(t, demos[0], review355())
	s := &altSearch{inner: e.p.Gatherer.Search, main: "Roblox", hits: []tavily.Result{
		{URL: "https://www.example.com/list", Title: "10 Best Games Like Roblox for Kids"},
		commonSense("Roblox", "app"),
		commonSense("Chatty World", "app"),
		commonSense("Calm Builder", "app"),
		commonSense("Blank Slate", "app"),
		commonSense("Fourth Pick", "app"),
	}}
	r := &altReader{main: e.reader, reads: map[string]int{}}
	e.p.Gatherer.Search = s
	e.p.Reader.Model = r
	ev, _, err := e.p.Collect(context.Background(), "Roblox", "check-1")
	if err != nil {
		t.Fatal(err)
	}
	card, err := e.p.Decide(context.Background(), ev, 9, "check-1")
	if err != nil {
		t.Fatal(err)
	}
	if card.Verdict != checklist.No {
		t.Fatalf("main verdict %s, want no", card.Verdict)
	}
	return e, s, r, card
}

// TestNoTitleGetsABetterAlternative is the Done-when: the "no" title
// yields an alternative with a better verdict and its own card id.
func TestNoTitleGetsABetterAlternative(t *testing.T) {
	e, s, r, card := altE2E(t)
	pending, err := e.p.FindAlternatives(context.Background(), card, "check-1")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range pending {
		names = append(names, a.Title)
		if a.Status != AltPending || a.CardID != "" || a.Verdict != "" {
			t.Errorf("pending alternative %+v", a)
		}
	}
	// The list and the title itself are skipped; the count is bounded.
	if strings.Join(names, ",") != "Chatty World,Calm Builder,Blank Slate" {
		t.Errorf("candidates %v", names)
	}

	got, cards, err := e.p.WithAlternatives(context.Background(), card, "check-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Alternatives) != 1 || len(cards) != 1 {
		t.Fatalf("alternatives %+v, cards %d", got.Alternatives, len(cards))
	}
	a := got.Alternatives[0]
	if a.Title != "Calm Builder" || a.Status != AltChecked || a.Verdict != checklist.YesWithSettings || a.CardID == "" || a.CardID != cards[0].ID || a.CardID == card.ID {
		t.Errorf("alternative %+v (card %s)", a, cards[0].ID)
	}
	if cards[0].Age != card.Age || cards[0].Title != "Calm Builder" {
		t.Errorf("alternative card %s age %d", cards[0].Title, cards[0].Age)
	}
	// Each candidate was booked under its own ref.
	for i := 1; i <= 3; i++ {
		if spent(t, e.meter, spend.PoolTavily, fmt.Sprintf("check-1/alt%d", i)) == 0 {
			t.Errorf("alt%d booked nothing", i)
		}
	}
	// Cached like any title: a second run reads no candidate again.
	before := fmt.Sprint(r.reads)
	if _, _, err := e.p.WithAlternatives(context.Background(), card, "check-2"); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(r.reads) != before {
		t.Errorf("candidates re-read: %s → %v", before, r.reads)
	}
	if s.altCalls != 3 {
		t.Errorf("alternatives searches %d", s.altCalls)
	}
}

// refuseMeter refuses every call booked under a ref containing a marker,
// the way the meter refuses a call over its ceiling.
type refuseMeter struct {
	inner Meter
	mark  string
}

func (m refuseMeter) Spend(ctx context.Context, pool spend.Pool, kind, ref string, est cost.Price, w cost.Work) (cost.Usage, error) {
	if strings.Contains(ref, m.mark) {
		return cost.Usage{}, cost.ErrOverBudget
	}
	return m.inner.Spend(ctx, pool, kind, ref, est, w)
}

// A budget refusal leaves the main card intact: a refused candidate check
// stops the run and is marked refused, and a refused search leaves the
// card exactly as it was.
func TestBudgetRefusalLeavesMainCard(t *testing.T) {
	e, _, _, card := altE2E(t)
	keep, _ := json.Marshal(card)
	m := refuseMeter{inner: e.meter, mark: "/alt"}
	e.p.Gatherer.Meter, e.p.Reader.Meter, e.p.Judge.Meter = m, m, m
	got, cards, err := e.p.WithAlternatives(context.Background(), card, "check-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 0 || len(got.Alternatives) != 3 {
		t.Fatalf("alternatives %+v cards %d", got.Alternatives, len(cards))
	}
	for _, a := range got.Alternatives {
		if a.Status != AltRefused || a.CardID != "" {
			t.Errorf("refused alternative %+v", a)
		}
	}
	got.Alternatives = card.Alternatives
	if b, _ := json.Marshal(got); string(b) != string(keep) {
		t.Error("a refusal changed the main card")
	}

	m.mark = "check-9"
	e.p.Gatherer.Meter = m
	got, _, err = e.p.WithAlternatives(context.Background(), card, "check-9")
	if err == nil || !reflect.DeepEqual(got, card) {
		t.Errorf("refused search: err %v, card changed %v", err, !reflect.DeepEqual(got, card))
	}
}

func TestAlternativesOnlyForNoAndSettings(t *testing.T) {
	p := &Pipeline{}
	for _, v := range []checklist.Verdict{checklist.Yes, checklist.Unclear} {
		if alts, err := p.FindAlternatives(context.Background(), Card{Verdict: v}, "c"); alts != nil || err != nil {
			t.Errorf("%s: %v %v", v, alts, err)
		}
	}
	for _, c := range []struct {
		alt, orig checklist.Verdict
		want      bool
	}{
		{checklist.Yes, checklist.No, true}, {checklist.YesWithSettings, checklist.No, true},
		{checklist.Unclear, checklist.No, false}, {checklist.No, checklist.No, false},
		{checklist.Yes, checklist.YesWithSettings, true}, {checklist.YesWithSettings, checklist.YesWithSettings, false},
	} {
		if beats(c.alt, c.orig) != c.want {
			t.Errorf("beats(%s, %s) != %v", c.alt, c.orig, c.want)
		}
	}
}

func TestCandidateName(t *testing.T) {
	for in, want := range map[string]string{
		"Toca Life World App Review | Common Sense Media":     "Toca Life World",
		"Minecraft: Education Edition - Apps":                 "Minecraft: Education Edition",
		"Animal Crossing: New Horizons":                       "Animal Crossing: New Horizons",
		"\u200eToca Boca World: Game & Play on the App Store": "Toca Boca World: Game & Play",
		"Sample Game - Apps on Google Play":                   "Sample Game",
		"Sample Game on Steam":                                "Sample Game",
		"10 Best Games Like Roblox":                           "",
		"Safe alternatives to Roblox":                         "",
		"Top apps for kids":                                   "",
		"Roblox vs Minecraft":                                 "",
		"PBS Kids Games Game Review":                          "PBS Kids Games",
	} {
		if got := candidateName(in); got != want {
			t.Errorf("candidateName(%q) = %q, want %q", in, got, want)
		}
	}
}

// The round-1 attack cases: the title's own pages are never candidates.
func TestSelfInAnyFormSkipped(t *testing.T) {
	for _, h := range []string{"Roblox on the App Store", "Roblox Parents Guide", "Is Roblox Safe for Kids?", "\u200eRoblox - Apps on Google Play", "ROBLOX App Review | Common Sense Media"} {
		if !sameTitle(h, "Roblox") {
			t.Errorf("%q not taken as Roblox itself", h)
		}
	}
	if sameTitle("Robloxian Tycoon", "Roblox") {
		t.Error("a word containing the title is not the title")
	}
	e, s, _, card := altE2E(t)
	s.hits = append([]tavily.Result{
		{URL: "https://apps.apple.com/us/app/roblox/id1", Title: "\u200eRoblox on the App Store"},
		{URL: "https://www.example.com/guide", Title: "Roblox Parents Guide"},
		{URL: "https://www.example.com/safe", Title: "Is Roblox Safe for Kids?"},
	}, s.hits...)
	pending, err := e.p.FindAlternatives(context.Background(), card, "check-1")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range pending {
		names = append(names, a.Title)
	}
	if strings.Join(names, ",") != "Chatty World,Calm Builder,Blank Slate" {
		t.Errorf("candidates %v", names)
	}
}

// A list page's entries become candidates, bounded, from the one metered
// search's page text. No capture covers a live list page; this is a
// stand-in.
func TestListPageEntries(t *testing.T) {
	list := "Looking for something calmer?\n" +
		"## 1. Calm Builder\nA gentle building game.\n" +
		"## 2. Roblox\nThe title itself, skipped.\n" +
		"3) Quiet Garden - grow plants, no chat\n" +
		"4. This entry reads like a long sentence about many different things at once\n" +
		"5. Animal Crossing: New Horizons (Switch)\n" +
		"6. Sixth Pick\n"
	got := listNames(list)
	if strings.Join(got, "|") != "Calm Builder|Roblox|Quiet Garden" {
		t.Errorf("listNames = %q", got)
	}
	e, s, _, card := altE2E(t)
	s.hits = []tavily.Result{{URL: "https://www.example.com/list", Title: "10 Best Games Like Roblox for Kids", RawContent: list}}
	pending, err := e.p.FindAlternatives(context.Background(), card, "check-1")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range pending {
		names = append(names, a.Title)
		if !strings.HasPrefix(a.Reason, "Listed by example.com") {
			t.Errorf("reason %q", a.Reason)
		}
	}
	if strings.Join(names, ",") != "Calm Builder,Quiet Garden" {
		t.Errorf("candidates %v", names)
	}
	if s.altCalls != 1 {
		t.Errorf("searches %d, want 1 (the list is read from the search's page text)", s.altCalls)
	}
}
