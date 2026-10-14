package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

// superRecorder stands in for Super with recorded replies, one per call in
// order. A reply that is an error makes that call fail.
type superRecorder struct {
	replies []any // string or error
	price   cost.Price

	mu    sync.Mutex
	users []string
	opts  []model.Options
}

func (s *superRecorder) Chat(_ context.Context, msgs []model.Message, o model.Options) (model.Reply, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.users)
	s.users = append(s.users, msgs[len(msgs)-1].Content)
	s.opts = append(s.opts, o)
	if n >= len(s.replies) {
		return model.Reply{}, errors.New("no recorded reply")
	}
	switch r := s.replies[n].(type) {
	case error:
		return model.Reply{}, r
	default:
		return model.Reply{Text: r.(string), Price: s.price, Measured: true}, nil
	}
}
func (s *superRecorder) Estimate([]model.Message, model.Options) cost.Price { return 3 * s.price }
func (s *superRecorder) Settings() model.Settings {
	return model.Settings{Provider: "recorded", Model: "super-recorded"}
}

func gradesJSON(t *testing.T, gs ...checklist.Grade) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"grades": gs})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// robloxFindings verifies a handful of Roblox findings against the fixture
// documents, so the ids are the real ones a card would carry, and adds a
// mitigation for contact so that area is in conflict.
func robloxFindings(t *testing.T) (decisive, mitigation, money finding.Finding, all []finding.Finding) {
	t.Helper()
	g, _, _ := gatherFixture(t, demos[0])
	segs := map[string]finding.Segment{}
	for i, d := range g.Docs {
		segs[refFor(d, i)] = finding.Segment{Text: d.Text, Source: d.Source}
	}
	terms := refOf(t, g.Docs, KindTerms, "")
	fs, drops := finding.Verify([]finding.Raw{
		{Ref: "R355", Area: "contact", Stance: "risk", Claim: "A parent reports that a child predator may ask a child where they are",
			Quote: "if your kid plays Roblox high chance a child predator might ask where you are"},
		{Ref: "R1", Area: "money", Stance: "risk", Claim: "A child reports a Robux purchase that did not arrive in full",
			Quote: "I got Robux and I got only 246 Robux instead of 1800"},
	}, segs)
	if len(fs) != 2 || drops != (finding.Drops{}) {
		t.Fatalf("verify: %d findings, drops %+v", len(fs), drops)
	}
	// The terms text is long; take a verbatim sentence from it as a stand-in
	// mitigation the conflict pass must weigh.
	text := segs[terms].Text
	i := strings.Index(text, "We are updating our Terms of Use")
	if i < 0 {
		t.Fatal("terms text moved")
	}
	mit, _ := finding.Verify([]finding.Raw{{Ref: terms, Area: "contact", Stance: "mitigation",
		Claim: "Roblox says it is updating its terms", Quote: text[i : i+60]}}, segs)
	if len(mit) != 1 {
		t.Fatal("mitigation did not verify")
	}
	return fs[0], mit[0], fs[1], append(fs, mit...)
}

