package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/nrynss/before-yes/internal/appstore"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nrynss/keel/cost"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/finding"
	"github.com/nrynss/before-yes/internal/model"
	"github.com/nrynss/before-yes/internal/spend"
)

// ruleJudge stands in for Super. It grades from the findings in its prompt
// alone: an area with a risk is flagged on its risks, an area with only a
// mitigation is ok with a setting, and anything else is unknown.
type ruleJudge struct {
	price cost.Price
	mu    sync.Mutex
	calls int
}

var findingLine = regexp.MustCompile(`(?m)^- id=(\S+) area=(\S+) stance=(\S+) `)
var onlyLine = regexp.MustCompile(`(?m)^Grade only these areas: (.+)$`)

func (r *ruleJudge) Chat(_ context.Context, msgs []model.Message, _ model.Options) (model.Reply, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	user := msgs[len(msgs)-1].Content
	areas := []checklist.Area{}
	if m := onlyLine.FindStringSubmatch(user); m != nil {
		for _, a := range strings.Split(m[1], ", ") {
			areas = append(areas, checklist.Area(a))
		}
	} else {
		for _, a := range checklist.Areas() {
			areas = append(areas, a.ID)
		}
	}
	risks, mits := map[string][]string{}, map[string][]string{}
	for _, m := range findingLine.FindAllStringSubmatch(user, -1) {
		switch m[3] {
		case "risk":
			risks[m[2]] = append(risks[m[2]], m[1])
		case "mitigation":
			mits[m[2]] = append(mits[m[2]], m[1])
		}
	}
	var gs []checklist.Grade
	for _, a := range areas {
		g := checklist.Grade{Area: a, Level: checklist.Unknown, Findings: []string{}, Reason: "Not enough evidence."}
		if ids := risks[string(a)]; len(ids) > 0 {
			g = checklist.Grade{Area: a, Level: checklist.Flag, Findings: append(ids, mits[string(a)]...), Reason: "A risk stands."}
		} else if ids := mits[string(a)]; len(ids) > 0 {
			g = checklist.Grade{Area: a, Level: checklist.OKWithSetting, Setting: "Parental controls", Findings: ids, Reason: "A setting answers it."}
		}
		gs = append(gs, g)
	}
	b, _ := json.Marshal(map[string]any{"grades": gs})
	return model.Reply{Text: string(b), Price: r.price, Measured: true}, nil
}
func (r *ruleJudge) Estimate([]model.Message, model.Options) cost.Price { return 2 * r.price }
func (r *ruleJudge) Settings() model.Settings {
	return model.Settings{Provider: "recorded", Model: "super-rules"}
}

type e2e struct {
	p      *Pipeline
	f      *fixture
	reader *recorder
	judge  *ruleJudge
	meter  *spend.Meter
}

func newE2E(t *testing.T, d demo, byRef map[string][]finding.Raw) *e2e {
	t.Helper()
	f, srv := newFixture(t, d)
	store, tc := fixtureClients(t, srv)
	m := openTestMeter(t)
	rec := &recorder{byRef: byRef, price: 1200}
	rj := &ruleJudge{price: 90000}
	now := func() time.Time { return fixedNow }
	return &e2e{
		p: &Pipeline{
			Gatherer: &Gatherer{Store: store, Search: tc, Meter: m, Now: now},
			Reader:   &Reader{Model: rec, Meter: m},
			Judge:    &Judge{Model: rj, Meter: m},
			Cache:    &MemCache{},
			Now:      now,
		},
		f: f, reader: rec, judge: rj, meter: m,
	}
}

func review355() map[string][]finding.Raw {
	return map[string][]finding.Raw{
		"R355": {{Ref: "R355", Area: "contact", Stance: "risk", Claim: "A parent reports that a child predator may ask a child where they are",
			Quote: "if your kid plays Roblox high chance a child predator might ask where you are"}},
		// An invented quote, which must not reach the card.
		"R356": {{Ref: "R356", Area: "content", Stance: "risk", Claim: "Invented", Quote: "this review never said anything like these words at all"}},
	}
}

