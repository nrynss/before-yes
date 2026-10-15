package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	// User decision: refused names are not shown; the card carries the one
	// line instead.
	if len(cards) != 0 || len(got.Alternatives) != 0 || got.AlternativesNote != MoreAlternativesNote {
		t.Fatalf("alternatives %+v note %q cards %d", got.Alternatives, got.AlternativesNote, len(cards))
	}
	got.Alternatives, got.AlternativesNote = card.Alternatives, card.AlternativesNote
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
// search's page text. The captures under testdata/alternatives are the
// ground truth for the layouts; this is the rule's unit-level shape.
// listHit wraps page text and a title as a search hit carries them.
func listHit(title, url, page string) tavily.Result {
	return tavily.Result{Title: title, URL: url, RawContent: page}
}

func TestListPageEntries(t *testing.T) {
	list := "Looking for something calmer?\n" +
		"## 1. **Calm Builder**\nA gentle building game.\n" +
		"## 2. **Roblox**\nThe title itself, skipped.\n" +
		"3) [Quiet Garden](https://example.com/qg) - grow plants, no chat\n" +
		"4. **This entry reads like a long sentence about many things**\n" +
		"5. **Animal Crossing: New Horizons** (Switch)\n" +
		"6. **Sixth Pick**\n"
	notRoblox := func(n string) bool { return sameTitle(n, "Roblox") }
	// On a page titled as a list, the ranked, marked entries are picks with
	// no corroborating hit needed (the T3.5 corroboration rule, redecided on
	// the captures: see TestCapturedListPagesYieldNames).
	got := listNames(listHit("10 Best Games Like Roblox for Kids", "https://www.example.com/list", list), notRoblox, nil)
	if strings.Join(got, "|") != "Calm Builder|Quiet Garden|Animal Crossing: New Horizons" {
		t.Errorf("listNames = %q", got)
	}
	// On a page titled as advice, the same markup names nothing.
	if got := listNames(listHit("How to Pick a Building Game", "https://www.example.com/guide", list), notRoblox, nil); len(got) != 0 {
		t.Errorf("advice page gave %q, want none", got)
	}
	e, s, _, card := altE2E(t)
	s.hits = []tavily.Result{
		{URL: "https://www.example.com/list", Title: "10 Best Games Like Roblox for Kids", RawContent: list},
		{URL: "https://play.google.com/store/apps/details?id=calm", Title: "Calm Builder - Apps on Google Play"},
		{URL: "https://apps.apple.com/us/app/quiet-garden/id2", Title: "\u200eQuiet Garden on the App Store"},
		{URL: "https://www.example.org/ac", Title: "Animal Crossing: New Horizons | Common Sense Media"},
	}
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
	if strings.Join(names, ",") != "Calm Builder,Quiet Garden,Animal Crossing: New Horizons" {
		t.Errorf("candidates %v", names)
	}
	if s.altCalls != 1 {
		t.Errorf("searches %d, want 1 (the list is read from the search's page text)", s.altCalls)
	}
}

// The round-2 reviewer's pages: section headings, advice steps and the title
// itself take no slot, and markdown is stripped from names.
// knownSet stands in for the titles of a search response's other hits.
func knownSet(names ...string) func(string) bool {
	return func(n string) bool {
		for _, k := range names {
			if appName(k) == appName(n) {
				return true
			}
		}
		return false
	}
}