func TestJudgeConflictPassFlagsContact(t *testing.T) {
	decisive, mit, money, fs := robloxFindings(t)
	first := gradesJSON(t,
		checklist.Grade{Area: checklist.Contact, Level: checklist.OK, Findings: []string{mit.ID}, Reason: "Settings exist."},
		checklist.Grade{Area: checklist.Money, Level: checklist.Flag, Findings: []string{"f_madeup"}, Reason: "Invented."},
	)
	second := "<think>Contact: {weighing} the parent's first-hand report against the terms.</think>\n```json\n" +
		gradesJSON(t,
			checklist.Grade{Area: checklist.Contact, Level: checklist.Flag, Findings: []string{decisive.ID, mit.ID}, Reason: "A parent reports a child predator might ask where the child is."},
			// Not asked: the second pass answers only the conflicted areas.
			checklist.Grade{Area: checklist.Money, Level: checklist.OK, Findings: []string{money.ID}},
		) + "\n```"
	rec := &superRecorder{replies: []any{first, second}, price: 40000}
	m := openTestMeter(t)
	j := &Judge{Model: rec, Meter: m,
		Fast:  map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": false}},
		Think: map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": true}}}
	got, err := j.Judge(context.Background(), "Roblox", 9, "check-judge", fs, []string{KindPolicy})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Conflicted, []checklist.Area{checklist.Contact}) || got.ConflictErr != nil {
		t.Fatalf("conflicted %v, err %v", got.Conflicted, got.ConflictErr)
	}
	if len(rec.users) != 2 {
		t.Fatalf("%d judge calls, want 2", len(rec.users))
	}
	if !strings.Contains(rec.users[1], "Grade only these areas: contact\n") || strings.Contains(rec.users[1], "area=money") {
		t.Errorf("conflict pass prompt:\n%s", rec.users[1])
	}
	if !reflect.DeepEqual(rec.opts[0].Extra, j.Fast) || !rec.opts[0].JSON || !reflect.DeepEqual(rec.opts[1].Extra, j.Think) || rec.opts[1].MaxTokens != 16000 {
		t.Errorf("options %+v", rec.opts)
	}
	for _, u := range rec.users {
		if !strings.Contains(u, "Child's age: 9\n") || !strings.Contains(u, "Unread sources: policy\n") {
			t.Errorf("prompt misses the age or the unread sources:\n%.200s", u)
		}
	}
	if !strings.Contains(rec.users[0], "source=review 355") || !strings.Contains(rec.users[0], "id="+decisive.ID) {
		t.Errorf("first prompt does not show review 355 by number and id")
	}

	byArea := map[checklist.Area]checklist.Grade{}
	for _, g := range got.Grades {
		byArea[g.Area] = g
	}
	c := byArea[checklist.Contact]
	if c.Level != checklist.Flag || len(c.Findings) == 0 || c.Findings[0] != decisive.ID {
		t.Errorf("contact = %+v, want flag on review 355's finding %s", c, decisive.ID)
	}
	mo := byArea[checklist.Money]
	if mo.Level != checklist.Unknown || len(mo.Findings) != 0 {
		t.Errorf("money = %+v, want the made-up id dropped and the flag demoted", mo)
	}
	demoted := false
	for _, d := range got.Demotions {
		if d.Area == checklist.Money {
			demoted = true
		}
	}
	if !demoted {
		t.Errorf("no demotion recorded for money: %+v", got.Demotions)
	}
	if len(got.Grades) != len(checklist.Areas()) {
		t.Errorf("%d grades, want one per area", len(got.Grades))
	}
	if got.Verdict != checklist.No {
		t.Errorf("verdict %s, want no", got.Verdict)
	}
	spent, _ := m.Spent(context.Background(), spend.PoolModel, "check-judge")
	if got.Cost != 80000 || spent != got.Cost {
		t.Errorf("cost %d, meter %d", got.Cost, spent)
	}
}