func spent(t *testing.T, m *spend.Meter, pool spend.Pool, ref string) cost.Price {
	t.Helper()
	p, err := m.Spent(context.Background(), pool, ref)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestReview355DecidesTheCard is the end-to-end check on the Roblox
// fixtures: review 355, the captures' equivalent of the brief's review 312,
// decides a "no", a second age costs one judge call, and the card's costs
// are what the meter booked.
func TestReview355DecidesTheCard(t *testing.T) {
	ctx := context.Background()
	e := newE2E(t, demos[0], review355())
	card, err := e.p.Run(ctx, Request{Title: "  Roblox ", Age: 9}, "check-1")
	if err != nil {
		t.Fatal(err)
	}
	if card.Verdict != checklist.No {
		t.Fatalf("verdict %s, want no", card.Verdict)
	}
	var contact checklist.Grade
	for _, g := range card.Grades {
		if g.Area == checklist.Contact {
			contact = g
		}
	}
	if contact.Level != checklist.Flag || len(contact.Findings) != 1 {
		t.Fatalf("contact = %+v", contact)
	}
	if len(card.Findings) != 1 || card.Findings[0].ID != contact.Findings[0] || card.Findings[0].Source.Index != 355 {
		t.Fatalf("card findings = %+v", card.Findings)
	}
	if card.Stats.Drops.Unquoted != 1 || card.Stats.Reviews != 500 || card.Stats.Chunks == 0 {
		t.Errorf("stats = %+v", card.Stats)
	}
	if !strings.HasPrefix(card.ID, "c_") || len(card.ID) != 24 || card.Title != "Roblox" || card.Age != 9 || card.Kind != KindIOS {
		t.Errorf("card head = %q %q %d %q", card.ID, card.Title, card.Age, card.Kind)
	}
	if card.App == nil || card.App.TrackID != 431946152 || card.ChecklistVersion != checklist.Version {
		t.Errorf("app %+v, checklist %s", card.App, card.ChecklistVersion)
	}
	if strings.Join(card.Stats.MissingKinds, ",") != KindPolicy || card.Stats.ReaderModel != "nano-recorded" || card.Stats.JudgeModel != "super-rules" {
		t.Errorf("missing %v, models %s %s", card.Stats.MissingKinds, card.Stats.ReaderModel, card.Stats.JudgeModel)
	}

	// The card's costs are the meter's.
	if got := cost.Price(card.Stats.ReadCostNano + card.Stats.JudgeCostNano); got != spent(t, e.meter, spend.PoolModel, "check-1") {
		t.Errorf("card model cost %d, meter %d", got, spent(t, e.meter, spend.PoolModel, "check-1"))
	}
	if card.Stats.JudgeCostNano != 90000 || card.Stats.ReadCostNano != int64(card.Stats.Chunks)*1200 {
		t.Errorf("read %d judge %d", card.Stats.ReadCostNano, card.Stats.JudgeCostNano)
	}
	if got := cost.Price(card.Stats.TavilyCredits) * spend.CreditUnit; got != spent(t, e.meter, spend.PoolTavily, "check-1") {
		t.Errorf("card credits %d, meter %d", got, spent(t, e.meter, spend.PoolTavily, "check-1"))
	}

	// A second age: no Tavily call, no reader call, one judge call.
	searches, extracts, reads := len(e.f.searches), len(e.f.extracts), e.reader.calls.Load()
	card2, err := e.p.Run(ctx, Request{Title: "roblox", Age: 15}, "check-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(e.f.searches) != searches || len(e.f.extracts) != extracts || e.reader.calls.Load() != reads {
		t.Errorf("second age made calls: searches %d→%d, extracts %d→%d, reads %d→%d",
			searches, len(e.f.searches), extracts, len(e.f.extracts), reads, e.reader.calls.Load())
	}
	if e.judge.calls != 2 {
		t.Errorf("judge calls %d, want 2", e.judge.calls)
	}
	if spent(t, e.meter, spend.PoolTavily, "check-2") != 0 || cost.Price(card2.Stats.JudgeCostNano) != spent(t, e.meter, spend.PoolModel, "check-2") {
		t.Errorf("second check booked tavily %d, model %d; card judge %d",
			spent(t, e.meter, spend.PoolTavily, "check-2"), spent(t, e.meter, spend.PoolModel, "check-2"), card2.Stats.JudgeCostNano)
	}
	if card2.Age != 15 || card2.ID == card.ID || !card2.CollectedAt.Equal(card.CollectedAt) || card2.Verdict != checklist.No {
		t.Errorf("second card %+v", card2)
	}
}

// Without review 355 the same evidence is unclear: it is that review that
// decides the card.
func TestWithoutReview355Unclear(t *testing.T) {
	e := newE2E(t, demos[0], map[string][]finding.Raw{})
	card, err := e.p.Run(context.Background(), Request{Title: "Roblox", Age: 9}, "c")
	if err != nil {
		t.Fatal(err)
	}
	if card.Verdict != checklist.Unclear {
		t.Errorf("verdict %s, want unclear", card.Verdict)
	}
}

func TestCollectFailureNotCached(t *testing.T) {
	e := newE2E(t, demos[0], review355())
	e.reader.fail = func(string) error { return errors.New("down") }
	if _, _, err := e.p.Collect(context.Background(), "Roblox", "c"); err == nil {
		t.Fatal("collect with every reader call failing did not fail")
	}
	if _, ok, _ := e.p.Cache.Get(context.Background(), EvidenceKey("Roblox", "us", "nano-recorded")); ok {
		t.Error("a failed collect was cached")
	}
	e.reader.fail = nil
	if _, hit, err := e.p.Collect(context.Background(), "Roblox", "c"); err != nil || hit {
		t.Errorf("retry: hit %v, err %v", hit, err)
	}
}

func TestEvidenceKey(t *testing.T) {
	base := EvidenceKey("Roblox", "us", "nano")
	if EvidenceKey("  roblox ", "US", "nano") != base {
		t.Error("title case, whitespace or storefront case changed the key")
	}
	for _, k := range []string{EvidenceKey("Roblox", "gb", "nano"), EvidenceKey("Roblox", "us", "other"), EvidenceKey("Minecraft", "us", "nano")} {
		if k == base {
			t.Errorf("%s collides with %s", k, base)
		}
	}
	if !strings.Contains(base, PlanVersion) {
		t.Error("plan version is not in the key")
	}
}

func TestRequestNormalize(t *testing.T) {
	r, err := Request{Title: "  Toca   Life World ", Age: 7}.Normalize()
	if err != nil || r.Title != "Toca Life World" {
		t.Errorf("%+v %v", r, err)
	}
	for _, bad := range []Request{{Title: "", Age: 7}, {Title: "x", Age: 2}, {Title: "x", Age: 18}, {Title: strings.Repeat("é", 121), Age: 7}} {
		if _, err := bad.Normalize(); !errors.Is(err, ErrRequest) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

func TestCitedKeepsRisks(t *testing.T) {
	fs := []finding.Finding{{ID: "a", Stance: finding.Risk}, {ID: "b", Stance: finding.Mitigation}, {ID: "c", Stance: finding.Neutral}, {ID: "d", Stance: finding.Mitigation}}
	got := cited(fs, []checklist.Grade{{Findings: []string{"d"}}})
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "d" {
		t.Errorf("cited = %+v", got)
	}
}

func TestCardIDs(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id, err := newCardID()
		if err != nil || len(id) != 24 || seen[id] || strings.Trim(id[2:], idAlphabet) != "" {
			t.Fatalf("id %q err %v", id, err)
		}
		seen[id] = true
	}
}

func (s feedStore) Storefront() string { return s.Client.Country }

// Evidence read with a failed chunk is used for the card but not cached:
// with 35 of 36 chunks failing, the findings are thin for a reason that
// will not last.
func TestThinEvidenceNotCached(t *testing.T) {
	e := newE2E(t, demos[0], review355())
	var calls atomic.Int32
	e.reader.fail = func(string) error {
		if calls.Add(1) == 1 {
			return nil
		}
		return errors.New("down")
	}
	ev, hit, err := e.p.Collect(context.Background(), "Roblox", "c")
	if err != nil || hit {
		t.Fatalf("hit %v, err %v", hit, err)
	}
	if ev.Stats.FailedChunks != ev.Stats.Chunks-1 || ev.Stats.Chunks < 2 {
		t.Fatalf("chunks %d failed %d, want all but one failed", ev.Stats.Chunks, ev.Stats.FailedChunks)
	}
	t.Logf("%d of %d chunks failed", ev.Stats.FailedChunks, ev.Stats.Chunks)
	if _, ok, _ := e.p.Cache.Get(context.Background(), EvidenceKey("Roblox", "us", "nano-recorded")); ok {
		t.Error("thin evidence was cached")
	}
	e.reader.fail = nil
	if _, hit, err := e.p.Collect(context.Background(), "Roblox", "c"); err != nil || hit {
		t.Errorf("after thin read: hit %v, err %v, want a fresh read", hit, err)
	}
	if _, hit, _ := e.p.Collect(context.Background(), "Roblox", "c"); !hit {
		t.Error("a full read was not cached")
	}
}

// The storefront in the key comes from the store, so it cannot drift from
// the country the App Store client reads.
func TestStorefrontFromStore(t *testing.T) {
	gb := appstore.New("http://x", "gb")
	for _, c := range []struct {
		store Store
		want  string
	}{{gb, "gb"}, {nil, "none"}, {feedStore{Client: gb}, "gb"}} {
		got, err := storefrontOf(c.store)
		if err != nil || got != c.want {
			t.Errorf("storefrontOf(%T) = %q, %v; want %q", c.store, got, err, c.want)
		}
	}
	if _, err := storefrontOf(stubStore{}); err == nil {
		t.Error("a store that cannot say its storefront was given a guess")
	}
}

func TestEvidenceKeyHasReaderVersion(t *testing.T) {
	v := ReaderVersion()
	if !strings.HasPrefix(v, "reader-") || !strings.Contains(EvidenceKey("Roblox", "us", "nano"), "|"+v+"|") {
		t.Errorf("reader version %q not in key", v)
	}
}

func (failStore) Storefront() string { return "us" }

// Evidence gathered while a source call errored is used for the card but
// not cached; the same sources simply absent are cached.
func TestTransientSourceFailureNotCached(t *testing.T) {
	khan := []appstore.App{{ID: 1, Name: "Khan Academy Kids", Seller: "Khan Academy", SellerURL: "http://khankids.org"}}
	pages := map[string]string{
		"https://www.khankids.org":                "Khan Academy Kids\nPrivacy: https://www.khankids.org/privacy-policy",
		"https://www.khankids.org/privacy-policy": "Privacy Policy\nWe collect as little as we can.",
	}
	for _, c := range []struct {
		name   string
		feed   error
		pages  map[string]string
		cached bool
	}{
		{"absent feed", nil, pages, true},
		{"feed error", errors.New("503"), pages, false},
		{"extract failed", nil, map[string]string{"https://www.khankids.org": pages["https://www.khankids.org"]}, false},
	} {
		m := openTestMeter(t)
		p := &Pipeline{
			Gatherer: &Gatherer{Store: failStore{stubStore: stubStore{khan}, feedErr: c.feed}, Search: &stubSearch{pages: c.pages}, Meter: m},
			Reader:   &Reader{Model: &recorder{price: 10}, Meter: m},
			Cache:    &MemCache{},
		}
		ev, hit, err := p.Collect(context.Background(), "Khan Academy Kids", "c")
		if err != nil && !errors.Is(err, ErrNoSources) {
			t.Fatalf("%s: %v", c.name, err)
		}
		if err == nil && (hit || ev.Title == "") {
			t.Fatalf("%s: hit %v", c.name, hit)
		}
		_, ok, _ := p.Cache.Get(context.Background(), EvidenceKey("Khan Academy Kids", "us", "nano-recorded"))
		if ok != c.cached {
			t.Errorf("%s: cached %v, want %v (err %v)", c.name, ok, c.cached, err)
		}
	}
}

// The key's versions are hashes of what the reader and gatherer do, so an
// edit to a template, a chunk size or the plan moves the key without a bump.
func TestKeyVersionsAreHashes(t *testing.T) {
	r, g := ReaderVersion(), GatherVersion()
	if r != ReaderVersion() || g != GatherVersion() {
		t.Error("versions are not stable")
	}
	if !strings.HasPrefix(g, PlanVersion+"-") || len(g) != len(PlanVersion)+13 || len(r) != len("reader-")+12 {
		t.Errorf("versions %q %q", r, g)
	}
	k := EvidenceKey("Roblox", "us", "nano")
	if !strings.Contains(k, "|"+g+"|"+r+"|") {
		t.Errorf("key %q lacks %q and %q", k, g, r)
	}
	// The sample reaches the user template and splits a long page.
	docs := versionSample()
	cs := chunkDocs(docs)
	if len(cs) < 2 || !strings.Contains(readerUser("Sample Title", cs[0], docs), "[R1]") {
		t.Errorf("sample does not exercise the chunker: %d chunks", len(cs))
	}
}