func TestListPageRejectsHeadingsAndSteps(t *testing.T) {
	notRoblox := func(n string) bool { return sameTitle(n, "Roblox") }
	// The round-4 probe pages, on a page titled as advice: markup locates,
	// but nothing evidences the name, so none of these gives one.
	guide := func(page string) tavily.Result {
		return listHit("How to Keep Kids Safe in Roblox", "https://www.example.com/guide", page)
	}
	for _, page := range []string{
		"1. **Turn on parental controls**\n2. **Disable chat** in settings\n3. **Set a time limit**\n",
		"## **Why we like it**\n## **Our verdict**\n## **Pros and cons**\n",
		"1. **Note:** keep chat off.\n2. **Tip:** set a time limit.\n",
		"1. [Download the app](x)\n2. [Open settings](y)\n",
		"1. Download the app\n2. Open settings\n3. Disable chat\n",
		"1. Turn on parental controls\n2. Set a spending limit\n3. Talk to your kid\n",
		"1. **Stop Motion Studio**\n2. **Minecraft**\n",
	} {
		if got := listNames(guide(page), notRoblox, knownSet()); len(got) != 0 {
			t.Errorf("listNames(%q) = %q, want none", page, got)
		}
	}
	// The round-4 change, redecided on the captures: the same picks on a page
	// titled as a list are names with no corroborating hit.
	if got := listNames(listHit("12 Best Building Games for Kids", "https://www.example.com/best", "1. **Stop Motion Studio**\n2. **Minecraft**\n"), notRoblox, knownSet()); strings.Join(got, "|") != "Stop Motion Studio|Minecraft" {
		t.Errorf("list-page picks = %q", got)
	}
	cases := []struct {
		page  string
		known func(string) bool
		want  string
	}{
		{"## Why parents worry about Roblox\n## Our top picks\n1. **Minecraft**\n2. [Toca Life World](https://example.com/toca)\n3. Lego Builder\n## Conclusion\n",
			knownSet("Minecraft", "Toca Life World", "Lego Builder", "Our top picks", "Conclusion"), "Minecraft|Toca Life World|Lego Builder"},
		{"## How we chose\n## 1. Minecraft\n## 2. Toca\n", knownSet("Minecraft", "Toca"), "Minecraft|Toca"},
		{"## FAQ\n1. **Is Minecraft safe?**\n", knownSet("Is Minecraft safe?"), ""},
		{"1. **Roblox**\n2. *Minecraft*.\n3. **Minecraft**\n4. [`Toca Boca World`](u)\n## **Lego Builder**\n5. **Sky**\n",
			knownSet("Roblox", "Minecraft", "Toca Boca World", "Lego Builder", "Sky"), "Minecraft|Toca Boca World|Lego Builder"},
		{"1. **Stop Motion Studio**\n2. [Read Along by Google](u)\n3. **Pick a Path** - stories\n4. **Top Eleven**\n",
			knownSet("Stop Motion Studio", "Read Along by Google", "Pick a Path", "Top Eleven"), "Stop Motion Studio|Read Along by Google|Pick a Path"},
		{"## **Top Eleven**\n1. **download the app**\n", knownSet("Top Eleven", "download the app"), "Top Eleven"},
		{"1. **Stop Motion Studio**\n2. **Disable chat** in settings\n", knownSet("Stop Motion Studio"), "Stop Motion Studio"},
	}
	for _, c := range cases {
		if got := strings.Join(listNames(guide(c.page), notRoblox, c.known), "|"); got != c.want {
			t.Errorf("listNames(%q) = %q, want %q", c.page, got, c.want)
		}
	}
}

// The evidence forms a list entry can carry, one per case: the page's own
// list title behind a ranked, marked entry (E2), and another hit titled with
// the name (E3, the T3.5 corroboration, kept as one form). An unmarked
// ranked line needs the corroboration; a bolded one needs only the list
// title.
func TestListEntryEvidenceForms(t *testing.T) {
	page := "1. Download the app\n2. **Minecraft**\n3. Lego Builder\n4. Open settings\n"
	hits := []tavily.Result{
		listHit("Best Games Like Roblox", "https://www.example.com/list", page),
		{URL: "https://play.google.com/store/apps/details?id=lego", Title: "Lego Builder - Apps on Google Play"},
	}
	p := &Pipeline{Gatherer: &Gatherer{Search: &staticSearch{hits: hits}, Meter: openTestMeter(t)}}
	card := Card{Title: "Roblox", Age: 9, Verdict: checklist.No, Kind: KindIOS}
	pending, err := p.FindAlternatives(context.Background(), card, "check-1")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range pending {
		names = append(names, a.Title)
	}
	// Minecraft is bold and ranked on a list-titled page. Lego Builder is
	// corroborated by the Play Store hit. The unmarked advice lines around
	// them name nothing.
	if strings.Join(names, ",") != "Minecraft,Lego Builder" {
		t.Errorf("candidates %v", names)
	}
}

func TestStripMarkdown(t *testing.T) {
	for in, want := range map[string]string{
		"**Minecraft**": "Minecraft", "[Toca Life World](https://x.test/a)": "Toca Life World",
		"*Lego Builder*.": "Lego Builder", "`PBS Kids`": "PBS Kids", "## Sky:": "Sky", "__Calm__!": "Calm",
	} {
		if got := stripMarkdown(in); got != want {
			t.Errorf("stripMarkdown(%q) = %q, want %q", in, got, want)
		}
	}
}