func TestJudgeConflictPassFailureKeepsFirst(t *testing.T) {
	decisive, mit, _, fs := robloxFindings(t)
	first := gradesJSON(t,
		checklist.Grade{Area: checklist.Contact, Level: checklist.Flag, Findings: []string{decisive.ID, mit.ID}, Reason: "A parent reports a child predator might ask where the child is."},
	)
	for _, second := range []any{errors.New("overloaded"), "I cannot decide."} {
		rec := &superRecorder{replies: []any{first, second}, price: 10}
		j := &Judge{Model: rec, Meter: openTestMeter(t)}
		got, err := j.Judge(context.Background(), "Roblox", 9, "c", fs, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.ConflictErr == nil {
			t.Errorf("%v: the failed conflict pass is not reported", second)
		}
		if got.Grades[0].Area != checklist.Contact || got.Grades[0].Level != checklist.Flag {
			t.Errorf("%v: first grade did not stand: %+v", second, got.Grades[0])
		}
		if got.Verdict != checklist.No {
			t.Errorf("verdict %s", got.Verdict)
		}
		if strings.Contains(rec.users[0], "Unread sources") {
			t.Error("prompt lists unread sources when none are missing")
		}
	}
}

func TestJudgeNoConflictOneCall(t *testing.T) {
	decisive, _, money, _ := robloxFindings(t)
	rec := &superRecorder{replies: []any{gradesJSON(t,
		checklist.Grade{Area: checklist.Contact, Level: checklist.Flag, Findings: []string{decisive.ID}},
		checklist.Grade{Area: checklist.Money, Level: checklist.Flag, Findings: []string{money.ID}},
	)}}
	got, err := (&Judge{Model: rec, Meter: openTestMeter(t)}).Judge(context.Background(), "Roblox", 12, "c", []finding.Finding{decisive, money}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.users) != 1 || len(got.Conflicted) != 0 {
		t.Errorf("%d calls, conflicted %v", len(rec.users), got.Conflicted)
	}
}

func TestJudgeFirstPassFailure(t *testing.T) {
	for _, r := range []any{errors.New("down"), "no json here"} {
		rec := &superRecorder{replies: []any{r}}
		if _, err := (&Judge{Model: rec, Meter: openTestMeter(t)}).Judge(context.Background(), "X", 9, "c", nil, nil); err == nil {
			t.Errorf("%v: Judge did not fail", r)
		}
	}
}

func TestReplaceAddsMissingArea(t *testing.T) {
	first := []checklist.Grade{{Area: checklist.Money, Level: checklist.OK}}
	second := []checklist.Grade{{Area: checklist.Contact, Level: checklist.Flag}, {Area: checklist.Contact, Level: checklist.OK}, {Area: checklist.Money, Level: checklist.Flag}}
	got := replace(first, second, []checklist.Area{checklist.Contact})
	want := []checklist.Grade{{Area: checklist.Money, Level: checklist.OK}, {Area: checklist.Contact, Level: checklist.Flag}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("replace = %+v", got)
	}
}

func TestJudgeSystemWeighting(t *testing.T) {
	s := judgeSystem()
	for _, w := range []string{"First-hand reports of what actually happened outweigh what a policy or a store listing claims", "Do not invent ids", "silence is not evidence"} {
		if !strings.Contains(s, w) {
			t.Errorf("judge prompt misses %q", w)
		}
	}
}

// A thinking reply that drafts a grade and then revises it is read for its
// final answer.
func TestParseGradesTakesTheLastBlock(t *testing.T) {
	reply := `Draft: {"grades":[{"area":"contact","level":"ok","findings":[]}]} no wait, review 355 is first-hand. ` +
		"Final:\n```json\n" + `{"grades":[{"area":"contact","level":"flag","findings":["f_1"]},{"area":"money","level":"unknown","findings":[]}]}` + "\n```\nDone {note}."
	gs, err := parseGrades(reply)
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) != 2 || gs[0].Level != checklist.Flag || gs[1].Area != checklist.Money {
		t.Errorf("grades = %+v", gs)
	}
}

