package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
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
// search's page text. No capture covers a live list page; this is a
// stand-in.
func TestListPageEntries(t *testing.T) {
	list := "Looking for something calmer?\n" +
		"## 1. **Calm Builder**\nA gentle building game.\n" +
		"## 2. **Roblox**\nThe title itself, skipped.\n" +
		"3) [Quiet Garden](https://example.com/qg) - grow plants, no chat\n" +
		"4. **This entry reads like a long sentence about many things**\n" +
		"5. **Animal Crossing: New Horizons** (Switch)\n" +
		"6. **Sixth Pick**\n"
	if got := listNames(list, nil, nil); len(got) != 0 {
		t.Errorf("uncorroborated listNames = %q, want none", got)
	}
	got := listNames(list, func(n string) bool { return sameTitle(n, "Roblox") },
		knownSet("Calm Builder", "Roblox", "Quiet Garden", "Animal Crossing: New Horizons", "Sixth Pick"))
	if strings.Join(got, "|") != "Calm Builder|Quiet Garden|Animal Crossing: New Horizons" {
		t.Errorf("listNames = %q", got)
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
	// The round-4 probe pages: markup is no title evidence, so none of these
	// gives a name without a corroborating hit.
	for _, page := range []string{
		"1. **Turn on parental controls**\n2. **Disable chat** in settings\n3. **Set a time limit**\n",
		"## **Why we like it**\n## **Our verdict**\n## **Pros and cons**\n",
		"1. **Note:** keep chat off.\n2. **Tip:** set a time limit.\n",
		"1. [Download the app](x)\n2. [Open settings](y)\n",
		"1. Download the app\n2. Open settings\n3. Disable chat\n",
		"1. Turn on parental controls\n2. Set a spending limit\n3. Talk to your kid\n",
		"1. **Stop Motion Studio**\n2. **Minecraft**\n",
	} {
		if got := listNames(page, notRoblox, knownSet()); len(got) != 0 {
			t.Errorf("listNames(%q) = %q, want none", page, got)
		}
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
		if got := strings.Join(listNames(c.page, notRoblox, c.known), "|"); got != c.want {
			t.Errorf("listNames(%q) = %q, want %q", c.page, got, c.want)
		}
	}
}

// A list entry counts only when the same search response has a hit titled
// with that name.
func TestListEntryCorroboratedBySearch(t *testing.T) {
	e, s, _, card := altE2E(t)
	s.hits = []tavily.Result{
		{URL: "https://www.example.com/list", Title: "Best Games Like Roblox", RawContent: "1. Download the app\n2. **Minecraft**\n3. Lego Builder\n4. Open settings\n"},
		{URL: "https://play.google.com/store/apps/details?id=lego", Title: "Lego Builder - Apps on Google Play"},
	}
	pending, err := e.p.FindAlternatives(context.Background(), card, "check-1")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range pending {
		names = append(names, a.Title)
	}
	// Minecraft is bold but has no hit of its own, so markup alone does not
	// make it a candidate.
	if strings.Join(names, ",") != "Lego Builder" {
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
	for _, a := range res.Alternatives {
		statuses = append(statuses, a.Title+"="+a.Status)
	}
	// Chatty World and Calm Builder use the two checks; Chatty World does
	// not beat the card so it is not kept; Blank Slate is refused by the cap.
	if strings.Join(statuses, ",") != "Calm Builder=checked,Blank Slate=refused" || !res.Refused {
		t.Errorf("internal statuses %v refused %v", statuses, res.Refused)
	}
	shownAlts, note := shown(res.Alternatives)
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
	for _, a := range res.Alternatives {
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
	if len(alts) != 2 || note != "" {
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