// openChecksMeter is a spend meter whose checks_daily cap is limit, with
// used checks already booked for the day.
func openChecksMeter(t *testing.T, limit, used int) *spend.Meter {
	t.Helper()
	m, err := spend.Open(context.Background(), spend.Config{
		Path:  filepath.Join(t.TempDir(), "checks.db"),
		Pools: map[spend.Pool]spend.PoolConfig{spend.PoolChecksDaily: {Limit: cost.Price(limit)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	for i := 0; i < used; i++ {
		c, err := m.BeginCheck(context.Background(), fmt.Sprintf("earlier-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Done(); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func checksSpent(t *testing.T, m *spend.Meter) cost.Price {
	t.Helper()
	st, err := m.Status(context.Background(), spend.PoolChecksDaily)
	if err != nil {
		t.Fatal(err)
	}
	return st.Spent
}

// User decision: each fresh alternative check is one check against the
// 5-a-day cap; when the cap refuses, the candidate is refused, and the
// card hides refused names behind the one line.
func TestFreshAlternativeChecksCountAgainstCap(t *testing.T) {
	e, _, _, card := altE2E(t)
	checks := openChecksMeter(t, 5, 3) // two checks left today
	e.p.Checks = checks
	pending, err := e.p.FindAlternatives(context.Background(), card, "check-1")
	if err != nil || len(pending) != 3 {
		t.Fatalf("pending %v err %v", pending, err)
	}
	res := e.p.CheckAlternatives(context.Background(), card, pending, "check-1")
	if got := checksSpent(t, checks); got != 5 {
		t.Errorf("checks spent %d, want 5 (two fresh alternative checks booked)", got)
	}
	var statuses []string
	for _, a := range res.outcomes {
		statuses = append(statuses, a.Title+"="+a.Status)
	}
	// Chatty World and Calm Builder use the two checks; Chatty World does
	// not beat the card so it is not kept; Blank Slate is refused by the cap.
	if strings.Join(statuses, ",") != "Calm Builder=checked,Blank Slate=refused" || !res.Refused {
		t.Errorf("internal statuses %v refused %v", statuses, res.Refused)
	}
	shownAlts, note := res.Alternatives, res.Note
	if len(shownAlts) != 1 || shownAlts[0].Title != "Calm Builder" || note != MoreAlternativesNote {
		t.Errorf("shown %+v note %q", shownAlts, note)
	}
	b, _ := json.Marshal(Card{Alternatives: shownAlts, AlternativesNote: note})
	if strings.Contains(string(b), "Blank Slate") || !strings.Contains(string(b), `"alternatives_note":"More alternatives when today's checks reset"`) {
		t.Errorf("card JSON %s", b)
	}
}

// A cached alternative runs free: no check is reserved or booked, even
// with the cap spent, while a fresh one is refused.
func TestCachedAlternativeRunsFree(t *testing.T) {
	e, _, _, card := altE2E(t)
	if _, _, err := e.p.Collect(context.Background(), "Calm Builder", "warm"); err != nil {
		t.Fatal(err)
	}
	checks := openChecksMeter(t, 5, 5) // none left today
	e.p.Checks = checks
	pending, err := e.p.FindAlternatives(context.Background(), card, "check-1")
	if err != nil {
		t.Fatal(err)
	}
	res := e.p.CheckAlternatives(context.Background(), card, pending, "check-1")
	if got := checksSpent(t, checks); got != 5 {
		t.Errorf("checks spent %d, want 5 (the cached check books nothing)", got)
	}
	var statuses []string
	for _, a := range res.outcomes {
		statuses = append(statuses, a.Title+"="+a.Status)
	}
	if strings.Join(statuses, ",") != "Chatty World=refused,Calm Builder=checked,Blank Slate=refused" {
		t.Errorf("statuses %v", statuses)
	}
	if len(res.Cards) != 1 || res.Cards[0].Title != "Calm Builder" {
		t.Errorf("cards %+v", res.Cards)
	}
}

// With nothing refused, the card shows no note.
func TestNoNoteWithoutRefusal(t *testing.T) {
	alts, note := shown([]Alternative{{Title: "A", Status: AltChecked}, {Title: "B", Status: AltPending}})
	if len(alts) != 1 || alts[0].Title != "A" || note != "" {
		t.Errorf("shown %v note %q", alts, note)
	}
	b, _ := json.Marshal(Card{Alternatives: alts})
	if strings.Contains(string(b), "alternatives_note") {
		t.Errorf("card JSON %s", b)
	}
	if alts, _ := shown(nil); alts == nil {
		t.Error("shown(nil) must be an empty list, not null")
	}
}

// The two-step server hook (FindAlternatives, then CheckAlternatives, then
// Apply) never puts a refused name on a card or in its JSON, and neither
// does a card assembled by hand: the pending list is not shown, and Card's
// encoding drops anything not checked.
func TestServerHookPathHidesRefused(t *testing.T) {
	e, _, _, card := altE2E(t)
	e.p.Checks = openChecksMeter(t, 5, 3) // two checks left: Blank Slate is refused
	pending, err := e.p.FindAlternatives(context.Background(), card, "check-1")
	if err != nil || len(pending) != 3 {
		t.Fatalf("pending %v err %v", pending, err)
	}
	early := card
	early.Alternatives = pending // a server that shows the pending list anyway
	b, _ := json.Marshal(early)
	for _, a := range pending {
		if strings.Contains(string(b), a.Title) {
			t.Errorf("pending name %q in card JSON before the checks", a.Title)
		}
	}
	res := e.p.CheckAlternatives(context.Background(), card, pending, "check-1")
	for _, a := range res.Alternatives {
		if a.Status != AltChecked {
			t.Errorf("result shows %+v", a)
		}
	}
	if res.Note != MoreAlternativesNote {
		t.Errorf("note %q", res.Note)
	}
	final := res.Apply(card)
	b, _ = json.Marshal(final)
	if strings.Contains(string(b), "Blank Slate") || strings.Contains(string(b), `"refused"`) || !strings.Contains(string(b), "Calm Builder") ||
		!strings.Contains(string(b), MoreAlternativesNote) {
		t.Errorf("final card JSON %s", b)
	}
	// A card assembled from the internal outcomes still hides the refused
	// name (the note comes with Apply).
	raw := card
	raw.Alternatives = res.outcomes
	b, _ = json.Marshal(raw)
	if strings.Contains(string(b), "Blank Slate") || strings.Contains(string(b), `"refused"`) {
		t.Errorf("hand-assembled card JSON %s", b)
	}
	var back Card
	if err := json.Unmarshal(b, &back); err != nil || len(back.Alternatives) != 1 {
		t.Errorf("decoded %+v err %v", back.Alternatives, err)
	}
}

// A Tavily or model refusal partway through a fresh alternative's check,
// with the cap on, releases that check's reservation (Fail): nothing is
// booked, no hold stands, and the rest are refused.
func TestMidCheckBudgetRefusalReleasesCheck(t *testing.T) {
	e, _, _, card := altE2E(t)
	checks := openChecksMeter(t, 5, 0)
	e.p.Checks = checks
	m := refuseMeter{inner: e.meter, mark: "/alt1"}
	e.p.Gatherer.Meter, e.p.Reader.Meter, e.p.Judge.Meter = m, m, m
	pending, err := e.p.FindAlternatives(context.Background(), card, "check-1")
	if err != nil || len(pending) != 3 {
		t.Fatalf("pending %v err %v", pending, err)
	}
	res := e.p.CheckAlternatives(context.Background(), card, pending, "check-1")
	st, err := checks.Status(context.Background(), spend.PoolChecksDaily)
	if err != nil {
		t.Fatal(err)
	}
	if st.Spent != 0 || st.Remaining != 5 {
		t.Errorf("checks spent %d remaining %d, want 0 and 5 (the hold released)", st.Spent, st.Remaining)
	}
	if !res.Refused || len(res.Alternatives) != 0 || res.Note != MoreAlternativesNote {
		t.Errorf("result %+v", res)
	}
	for _, a := range res.outcomes {
		if a.Status != AltRefused {
			t.Errorf("outcome %+v, want refused", a)
		}
	}
}

// A struct that embeds Card encodes its own fields, and the card's
// alternatives still show no pending or refused name: the filter is on the
// Alternatives type, not on Card.
func TestEmbeddedCardKeepsFieldsAndHidesNames(t *testing.T) {
	card := Card{Title: "Roblox", Alternatives: Alternatives{
		{Title: "Calm Builder", Status: AltChecked},
		{Title: "Pending Pick", Status: AltPending},
		{Title: "Refused Pick", Status: AltRefused},
	}, AlternativesNote: MoreAlternativesNote}
	b, err := json.Marshal(struct {
		Card
		Extra int `json:"extra"`
	}{card, 7})
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, `"extra":7`) || !strings.Contains(got, `"title":"Roblox"`) || !strings.Contains(got, "Calm Builder") {
		t.Errorf("embedded JSON %s", got)
	}
	if strings.Contains(got, "Pending Pick") || strings.Contains(got, "Refused Pick") || !strings.Contains(got, MoreAlternativesNote) {
		t.Errorf("embedded JSON leaks or drops the note: %s", got)
	}
	if b, _ := json.Marshal(Card{}); !strings.Contains(string(b), `"alternatives":[]`) {
		t.Errorf("empty card alternatives %s", b)
	}
}

// The T3.12 captures: the live responses of the one search FindAlternatives
// makes for the three demo titles, under testdata/alternatives. The tests
// replay them through a static searcher; the captures are evidence and are
// never edited.

// altCapture loads one captured alternatives search and checks it is one.
func altCapture(t *testing.T, slug string) []tavily.Result {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "alternatives", slug, "tavily-search-alternatives.json"))
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Query   string          `json:"query"`
		Results []tavily.Result `json:"results"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(body.Query, "safer ") {
		t.Fatalf("capture %s is not an alternatives search: %q", slug, body.Query)
	}
	if len(body.Results) == 0 {
		t.Fatalf("capture %s has no results", slug)
	}
	return body.Results
}

// staticSearch answers every search with one fixed response.
type staticSearch struct {
	hits []tavily.Result
}

func (s *staticSearch) Search(context.Context, tavily.Search) ([]tavily.Result, tavily.Credits, error) {
	return s.hits, tavily.Credits{Used: 1, Reported: true}, nil
}

func (s *staticSearch) ExtractWithFailures(context.Context, []string) (tavily.Extracted, error) {
	return tavily.Extracted{}, errors.New("extract is not part of the alternatives search")
}

// replayPending runs FindAlternatives for one demo title against its
// captured search, on a card the server would have made: a no, kind ios,
// with no listing genre (the pipeline never fills AppInfo.Genre).
func replayPending(t *testing.T, slug, title string, age int) []Alternative {
	t.Helper()
	p := &Pipeline{Gatherer: &Gatherer{Search: &staticSearch{hits: altCapture(t, slug)}, Meter: openTestMeter(t)}}
	card := Card{Title: title, Age: age, Verdict: checklist.No, Kind: KindIOS}
	pending, err := p.FindAlternatives(context.Background(), card, "replay")
	if err != nil {
		t.Fatal(err)
	}
	return pending
}

// The Done-when: on the captured live results, each demo title's candidates
// are real game or app names from the pages the search returned, with the
// source page named in the reason, and no site or platform name, no
// question post, never the title itself, no duplicates, and at most
// MaxAlternatives.
func TestCapturedListPagesYieldNames(t *testing.T) {
	for _, c := range []struct {
		slug, title string
		age         int
		want        []string
		listedBy    string
	}{
		{"roblox", "Roblox", 9,
			[]string{"Theme Park Tycoon 2", "Build A Boat for Treasure", "My Hello Kitty Cafe"},
			"Listed by bitdefender.com: 12 Safe Roblox Games for Children Under 10 (2025 Guide)"},
		{"minecraft", "Minecraft", 9,
			[]string{"Blox 3D Junior", "Pango Build Safari", "RaceCraft"},
			"Listed by commonsensemedia.org: Games Like Minecraft | Common Sense Media"},
		{"khan-academy-kids", "Khan Academy Kids", 5,
			[]string{"Duolingo", "Lingokids", "ELSA Speak"},
			"Listed by samiolearning.com: Best learning apps for elementary students 2026"},
	} {
		pending := replayPending(t, c.slug, c.title, c.age)
		if len(pending) > MaxAlternatives {
			t.Errorf("%s: %d candidates, over the bound", c.title, len(pending))
		}
		var names []string
		seen := map[string]bool{}
		for _, a := range pending {
			names = append(names, a.Title)
			if seen[appName(a.Title)] {
				t.Errorf("%s: duplicate candidate %q", c.title, a.Title)
			}
			seen[appName(a.Title)] = true
			if sameTitle(a.Title, c.title) {
				t.Errorf("%s: the title itself is a candidate: %q", c.title, a.Title)
			}
			if platformNames[nameFold(a.Title)] {
				t.Errorf("%s: platform name %q is a candidate", c.title, a.Title)
			}
			if strings.Contains(a.Title, "?") {
				t.Errorf("%s: a question is a candidate: %q", c.title, a.Title)
			}
			if a.Status != AltPending || a.CardID != "" || a.Verdict != "" {
				t.Errorf("%s: candidate %+v is not pending", c.title, a)
			}
			if !strings.HasPrefix(a.Reason, "Listed by ") && !strings.HasPrefix(a.Reason, "Suggested by ") {
				t.Errorf("%s: reason %q does not name the source page", c.title, a.Reason)
			}
		}
		if strings.Join(names, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s: candidates %q, want %q", c.title, names, c.want)
		}
		if len(pending) == 0 || pending[0].Reason != c.listedBy {
			first := ""
			if len(pending) > 0 {
				first = pending[0].Reason
			}
			t.Errorf("%s: first reason %q, want %q", c.title, first, c.listedBy)
		}
	}
}

// The captured offenders never become candidates: the blogging platform hit
// titled "Medium", the blogs' own names in their pages' picks ("Mysh" on
// playmysh.com, "Samio Learning" on samiolearning.com, "TruPlay" on
// truplaygames.com), and the hit titles that are page names or questions.
func TestCapturedSiteNamesAndQuestionsRejected(t *testing.T) {
	for _, c := range []struct {
		name, title, url string
	}{
		{"Medium", "Medium", "https://sa-liberty.medium.com/your-kids-love-video-games-which-are-the-safest-ones-to-play-8ba311f201c6"},
		{"Mysh", "Safe Games Like Roblox for Kids: 8 Picks (2026) | Mysh Blog", "https://playmysh.com/blog/the-best-games-like-roblox-for-kids-in-2026"},
		{"Samio Learning", "Best learning apps for elementary students 2026 — 5 tested, the honest verdict — Sami", "https://samiolearning.com/best-learning-apps-for-kids"},
		{"TruPlay", "7 Alternatives to Roblox for Kids (2026 Parent Guide) | TruPlay", "https://www.truplaygames.com/blog/safe-alternatives-to-roblox-for-kids"},
	} {
		if !isSiteName(c.name, c.title, c.url) {
			t.Errorf("isSiteName(%q, %q) = false, want true", c.name, c.url)
		}
	}
	for _, c := range []struct{ name, title, url string }{
		{"Teacher-Recommended Apps", "Teacher-Recommended Apps | Field Trip Directory", "https://fieldtripdirectory.com/blog/teacher-recommended-apps-for-home-learning"},
		{"What are safe and fun app alternatives to Roblox for a 4- ...", "What are safe and fun app alternatives to Roblox for a 4- ...", "https://www.facebook.com/groups/688042038014714/posts/2920045021481060"},
		{"I don't want my 8 year old child playing Roblox anymore, so ...", "I don't want my 8 year old child playing Roblox anymore, so ...", "https://www.reddit.com/r/XboxGamePass/comments/1mpy0nj/i_dont_want_my_8_year_old_child_playing_roblox"},
	} {
		name := candidateName(c.name)
		if plausibleName(name) && !genericTail.MatchString(name) && !isSiteName(name, c.title, c.url) {
			t.Errorf("hit-title candidate %q (%s) is not rejected", name, c.url)
		}
	}
	for _, c := range []struct{ name, title, url string }{
		{"Toca Boca World", "7 Alternatives to Roblox for Kids (2026 Parent Guide) | TruPlay", "https://www.truplaygames.com/blog/safe-alternatives-to-roblox-for-kids"},
		{"PBS Kids Games", "The Best Early Learning Apps for Kids: A Parent and Teacher Guide", "https://blog.khanacademy.org/best-early-learning-apps-for-kids"},
		{"RaceCraft - Build & Race", "Games Like Minecraft | Common Sense Media", "https://www.commonsensemedia.org/lists/games-like-minecraft"},
	} {
		if isSiteName(c.name, c.title, c.url) {
			t.Errorf("isSiteName(%q on %q) = true, want false", c.name, c.url)
		}
	}
}

// boundLifted lists a hit's names with the maxListEntries bound lifted, the
// way the round-1 review probed: listNames runs again with the names already
// found (and the caller's own rejects) skipped, until nothing new comes.
func boundLifted(h tavily.Result, also func(string) bool) []string {
	var all []string
	for n := 0; n < 40; n++ {
		found := map[string]bool{}
		for _, name := range all {
			found[appName(name)] = true
		}
		got := listNames(h, func(name string) bool {
			return found[appName(name)] || also != nil && also(name)
		}, nil)
		if len(got) == 0 {
			return all
		}
		all = append(all, got...)
	}
	return all
}

// The round-1 M1 probes, pinned.
//
// A page titled as a list is the evidence E2 reads, so its own junk would
// ride in on it: a table of contents' one-word entries ("Games", "Apps",
// "Picks") and an advice section's bolded steps ("Set limits before bed",
// "Disable chat") all sit under ranked, marked lines there. A collection
// word is a column or a section label, never a title (a real title may end
// in one: "PBS Kids Games" stays, pinned below), and a sentence-shaped name
// is advice, not a title.
func TestListEntriesRejectCollectionNames(t *testing.T) {
	got := listNames(listHit("10 Best Games Like Roblox", "https://www.example.com/list", "1. **Games**\n2. **Apps**\n3. **Picks**\n"), nil, nil)
	if len(got) != 0 {
		t.Errorf("collection words on a list-titled page = %q, want none", got)
	}
}

func TestListEntriesRejectAdviceSteps(t *testing.T) {
	got := listNames(listHit("12 Best Building Games for Kids", "https://www.example.com/best", "1. **Set limits before bed**\n2. **Disable chat**\n"), nil, nil)
	if len(got) != 0 {
		t.Errorf("bolded advice steps on a list-titled page = %q, want none", got)
	}
}

// The round-2 L4 residual, pinned, not fixed: the same advice steps in
// title case are shape-identical to real picks ("Build A Boat for
// Treasure", "Hide and Seek Extreme"), so titleShape admits them and E2
// takes them. See the residual note on titleShape for why no shape check
// can separate them and why the captures never produce the shape. The
// sentence-cased shape above stays rejected.
func TestTitleCasedAdviceStepsAreAdmitted(t *testing.T) {
	got := listNames(listHit("12 Best Building Games for Kids", "https://www.example.com/best", "1. **Set Limits Before Bed**\n2. **Disable In-App Chat**\n"), nil, nil)
	if strings.Join(got, "|") != "Set Limits Before Bed|Disable In-App Chat" {
		t.Errorf("title-cased advice steps = %q, want the documented residual [Set Limits Before Bed|Disable In-App Chat]", got)
	}
}

// The captured Khan blog hit's comparison table is headed by an all-bold
// row, "| **App** | **Ages** | **Cost** | **Offline** | **Best For** |".
// Replayed with the bound lifted, that header row names no pick, while the
// data rows — bold name cell alone — still name theirs.
func TestCapturedTableHeaderNotAPick(t *testing.T) {
	var blog tavily.Result
	for _, h := range altCapture(t, "khan-academy-kids") {
		if strings.Contains(h.URL, "blog.khanacademy.org") {
			blog = h
			break
		}
	}
	if blog.URL == "" {
		t.Fatal("the khan capture has no blog.khanacademy.org hit")
	}
	all := boundLifted(blog, func(n string) bool { return sameTitle(n, "Khan Academy Kids") })
	for _, n := range all {
		if f := nameFold(n); f == "app" || f == "games" || f == "picks" {
			t.Errorf("the captured table's header cell %q is a candidate", n)
		}
	}
	for _, want := range []string{"PBS Kids Games", "ScratchJr", "Duolingo ABC"} {
		found := false
		for _, n := range all {
			if appName(n) == appName(want) {
				found = true
			}
		}
		if !found {
			t.Errorf("bound-lifted khan blog names %q miss the data row %q", all, want)
		}
	}
}

// The round-2 L5 probes, in the reviewer's shapes.
//
// (a) A pick's row may bold a badge in a later cell ("**Best overall**"):
// the row is the pick's, not a header, and its name stays a candidate. The
// captured data rows bold the name cell alone, so no captured pick moves.
func TestBadgeBoldRowKeepsItsPick(t *testing.T) {
	if boldHeaderRow("| **Toca Builders** | **Best overall** | 4-7 | None |") {
		t.Error("a badge-bold data row is taken for a header")
	}
	page := "| Game | Notes |\n| --- | --- |\n" +
		"| **Toca Builders** | **Best overall** — ages 4-7, no chat |\n" +
		"| **Block Craft 3D** | ages 6-9, builds on a phone |\n"
	got := listNames(listHit("7 Best Building Apps for Kids", "https://www.example.com/best", page), nil, nil)
	if strings.Join(got, "|") != "Toca Builders|Block Craft 3D" {
		t.Errorf("badge-bold rows = %q, want [Toca Builders|Block Craft 3D]", got)
	}
}

// (b) A header row that bolds only its first cell is still a header: its
// label names a column ("Best For" is one of the captured header cells),
// not a pick, with or without the delimiter row a markdown table puts under
// its header. The data row under it still names its pick.
func TestFirstCellOnlyHeaderNotAPick(t *testing.T) {
	if !boldHeaderRow("| **Best For** | Ages | Cost |") {
		t.Error("a first-cell-only header row is not taken for a header")
	}
	for _, c := range []struct{ what, under string }{
		{"with the delimiter row", "| --- | --- | --- |\n"},
		{"without the delimiter row", ""},
	} {
		page := "| **Best For** | Ages | Cost |\n" + c.under + "| **Toca Builders** | 4-7 | None |\n"
		got := listNames(listHit("7 Best Building Apps for Kids", "https://www.example.com/best", page), nil, nil)
		if strings.Join(got, "|") != "Toca Builders" {
			t.Errorf("first-cell-only header (%s) = %q, want [Toca Builders]", c.what, got)
		}
	}
}

// The round-1 L1 probes: a candidate whose name contains the reviewing
// host's label is still a title — "Duolingo Kids" on lingokids.com,
// "Minecraft Dungeons" on minecraft.net — while the captured site names stay
// rejected and the review's control stays clean.
func TestSiteNameNotSharedSubstring(t *testing.T) {
	for _, c := range []struct{ name, title, url string }{
		{"Duolingo Kids", "Best learning apps", "https://www.lingokids.com/blog/best-apps"},
		{"Minecraft Dungeons", "Games Like Minecraft", "https://www.minecraft.net/en-us/article/like-minecraft"},
		{"RaceCraft - Build & Race", "Games Like Minecraft", "https://racecraftgames.com/best"},
	} {
		if isSiteName(c.name, c.title, c.url) {
			t.Errorf("isSiteName(%q, …, %q) = true, want false (a title sharing the host's letters)", c.name, c.url)
		}
	}
	for _, c := range []struct{ name, title, url string }{
		{"Medium", "Medium", "https://sa-liberty.medium.com/your-kids-love-video-games-which-are-the-safest-ones-to-play-8ba311f201c6"},
		{"Mysh", "Safe Games Like Roblox for Kids: 8 Picks (2026) | Mysh Blog", "https://playmysh.com/blog/the-best-games-like-roblox-for-kids-in-2026"},
		{"Samio Learning", "Best learning apps for elementary students 2026 — 5 tested, the honest verdict — Sami", "https://samiolearning.com/best-learning-apps-for-kids"},
		{"TruPlay", "7 Alternatives to Roblox for Kids (2026 Parent Guide) | TruPlay", "https://www.truplaygames.com/blog/safe-alternatives-to-roblox-for-kids"},
	} {
		if !isSiteName(c.name, c.title, c.url) {
			t.Errorf("isSiteName(%q, …, %q) = false, want true (the site's own name)", c.name, c.url)
		}
	}
}

// The round-1 L3 probes: a nav-style heading whose address repeats inside
// the 600-character window is not evidenced, however it repeats — the
// second link on its own line with the same label, inline in a line of
// text, or on its own line under another label but with no blurb between —
// while Common Sense Media's real pick shape (its card link, an age line
// and a blurb, then "[See full review](…)" as its own line) still is, on a
// synthesized page and on the captured one, whose bound-lifted names are
// exactly its 20 review-page titles.
func TestRepeatLinkMustBeReadMore(t *testing.T) {
	addr := "https://www.example.com/best-kids-tv"
	for _, c := range []struct{ what, page string }{
		{"own line, same label", "### [Best Kids' TV](" + addr + ")\n[Best Kids' TV](" + addr + ")\n"},
		{"inline, same label", "### [Best Kids' TV](" + addr + ")\nTonight: [Best Kids' TV](" + addr + ")\n"},
		{"own line, other label, no blurb", "### [Best Kids' TV](" + addr + ")\n[Full schedule](" + addr + ")\n"},
	} {
		if got := listNames(listHit("Kids TV Guide", "https://www.example.com/tv", c.page), nil, nil); len(got) != 0 {
			t.Errorf("twice-linked nav heading (%s) = %q, want none", c.what, got)
		}
	}
	pick := "### [Blox 3D Junior](https://www.commonsensemedia.org/app-reviews/blox-3d-junior)\n\n" +
		"age 5+\n\nKids build block models in challenging creative app.\n\n" +
		"[See full review](https://www.commonsensemedia.org/app-reviews/blox-3d-junior)\n"
	got := listNames(listHit("Games Like Minecraft | Common Sense Media", "https://www.commonsensemedia.org/lists/games-like-minecraft", pick), nil, nil)
	if strings.Join(got, "|") != "Blox 3D Junior" {
		t.Errorf("CSM pick shape = %q, want [Blox 3D Junior]", got)
	}

	var csm tavily.Result
	for _, h := range altCapture(t, "minecraft") {
		if strings.Contains(h.URL, "commonsensemedia.org/lists/") {
			csm = h
			break
		}
	}
	if csm.URL == "" {
		t.Fatal("the minecraft capture has no commonsensemedia.org list hit")
	}
	all := boundLifted(csm, func(n string) bool { return sameTitle(n, "Minecraft") })
	if len(all) != 20 {
		t.Errorf("bound-lifted CSM names %d: %q", len(all), all)
	}
	for _, n := range all {
		if platformNames[nameFold(n)] {
			t.Errorf("bound-lifted CSM names a platform: %q", n)
		}
	}
}
