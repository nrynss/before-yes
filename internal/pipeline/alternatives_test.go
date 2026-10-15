package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/nrynss/keel/cost"

	"github.com/nrynss/before-yes/internal/appstore"
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
	if strings.HasPrefix(q.Query, s.main+" ") && !strings.HasSuffix(q.Query, " alternatives for kids") {
		return s.inner.Search(ctx, q)
	}
	one := tavily.Credits{Used: 1, Reported: true}
	if strings.HasPrefix(q.Query, "safer ") || strings.HasPrefix(q.Query, "best ") || strings.HasSuffix(q.Query, " alternatives for kids") {
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
	mu    sync.Mutex
	main  *recorder
	reads map[string]int
}

var titleLine = regexp.MustCompile(`(?m)^Title: (.+)$`)

func (r *altReader) Chat(ctx context.Context, msgs []model.Message, o model.Options) (model.Reply, error) {
	user := msgs[len(msgs)-1].Content
	if strings.HasPrefix(user, "Original title:") {
		var products []alternativeProduct
		for _, name := range []string{"Roblox", "Chatty World", "Calm Builder", "Blank Slate", "Fourth Pick"} {
			products = append(products, alternativeProduct{Name: name, Line: name})
		}
		b, _ := json.Marshal(map[string]any{"products": products})
		return model.Reply{Text: string(b), Price: 1000, Measured: true}, nil
	}
	title := titleLine.FindStringSubmatch(user)[1]
	fs, ok := altFindings[title]
	if _, page := altPages[title]; !page && !ok {
		return r.main.Chat(ctx, msgs, o)
	}
	r.mu.Lock()
	r.reads[title]++
	r.mu.Unlock()
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

func altE2E(t *testing.T) (*e2e, *altSearch, *altReader, Card) {
	t.Helper()
	e := newE2E(t, demos[0], review355())
	s := &altSearch{inner: e.p.Gatherer.Search, main: "Roblox", hits: []tavily.Result{
		{URL: "https://example.com/list", Title: "Games like Roblox", RawContent: "Roblox\nChatty World\nCalm Builder\nBlank Slate\nFourth Pick"},
	}}
	e.p.Gatherer.Store = alternativeStore{Store: e.p.Gatherer.Store}

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
	if card.App == nil || card.App.Genre != "Games" {
		t.Fatalf("App Store primary genre did not reach card: %+v", card.App)
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
	if s.altCalls != 9 {
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

// alternativeStore supplies independent product identities for the check-path
// fakes, retaining the real fixture store for the main title.
type alternativeStore struct{ Store }

func (s alternativeStore) Find(ctx context.Context, title string, limit int) ([]appstore.App, error) {
	for i, name := range []string{"Chatty World", "Calm Builder", "Blank Slate", "Fourth Pick"} {
		if title == name {
			return []appstore.App{{ID: int64(i + 100), Name: name}}, nil
		}
	}
	return s.Store.Find(ctx, title, limit)
}
func (s alternativeStore) Storefront() string { c, _ := storefrontOf(s.Store); return c }

func TestNanoQuoteWitness(t *testing.T) {
	text := "### [Garden Story](https://example.com/garden)\nScratchJr\n**Calm Builder** is our pick.\n"
	for _, tc := range []struct {
		name, line string
		want       bool
	}{
		{"Garden Story", "### [Garden Story](https://example.com/garden)", true},

		{"Garden", "Garden", false},
		{"Scratch", "ScratchJr", false},
		{"Invented", "ScratchJr", false},
		{"ScratchJr", "Scratch Jr", false},
		{"Calm Builder", "**Calm Builder** is our pick.", true},
		{"Calm Builder", "**Calm Builder** is our pick.\n", false},
		{"Garden Story", " ", false},
	} {
		if got := quotedProduct(text, alternativeProduct{Name: tc.name, Line: tc.line}); got != tc.want {
			t.Errorf("%q on %q = %v", tc.name, tc.line, got)
		}
	}
	if quotedProduct("A different page", alternativeProduct{Name: "ScratchJr", Line: "ScratchJr"}) {
		t.Error("cross-page quote accepted")
	}
}

type discoverySearch struct {
	hits    []tavily.Result
	queries []tavily.Search
}

func (s *discoverySearch) Search(_ context.Context, q tavily.Search) ([]tavily.Result, tavily.Credits, error) {
	s.queries = append(s.queries, q)
	return s.hits, tavily.Credits{Used: 1, Reported: true}, nil
}
func (s *discoverySearch) ExtractWithFailures(context.Context, []string) (tavily.Extracted, error) {
	return tavily.Extracted{}, nil
}

type discoveryReader struct {
	text  string
	calls int
}

func (r *discoveryReader) Chat(context.Context, []model.Message, model.Options) (model.Reply, error) {
	r.calls++
	return model.Reply{Text: r.text, Price: 1000, Measured: true}, nil
}
func (r *discoveryReader) Estimate([]model.Message, model.Options) cost.Price { return 2000 }
func (r *discoveryReader) Settings() model.Settings {
	return model.Settings{Provider: "fake", Model: "nano"}
}

type discoveryStore struct {
	Store
	apps  map[string][]appstore.App
	names []string
}

func (s *discoveryStore) Find(_ context.Context, name string, _ int) ([]appstore.App, error) {
	s.names = append(s.names, name)
	return s.apps[name], nil
}

func TestNanoDiscoveryConfirmsBeforeChecking(t *testing.T) {
	products := []alternativeProduct{
		{Name: "Invented", Line: "Actual Product"},   // invented name on real span
		{Name: "Lost Product", Line: "Lost Product"}, // invented span
		{Name: "No Such App", Line: "No Such App"},
		{Name: "Wrong Search Hit", Line: "Wrong Search Hit"},
		{Name: "Renamed Original", Line: "Renamed Original"},
		{Name: "Original", Line: "Original"},
		{Name: "Platform Experience", Line: "Platform Experience", RequiresOriginalPlatform: true},
		{Name: "Hidden Experience", Line: "Hidden Experience"},
		{Name: "Garden", Line: "Garden"},
		{Name: "One Product", Line: "One Product"},
		{Name: "Other Name", Line: "Other Name"}, // duplicate confirmed ID
		{Name: "Second Product", Line: "Second Product"},
		{Name: "Third Product", Line: "Third Product"},
		{Name: "Fourth Product", Line: "Fourth Product"},
	}
	page := "Actual Product\nNo Such App\nWrong Search Hit\nRenamed Original\nOriginal\nPlatform Experience\nHidden Experience\nGarden Story\nOne Product\nOther Name\nSecond Product\nThird Product\nFourth Product"
	raw, _ := json.Marshal(map[string]any{"products": products})
	reader := &discoveryReader{text: string(raw)}
	store := &discoveryStore{apps: map[string][]appstore.App{
		"Wrong Search Hit":  {{ID: 2, Name: "Unrelated Product"}},
		"Renamed Original":  {{ID: 1, Name: "Renamed Original"}},
		"Original":          {{ID: 3, Name: "Original: official app"}},
		"Hidden Experience": {{ID: 4, Name: "Hidden Experience", Description: "A game experience on Original for kids."}},
		"One Product":       {{ID: 5, Name: "One Product: Play"}},
		"Other Name":        {{ID: 5, Name: "Other Name"}},
		"Second Product":    {{ID: 6, Name: "Second Product"}},
		"Third Product":     {{ID: 7, Name: "Third Product"}},
		"Fourth Product":    {{ID: 8, Name: "Fourth Product"}},
	}}
	search := &discoverySearch{hits: []tavily.Result{{URL: "https://example.com/picks", Title: "Best picks", RawContent: page}}}
	meter := openTestMeter(t)
	p := &Pipeline{Gatherer: &Gatherer{Search: search, Store: store, Meter: meter}, Reader: &Reader{Model: reader, Meter: meter}}
	card := Card{Title: "Original", Age: 9, Verdict: checklist.No, Kind: KindIOS, App: &AppInfo{TrackID: 1, Genre: "Games"}}
	got, err := p.FindAlternatives(context.Background(), card, "discovery")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range got {
		names = append(names, a.Title)
		if a.Status != AltPending || !strings.Contains(a.Reason, "https://example.com/picks") {
			t.Errorf("provenance/status %v", a)
		}
	}
	if strings.Join(names, "|") != "One Product: Play|Second Product|Third Product" {
		t.Fatalf("confirmed %v", names)
	}
	if reader.calls != 1 || len(search.queries) != 3 {
		t.Errorf("calls reader=%d searches=%d", reader.calls, len(search.queries))
	}
	queries := map[string]bool{}
	for _, q := range search.queries {
		queries[q.Query] = true
		if !q.IncludeRaw {
			t.Error("no page text requested")
		}
	}
	if len(queries) != 3 || !strings.Contains(search.queries[0].Query, "games") {
		t.Errorf("queries %v", search.queries)
	}
	for _, name := range store.names {
		if name == "Invented" || name == "Lost Product" || name == "Platform Experience" || name == "Garden" || name == "Fourth Product" {
			t.Errorf("unverified/excess lookup %s", name)
		}
	}
	if spent(t, meter, spend.PoolModel, "discovery") != 1000 || spent(t, meter, spend.PoolTavily, "discovery") != 3*spend.CreditUnit {
		t.Error("discovery calls not metered")
	}
}

func TestNanoDiscoveryRefusalAndMalformed(t *testing.T) {
	e, _, _, card := altE2E(t)
	before := card
	m := refuseMeter{inner: e.meter, mark: "read-refused"}
	e.p.Reader.Meter = m
	got, _, err := e.p.WithAlternatives(context.Background(), card, "read-refused")
	if err == nil || !reflect.DeepEqual(got, before) {
		t.Error("Nano budget refusal changed card or was hidden")
	}
	e.p.Reader.Meter = e.meter
	e.p.Reader.Model = &discoveryReader{text: "not JSON"}
	pending, err := e.p.FindAlternatives(context.Background(), card, "bad-json")
	if err != nil || len(pending) != 0 {
		t.Errorf("malformed reply %v %v", pending, err)
	}
}

// These are actual metered Nano replies and free App Store responses, paired
// with the Tavily pages they read. Replay never needs a key or network.
type recordedNano struct {
	SourceURL string
	Page      tavily.Result
	Reply     model.Reply
}
type recordedReader struct {
	records map[string]recordedNano
	calls   int
}

func (r *recordedReader) Chat(_ context.Context, msgs []model.Message, _ model.Options) (model.Reply, error) {
	_, tail, _ := strings.Cut(msgs[len(msgs)-1].Content, "Source URL: ")
	url, _, _ := strings.Cut(tail, "\n")
	rec, ok := r.records[url]
	if !ok {
		return model.Reply{}, fmt.Errorf("missing recorded Nano response for %s", url)
	}
	text := rec.Page.RawContent
	if text == "" {
		text = rec.Page.Content
	}
	if !strings.HasSuffix(msgs[len(msgs)-1].Content, "PAGE TEXT\n"+text) {
		return model.Reply{}, fmt.Errorf("wrong response/page pairing for %s", url)
	}
	r.calls++
	return rec.Reply, nil
}
func (r *recordedReader) Estimate([]model.Message, model.Options) cost.Price { return 20000000 }
func (r *recordedReader) Settings() model.Settings {
	return model.Settings{Provider: "tokenfactory", Model: "nvidia/NVIDIA-Nemotron-3-Nano-30B-A3B"}
}

type recordedSearch struct {
	replies [][]tavily.Result
	n       int
}

func (s *recordedSearch) Search(context.Context, tavily.Search) ([]tavily.Result, tavily.Credits, error) {
	if s.n >= len(s.replies) {
		return nil, tavily.Credits{}, fmt.Errorf("extra search")
	}
	hits := s.replies[s.n]
	s.n++
	return hits, tavily.Credits{Used: 1, Reported: true}, nil
}
func (s *recordedSearch) ExtractWithFailures(context.Context, []string) (tavily.Extracted, error) {
	return tavily.Extracted{}, fmt.Errorf("unexpected extract")
}
func readRecorded(t *testing.T, path string, v any) {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, v); e != nil {
		t.Fatalf("%s: %v", path, e)
	}
}

func TestRecordedNanoAndAppStoreDiscovery(t *testing.T) {
	for _, tc := range []struct {
		slug, title, want string
		age               int
	}{
		{"roblox", "Roblox", "Stardew Valley|Toca Boca World: Game & Play|LEGO® Builder's Journey", 9},
		{"minecraft", "Minecraft", "Skip-Bo™: Solitaire Card Game|Tsuro - The Game of the Path|Similo: The Card Game", 9},
		{"khan-academy-kids", "Khan Academy Kids", "ABCmouse: Kids Learning Games", 5},
	} {
		t.Run(tc.slug, func(t *testing.T) {
			dir := filepath.Join("..", "..", "testdata", "alternatives", "nano", tc.slug)
			search := &recordedSearch{}
			for i := 1; i <= 3; i++ {
				var rec struct{ Results []tavily.Result }
				readRecorded(t, filepath.Join(dir, fmt.Sprintf("search-%02d.json", i)), &rec)
				search.replies = append(search.replies, rec.Results)
			}
			reader := &recordedReader{records: map[string]recordedNano{}}
			files, e := filepath.Glob(filepath.Join(dir, "nano-*.json"))
			if e != nil {
				t.Fatal(e)
			}
			for _, f := range files {
				var r recordedNano
				readRecorded(t, f, &r)
				reader.records[r.SourceURL] = r
			}
			store := &discoveryStore{apps: map[string][]appstore.App{}}
			lookups, e := filepath.Glob(filepath.Join(dir, "lookup-*.json"))
			if e != nil {
				t.Fatal(e)
			}
			var original appstore.App
			for i, f := range lookups {
				var r struct {
					Name string
					Apps []appstore.App
				}
				readRecorded(t, f, &r)
				store.apps[r.Name] = r.Apps
				if i == 0 {
					a := pickApp(r.Apps, tc.title)
					if a == nil {
						t.Fatal("original absent from captured lookup")
					}
					original = *a
				}
			}
			if original.Genre == "" {
				t.Fatal("captured genre absent")
			}
			meter := openTestMeter(t)
			p := &Pipeline{Gatherer: &Gatherer{Store: store, Search: search, Meter: meter}, Reader: &Reader{Model: reader, Meter: meter}}
			card := Card{Title: tc.title, Age: tc.age, Kind: KindIOS, Verdict: checklist.No, App: &AppInfo{TrackID: original.ID, Name: original.Name, Genre: original.Genre}}
			got, e := p.FindAlternatives(context.Background(), card, "recorded-"+tc.slug)
			if e != nil {
				t.Fatal(e)
			}
			var names []string
			for _, a := range got {
				names = append(names, a.Title)
				confirmed := false
				for _, apps := range store.apps {
					for _, app := range apps {
						if app.Name == a.Title && app.ID != original.ID {
							confirmed = true
						}
					}
				}
				if !confirmed {
					t.Errorf("not a recorded distinct App Store product: %s", a.Title)
				}
				witnessed := false
				for url, r := range reader.records {
					if !strings.Contains(a.Reason, url) {
						continue
					}
					var parsed struct{ Products []alternativeProduct }
					if json.Unmarshal([]byte(r.Reply.Text), &parsed) != nil {
						continue
					}
					text := r.Page.RawContent
					if text == "" {
						text = r.Page.Content
					}
					for _, product := range parsed.Products {
						if quotedProduct(text, product) && strings.Contains(a.Reason, product.Line) && confirmedProduct(store.apps[product.Name], product.Name) != nil {
							witnessed = true
						}
					}
				}
				if !witnessed {
					t.Errorf("no genuine same-page line/name/store witness: %+v", a)
				}
			}
			if strings.Join(names, "|") != tc.want {
				t.Errorf("recorded discoveries %v, want %s", names, tc.want)
			}
			if search.n != 3 || reader.calls != len(files) {
				t.Errorf("searches %d Nano calls %d, expected3/%d", search.n, reader.calls, len(files))
			}
			rejected := 0
			for name, apps := range store.apps {
				if name != tc.title && confirmedProduct(apps, name) == nil {
					rejected++
				}
			}
			if tc.slug != "roblox" && rejected == 0 {
				t.Error("capture did not exercise free non-product/identity rejection")
			}
		})
	}
}