// A conflict pass that parses but grades none of the conflicted areas
// counts as failed: the first grades stand and the record says why.
func TestConflictPassAnsweringNothingFails(t *testing.T) {
	decisive, mit, money, fs := robloxFindings(t)
	first := gradesJSON(t, checklist.Grade{Area: checklist.Contact, Level: checklist.Flag, Findings: []string{decisive.ID, mit.ID}, Reason: "A child predator might ask where the child is."})
	second := gradesJSON(t, checklist.Grade{Area: checklist.Money, Level: checklist.Flag, Findings: []string{money.ID}})
	rec := &superRecorder{replies: []any{first, second}}
	got, err := (&Judge{Model: rec, Meter: openTestMeter(t)}).Judge(context.Background(), "Roblox", 9, "c", fs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.ConflictErr == nil || !strings.Contains(got.ConflictErr.Error(), "none of the conflicted areas") {
		t.Errorf("ConflictErr = %v", got.ConflictErr)
	}
	for _, g := range got.Grades {
		if g.Area == checklist.Money && g.Level == checklist.Flag {
			t.Error("an area the conflict pass was not asked about changed")
		}
		if g.Area == checklist.Contact && g.Level != checklist.Flag {
			t.Errorf("first contact grade did not stand: %+v", g)
		}
	}
}

// A final block cut off after a draft is an error, not the draft.
func TestParseGradesTruncatedFinalFails(t *testing.T) {
	for _, reply := range []string{
		`Draft {"grades":[{"area":"contact","level":"ok","findings":[]}]} Final: {"grades":[{"area":"contact","level":"flag","findings":["f_1"]},{"area":"mon`,
		`Draft {"grades":[{"area":"contact","level":"ok","findings":[]}]} Final: {"grades": [oops]}`,
	} {
		if gs, err := parseGrades(reply); err == nil || !strings.Contains(err.Error(), "truncated") {
			t.Errorf("parseGrades(%.50q) = %+v, %v; want a truncation error", reply, gs, err)
		}
	}
	if _, err := parseGrades(`{"grades":[{"area":"contact","level":"flag","findings":[]}]} done`); err != nil {
		t.Errorf("a whole final block failed: %v", err)
	}
}

// recordedReplies answers from T3.6b's recorded Token Factory replies under
// testdata/eval/replies/<slug>/<role>/. The file name is the key tools/eval
// computes (ReplyKey in tools/eval/replay.go): the first 24 hex digits of a
// SHA-256 over the role, the messages and the options. A call with no
// recording fails, and the test counts it as a miss.
type recordedReplies struct {
	dir, role string
	misses    atomic.Int64
}

func (r *recordedReplies) load(msgs []model.Message, o model.Options) (model.Reply, error) {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\x00", r.role)
	for _, m := range msgs {
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00", m.Role, m.Content)
	}
	b, _ := json.Marshal(o)
	h.Write(b)
	raw, err := os.ReadFile(filepath.Join(r.dir, r.role, hex.EncodeToString(h.Sum(nil))[:24]+".json"))
	if err != nil {
		return model.Reply{}, errors.New("no recorded reply")
	}
	var rec struct {
		Text         string     `json:"text"`
		InputTokens  int        `json:"input_tokens"`
		OutputTokens int        `json:"output_tokens"`
		PriceNano    cost.Price `json:"price_nano"`
		Measured     bool       `json:"measured"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return model.Reply{}, err
	}
	return model.Reply{Text: rec.Text, InputTokens: rec.InputTokens, OutputTokens: rec.OutputTokens, Price: rec.PriceNano, Measured: rec.Measured}, nil
}

func (r *recordedReplies) Chat(_ context.Context, msgs []model.Message, o model.Options) (model.Reply, error) {
	rep, err := r.load(msgs, o)
	if err != nil {
		r.misses.Add(1)
	}
	return rep, err
}
func (r *recordedReplies) Estimate(msgs []model.Message, o model.Options) cost.Price {
	rep, _ := r.load(msgs, o)
	return rep.Price
}
func (r *recordedReplies) Settings() model.Settings {
	return model.Settings{Provider: "replay", Model: "recorded-" + r.role}
}

// TestRecordedRepliesGradesRestOnVerifiedQuotes is T3.9's pin. It replays
// T3.6b's recorded Token Factory replies for the three demo titles through
// Collect and Decide, with the judge switches the recordings were made with,
// and checks that every flag and ok_with_setting on the card cites at least
// one verified finding in its area, of a fitting stance, whose quote shares
// content words with the reason; that every grade the rule dropped reads
// ThinReason; and that the verdicts are the ones T3.9 measured.
func TestRecordedRepliesGradesRestOnVerifiedQuotes(t *testing.T) {
	cases := []struct {
		d       demo
		age     int
		verdict checklist.Verdict
		thin    []checklist.Area
	}{
		{demos[0], 9, checklist.No, nil},
		{demos[1], 9, checklist.Unclear, []checklist.Area{"contact", "money", "data", "age_gating", "content", "developer_record"}},
		{demos[2], 5, checklist.No, []checklist.Area{"contact"}},
	}
	tokenFactory := func(on bool) map[string]any {
		return map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": on}}
	}
	for _, c := range cases {
		t.Run(c.d.slug, func(t *testing.T) {
			dir := filepath.Join("..", "..", "testdata", "eval", "replies", c.d.slug)
			reader := &recordedReplies{dir: dir, role: "reader"}
			judge := &recordedReplies{dir: dir, role: "judge"}
			_, srv := newFixture(t, c.d)
			store, tc := fixtureClients(t, srv)
			m := openTestMeter(t)
			now := func() time.Time { return time.Date(2026, 10, 13, 19, 0, 0, 0, time.UTC) }
			p := &Pipeline{
				Gatherer: &Gatherer{Store: store, Search: tc, Meter: m, Now: now},
				Reader:   &Reader{Model: reader, Meter: m},
				Judge:    &Judge{Model: judge, Meter: m, Fast: tokenFactory(false), Think: tokenFactory(true)},
				Now:      now,
			}
			ctx := context.Background()
			ev, _, err := p.Collect(ctx, c.d.title, "eval-"+c.d.slug)
			if err != nil {
				t.Fatal(err)
			}
			card, err := p.Decide(ctx, ev, c.age, "eval-"+c.d.slug)
			if err != nil {
				t.Fatal(err)
			}
			if n := reader.misses.Load() + judge.misses.Load(); n > 0 {
				t.Fatalf("%d calls had no recording: the replay no longer matches T3.6b's requests", n)
			}
			byID := map[string]finding.Finding{}
			for _, f := range ev.Findings {
				byID[f.ID] = f
			}
			skip := contentWords(c.d.title, nil)
			var thin []checklist.Area
			for _, g := range card.Grades {
				if g.Reason == ThinReason {
					thin = append(thin, g.Area)
					if g.Level != checklist.Unknown {
						t.Errorf("%s: thin reason on a %s grade", g.Area, g.Level)
					}
				}
				if g.Level != checklist.Flag && g.Level != checklist.OKWithSetting {
					continue
				}
				if len(g.Findings) == 0 {
					t.Errorf("%s %s stands with no citation", g.Area, g.Level)
				}
				said := contentWords(g.Reason+" "+g.Setting, skip)
				for _, id := range g.Findings {
					f, ok := byID[id]
					switch {
					case !ok:
						t.Errorf("%s cites %s, which is not a verified finding", g.Area, id)
					case f.Area != g.Area || !stanceFits(g.Level, f.Stance):
						t.Errorf("%s %s cites %s (%s, %s)", g.Area, g.Level, id, f.Area, f.Stance)
					case shared(said, contentWords(f.Quote, skip)) < minShared:
						t.Errorf("%s: reason %q does not name %s's quote %q", g.Area, g.Reason, id, f.Quote)
					}
				}
			}
			if card.Verdict != c.verdict {
				t.Errorf("verdict %s, want %s", card.Verdict, c.verdict)
			}
			if !reflect.DeepEqual(thin, c.thin) {
				t.Errorf("thin areas %v, want %v", thin, c.thin)
			}
		})
	}
}

// TestGroundDropsReasonsNoQuoteBacks pins ground on hand-made findings: a
// flag whose reason speaks to a cited risk quote stands and keeps only that
// citation; a flag whose reason no cited quote backs falls to unknown with
// ThinReason; an ok_with_setting backed only by a neutral finding falls too;
// ok is left alone; and the input is not modified.
func TestGroundDropsReasonsNoQuoteBacks(t *testing.T) {
	fs := []finding.Finding{
		{ID: "r1", Area: "contact", Stance: finding.Risk, Quote: "a stranger sent my daughter private messages in chat"},
		{ID: "r2", Area: "contact", Stance: finding.Risk, Quote: "the water splash sound is the same"},
		{ID: "r3", Area: "data", Stance: finding.Risk, Quote: "we love the colourful characters"},
		{ID: "n1", Area: "money", Stance: finding.Neutral, Quote: "purchases need a password under Ask to Buy"},
		{ID: "m1", Area: "content", Stance: finding.Mitigation, Quote: "everything is curated by teachers"},
	}
	in := []checklist.Grade{
		{Area: "contact", Level: checklist.Flag, Reason: "Strangers send private messages to children in chat.", Findings: []string{"r2", "r1"}},
		{Area: "data", Level: checklist.Flag, Reason: "Data cannot be deleted.", Findings: []string{"r3"}},
		{Area: "money", Level: checklist.OKWithSetting, Setting: "Ask to Buy", Reason: "Purchases need a password.", Findings: []string{"n1"}},
		{Area: "content", Level: checklist.OK, Reason: "Fine.", Findings: []string{"m1"}},
	}
	orig := fmt.Sprint(in)
	got, dem := ground(in, fs, "Example Title")
	if fmt.Sprint(in) != orig {
		t.Fatal("ground modified its input")
	}
	if g := got[0]; g.Level != checklist.Flag || !reflect.DeepEqual(g.Findings, []string{"r1"}) {
		t.Errorf("contact: %+v, want a flag citing only r1", g)
	}
	for _, i := range []int{1, 2} {
		if g := got[i]; g.Level != checklist.Unknown || g.Reason != ThinReason || g.Setting != "" {
			t.Errorf("%s: %+v, want unknown with the thin reason", g.Area, g)
		}
	}
	if !reflect.DeepEqual(got[3], in[3]) {
		t.Errorf("ok grade changed: %+v", got[3])
	}
	want := []checklist.Demotion{{Area: "data", Rule: RuleThinEvidence, Detail: "flag"}, {Area: "money", Rule: RuleThinEvidence, Detail: "ok_with_setting"}}
	if !reflect.DeepEqual(dem, want) {
		t.Errorf("demotions %+v, want %+v", dem, want)
	}
}
