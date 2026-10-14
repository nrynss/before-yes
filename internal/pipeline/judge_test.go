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
	"slices"
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
			checklist.Grade{Area: checklist.Contact, Level: checklist.Flag, Findings: []string{decisive.ID}, Reason: "A parent reports a child predator might ask where the child is."},
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
	decisive, _, _, fs := robloxFindings(t)
	first := gradesJSON(t,
		checklist.Grade{Area: checklist.Contact, Level: checklist.Flag, Findings: []string{decisive.ID}, Reason: "A parent reports a child predator might ask where the child is."},
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
	decisive, _, money, fs := robloxFindings(t)
	first := gradesJSON(t, checklist.Grade{Area: checklist.Contact, Level: checklist.Flag, Findings: []string{decisive.ID}, Reason: "A child predator might ask where the child is."})
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

// TestRecordedRepliesGradesRestOnVerifiedQuotes is T3.9b's pin, carried to
// T3.10. It replays the Token Factory replies recorded under T3.10 for the
// three demo titles through Collect and Decide, with the judge switches the
// recordings were made with and the entailment check wired to the recorded
// reader, and checks that no flag or ok_with_setting stands on the card
// without a valid citation: at least one cited id, every one a verified
// finding in the grade's area with a fitting stance, and for a setting, a
// cited mitigation quote that names it. It checks that every area in
// Card.ThinAreas is unknown, and pins each title's grade levels, thin areas
// and verdict. The entailment replays cover the definition rule: a standing
// grade also survives the area's flag definition and its own reason.
func TestRecordedRepliesGradesRestOnVerifiedQuotes(t *testing.T) {
	L := func(ls ...checklist.Level) []checklist.Level { return ls }
	const (
		ok, set, flag, unk = checklist.OK, checklist.OKWithSetting, checklist.Flag, checklist.Unknown
	)
	cases := []struct {
		d       demo
		age     int
		verdict checklist.Verdict
		levels  []checklist.Level // in card order, after every demotion
		thin    []checklist.Area
	}{
		// Roblox: the money, age_gating, content and developer_record flags
		// stand the entailment check (developer_record on the enforcement-
		// record sentence: the LA County suit is filed against the developer);
		// contact and data are dropped by it (flag_not_earned) and land thin.
		{demos[0], 9, checklist.No, L(unk, flag, unk, flag, flag, flag), []checklist.Area{"contact", "data"}},
		// Minecraft: four ok_with_settings, each naming a setting a cited
		// mitigation quote backs; data and developer_record are the judge's
		// own unknowns, so nothing is thin.
		{demos[1], 9, checklist.YesWithSettings, L(set, set, unk, set, set, unk), nil},
		// Khan: the judge graded four areas ok citing nothing (Enforce's
		// ok_without_finding), age_gating named a setting no cited quote
		// names, and the content flag's quotes do not meet the definition
		// (flag_not_earned). All six unknown make the verdict unclear.
		{demos[2], 5, checklist.Unclear, L(unk, unk, unk, unk, unk, unk), []checklist.Area{"age_gating", "content"}},
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
				Judge:    &Judge{Model: judge, Meter: m, Fast: tokenFactory(false), Think: tokenFactory(true), Entail: reader},
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
			for _, g := range card.Grades {
				if slices.Contains(card.ThinAreas, g.Area) {
					if g.Level != checklist.Unknown {
						t.Errorf("%s is thin but graded %s", g.Area, g.Level)
					}
					continue
				}
				if g.Level != checklist.Flag && g.Level != checklist.OKWithSetting {
					continue
				}
				if len(g.Findings) == 0 {
					t.Errorf("%s %s stands with no citation", g.Area, g.Level)
				}
				settingBacked := false
				for _, id := range g.Findings {
					f, ok := byID[id]
					if !ok || f.Area != g.Area || !stanceFits(g.Level, f.Stance) {
						t.Errorf("%s %s cites %s, not a verified finding of a fitting stance", g.Area, g.Level, id)
						continue
					}
					settingBacked = settingBacked || (f.Stance == finding.Mitigation && settingIn(normWords(f.Quote), normWords(g.Setting)))
				}
				if g.Level == checklist.OKWithSetting && !settingBacked {
					t.Errorf("%s: setting %q is in no cited mitigation quote", g.Area, g.Setting)
				}
			}
			for i, g := range card.Grades {
				if g.Level != c.levels[i] {
					t.Errorf("%s graded %s, pinned %s", g.Area, g.Level, c.levels[i])
				}
			}
			if card.Verdict != c.verdict {
				t.Errorf("verdict %s, want %s", card.Verdict, c.verdict)
			}
			if !reflect.DeepEqual(card.ThinAreas, c.thin) {
				t.Errorf("thin areas %v, want %v", card.ThinAreas, c.thin)
			}
		})
	}
}

// TestCitationsMustCheckOut pins checkCitations on hand-made findings. A
// flag or ok_with_setting stands only if every cited id is a verified
// finding in the grade's area with a fitting stance.
func TestCitationsMustCheckOut(t *testing.T) {
	fs := []finding.Finding{
		{ID: "r1", Area: "contact", Stance: finding.Risk, Quote: "A stranger sent my daughter private messages in chat."},
		{ID: "m1", Area: "contact", Stance: finding.Mitigation, Quote: "Chat can be switched off in Parental Controls."},
		{ID: "n1", Area: "contact", Stance: finding.Neutral, Quote: "The game has a chat feature for players."},
		{ID: "r2", Area: "money", Stance: finding.Risk, Quote: "Kids can buy gems without asking anyone."},
		{ID: "m2", Area: "money", Stance: finding.Mitigation, Quote: "Turn on Ask to Buy in Family Sharing."},
	}
	cases := []struct {
		name  string
		grade checklist.Grade
		stand bool
		why   string
	}{
		{"flag cites a risk", checklist.Grade{Area: "contact", Level: checklist.Flag, Findings: []string{"r1"}}, true, ""},
		{"setting cites a risk and a mitigation", checklist.Grade{Area: "contact", Level: checklist.OKWithSetting, Setting: "Parental Controls", Findings: []string{"r1", "m1"}}, true, ""},
		{"setting cites a mitigation alone", checklist.Grade{Area: "money", Level: checklist.OKWithSetting, Setting: "Ask to Buy", Findings: []string{"m2"}}, true, ""},
		{"unknown id", checklist.Grade{Area: "contact", Level: checklist.Flag, Findings: []string{"r1", "f_madeup"}}, false, "f_madeup is not a verified finding"},
		{"unverified finding", checklist.Grade{Area: "money", Level: checklist.Flag, Findings: []string{"f_dropped_by_verify"}}, false, "not a verified finding"},
		{"wrong area", checklist.Grade{Area: "contact", Level: checklist.Flag, Findings: []string{"r1", "r2"}}, false, "r2 is in area money"},
		{"flag cites a mitigation", checklist.Grade{Area: "contact", Level: checklist.Flag, Findings: []string{"r1", "m1"}}, false, "m1 is a mitigation finding"},
		{"flag cites a neutral", checklist.Grade{Area: "contact", Level: checklist.Flag, Findings: []string{"n1"}}, false, "n1 is a neutral finding"},
		{"setting cites a neutral", checklist.Grade{Area: "contact", Level: checklist.OKWithSetting, Setting: "Parental Controls", Findings: []string{"m1", "n1"}}, false, "n1 is a neutral finding"},
		{"flag cites nothing", checklist.Grade{Area: "contact", Level: checklist.Flag, Findings: []string{}}, false, "no finding cited"},
		{"setting cites nothing", checklist.Grade{Area: "money", Level: checklist.OKWithSetting, Setting: "Ask to Buy"}, false, "no finding cited"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := []checklist.Grade{c.grade}
			orig := fmt.Sprint(in)
			got, dem, thin := checkCitations(in, fs)
			if fmt.Sprint(in) != orig {
				t.Fatal("checkCitations modified its input")
			}
			if c.stand {
				if got[0].Level != c.grade.Level || !reflect.DeepEqual(got[0].Findings, c.grade.Findings) || len(dem) != 0 || len(thin) != 0 {
					t.Errorf("did not stand: %+v %+v %v", got[0], dem, thin)
				}
				return
			}
			if got[0].Level != checklist.Unknown || got[0].Setting != "" || len(got[0].Findings) != 0 || got[0].Reason != ThinReason {
				t.Errorf("not dropped to unknown: %+v", got[0])
			}
			if !reflect.DeepEqual(thin, []checklist.Area{c.grade.Area}) || len(dem) != 1 || dem[0].Rule != RuleBadCitation || !strings.Contains(dem[0].Detail, c.why) {
				t.Errorf("thin %v, demotions %+v; want rule %s with %q", thin, dem, RuleBadCitation, c.why)
			}
		})
	}
	// ok and unknown pass through untouched, bad ids and all (Enforce deals with them).
	pass := []checklist.Grade{{Area: "contact", Level: checklist.OK, Findings: []string{"n1", "f_x"}}, {Area: "money", Level: checklist.Unknown}}
	if got, dem, thin := checkCitations(pass, fs); !reflect.DeepEqual(got, pass) || len(dem) != 0 || len(thin) != 0 {
		t.Errorf("ok and unknown changed: %+v %+v %v", got, dem, thin)
	}
}

// TestGroundSettingNeedsANamingMitigation pins ground: an ok_with_setting
// stands only when a cited mitigation quote names the setting. A risk quote
// does not, and neither does a mitigation that never mentions it. A reason
// with no quoted words does not matter (T3.9b dropped that guard).
func TestGroundSettingNeedsANamingMitigation(t *testing.T) {
	fs := []finding.Finding{
		{ID: "r4", Area: "money", Stance: finding.Risk, Quote: "The app collects personal data about every account."},
		{ID: "m4", Area: "money", Stance: finding.Mitigation, Quote: "Purchases need a parent's approval with Ask to Buy turned on."},
		{ID: "m5", Area: "content", Stance: finding.Mitigation, Quote: "everything is curated by teachers"},
		{ID: "r6", Area: "age_gating", Stance: finding.Risk, Quote: "Kids can buy gems without asking anyone, so Ask to Buy matters."},
		{ID: "m6", Area: "age_gating", Stance: finding.Mitigation, Quote: "Turn on Ask to Buy in Family Sharing to approve every purchase."},
		{ID: "m7", Area: "data", Stance: finding.Mitigation, Quote: "Parents may request deletion by emailing the support team."},
	}
	in := []checklist.Grade{
		{Area: "money", Level: checklist.OKWithSetting, Setting: "Turn on Delete My Account", Reason: "Paraphrase only.", Findings: []string{"r4", "m4"}},
		{Area: "age_gating", Level: checklist.OKWithSetting, Setting: "Ask to Buy", Reason: "Paraphrase only.", Findings: []string{"r6", "m6"}},
		{Area: "content", Level: checklist.OK, Reason: "Fine.", Findings: []string{"m5"}},
		{Area: "data", Level: checklist.OKWithSetting, Setting: "Ask to Buy", Reason: "Paraphrase only.", Findings: []string{"m7"}},
		{Area: "contact", Level: checklist.Flag, Reason: "Paraphrase only.", Findings: []string{"r4"}},
	}
	orig := fmt.Sprint(in)
	got, dem, thin := ground(in, fs)
	if fmt.Sprint(in) != orig {
		t.Fatal("ground modified its input")
	}
	for _, i := range []int{0, 3} {
		if g := got[i]; g.Level != checklist.Unknown || g.Setting != "" || g.Reason != SettingThinReason {
			t.Errorf("%s (unbacked setting): %+v, want unknown", g.Area, g)
		}
	}
	if g := got[1]; g.Level != checklist.OKWithSetting || g.Setting != "Ask to Buy" || !reflect.DeepEqual(g.Findings, []string{"r6", "m6"}) {
		t.Errorf("age_gating: %+v, want the setting to stand", g)
	}
	if !reflect.DeepEqual(got[2], in[2]) || !reflect.DeepEqual(got[4], in[4]) {
		t.Errorf("ok or flag changed: %+v %+v", got[2], got[4])
	}
	if want := []checklist.Area{"money", "data"}; !reflect.DeepEqual(thin, want) {
		t.Errorf("thin %v, want %v", thin, want)
	}
	want := []checklist.Demotion{{Area: "money", Rule: RuleSettingUnbacked, Detail: "ok_with_setting"}, {Area: "data", Rule: RuleSettingUnbacked, Detail: "ok_with_setting"}}
	if !reflect.DeepEqual(dem, want) {
		t.Errorf("demotions %+v, want %+v", dem, want)
	}
}

// The whole judge step: a flag citing an unverified id drops to unknown and
// its area is thin; a setting no cited mitigation names drops too; a valid
// citation stands whatever words the reason uses.
func TestJudgeCitationEndToEnd(t *testing.T) {
	decisive, mit, money, fs := robloxFindings(t)
	reply := gradesJSON(t,
		checklist.Grade{Area: checklist.Contact, Level: checklist.Flag, Findings: []string{decisive.ID}, Reason: "Strangers can reach the child."},
		checklist.Grade{Area: checklist.Money, Level: checklist.Flag, Findings: []string{money.ID, "f_not_verified"}, Reason: "Purchases are a problem."},
		checklist.Grade{Area: checklist.Data, Level: checklist.OKWithSetting, Setting: "Unrelated Switch", Findings: []string{mit.ID}, Reason: "x"},
	)
	rec := &superRecorder{replies: []any{reply, reply}}
	got, err := (&Judge{Model: rec, Meter: openTestMeter(t)}).Judge(context.Background(), "Roblox", 9, "c", fs, nil)
	if err != nil {
		t.Fatal(err)
	}
	by := map[checklist.Area]checklist.Grade{}
	for _, g := range got.Grades {
		by[g.Area] = g
	}
	if g := by[checklist.Contact]; g.Level != checklist.Flag || !reflect.DeepEqual(g.Findings, []string{decisive.ID}) {
		t.Errorf("contact %+v, want a flag on the cited finding", g)
	}
	if by[checklist.Money].Level != checklist.Unknown || by[checklist.Data].Level != checklist.Unknown {
		t.Errorf("money %+v, data %+v; want both unknown", by[checklist.Money], by[checklist.Data])
	}
	if want := []checklist.Area{checklist.Money, checklist.Data}; !reflect.DeepEqual(got.Thin, want) {
		t.Errorf("thin %v, want %v", got.Thin, want)
	}
	if got.Verdict != checklist.No {
		t.Errorf("verdict %s, want no on the contact flag", got.Verdict)
	}
}

// The thin areas reach the card as a field, not only as reason text.
func TestThinAreasReachTheCard(t *testing.T) {
	decisive, mit, money, fs := robloxFindings(t)
	reply := gradesJSON(t,
		checklist.Grade{Area: checklist.Contact, Level: checklist.Flag, Findings: []string{decisive.ID}, Reason: "A parent reports a child predator might ask where the child is."},
		// Cites a contact mitigation for a money flag: wrong area, wrong stance.
		checklist.Grade{Area: checklist.Money, Level: checklist.Flag, Findings: []string{money.ID, mit.ID}, Reason: "Purchases are charged twice."},
	)
	rec := &superRecorder{replies: []any{reply, reply}}
	p := &Pipeline{Judge: &Judge{Model: rec, Meter: openTestMeter(t)}}
	card, err := p.Decide(context.Background(), Evidence{Title: "Roblox", Findings: fs}, 9, "c")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(card.ThinAreas, []checklist.Area{checklist.Money}) {
		t.Errorf("thin areas %v, want [money]", card.ThinAreas)
	}
	b, _ := json.Marshal(card)
	if !strings.Contains(string(b), `"thin_areas":["money"]`) {
		t.Errorf("card JSON lacks thin_areas: %s", b)
	}
	if card.Verdict != checklist.No {
		t.Errorf("verdict %s: the backed contact flag must still make no", card.Verdict)
	}
}

// Round 2: a setting is backed only when the quote names every content word
// of it (or holds it whole). A shared phrase like "made in the app" does not
// back a spending PIN no quote mentions; "Ask to Buy" still matches.
func TestSettingNeedsEveryContentWord(t *testing.T) {
	q := func(s string) []string { return normWords(s) }
	cases := []struct {
		quote, setting string
		want           bool
	}{
		{"All purchases made in the app are listed on the receipt page.", "Turn on the spending PIN made in the app", false},
		{"Turn on Ask to Buy in Family Sharing to approve every purchase.", "Ask to Buy", true},
		{"Parents can switch chat off under Parental Controls.", "Parental Controls > Chat > Off", true},
		{"Parents can switch chat off.", "Turn on", false},
	}
	for _, c := range cases {
		if got := settingIn(q(c.quote), q(c.setting)); got != c.want {
			t.Errorf("settingIn(%q, %q) = %v, want %v", c.quote, c.setting, got, c.want)
		}
	}
	fs := []finding.Finding{
		{ID: "r1", Area: "money", Stance: finding.Risk, Quote: "A PIN exists: purchases made in the app are blocked."},
		{ID: "m1", Area: "money", Stance: finding.Mitigation, Quote: "All purchases made in the app are listed on the receipt page."},
	}
	got, dem, thin := ground([]checklist.Grade{{Area: "money", Level: checklist.OKWithSetting,
		Setting: "Turn on the spending PIN made in the app", Reason: "A PIN exists: purchases made in the app are blocked.", Findings: []string{"r1", "m1"}}}, fs)
	if got[0].Level != checklist.Unknown || !reflect.DeepEqual(thin, []checklist.Area{"money"}) || dem[0].Rule != RuleSettingUnbacked {
		t.Errorf("invented PIN setting stood: %+v %v %v", got[0], thin, dem)
	}
}

// Round 2 L2: the matcher applies NFC as finding.Verify does, so a reason
// with a decomposed accent still carries a composed quote.
func TestMatcherAppliesNFC(t *testing.T) {
	quote := normWords("Strangers message the caf\u00e9 chat.")
	reason := normWords("A parent says strangers message the cafe\u0301 chat.")
	if !carries(reason, quote, 4, 2) {
		t.Errorf("NFC: %q does not carry %q", reason, quote)
	}
}

// T3.10, fault 1: the judge's prompt gives each area its own flag definition,
// the proportion rule, and the weighting rule bounded to this title and this
// age. The entailment prompt carries the same definitions.
func TestJudgeSystemAreaDefinitions(t *testing.T) {
	s := judgeSystem()
	for _, w := range []string{
		"What each area's \"flag\" means",
		"Proportion. A flag is a serious concern",
		"another product of the same developer",
		"not about a sibling product of the same developer and not another company's story",
		"A price on its own, or a plan a school pays for, is not purchase pressure on this child",
		"A parent's email asked for when the account is set up is not a data flag",
		"or the developer's own marketing about its values, is not a record",
	} {
		if !strings.Contains(s, w) {
			t.Errorf("judge prompt misses %q", w)
		}
	}
	for _, a := range checklist.Areas() {
		if !strings.Contains(s, areaDefs[a.ID]) {
			t.Errorf("judge prompt misses %s's definition", a.ID)
		}
		if areaDefs[a.ID] == "" {
			t.Errorf("no definition written for %s", a.ID)
		}
	}
	e := entailUser("X", 9, checklist.Grade{Area: checklist.Contact, Level: checklist.Flag, Findings: []string{"f_1"}}, nil)
	if !strings.Contains(e, areaDefs[checklist.Contact]) || !strings.Contains(e, "Child's age: 9") {
		t.Errorf("entail prompt misses the definition or the age:\n%s", e)
	}
}

// T3.10, fault 3, and the definition rule of fault 1: a standing grade whose
// cited findings do not meet its area's flag definition falls to unknown
// (meets_definition=false), and a standing grade whose reason goes beyond the
// cited quotes falls too (reason_supported=false). A grade that passes both
// stands, its setting and citations intact.
func TestEntailmentFallsToUnknown(t *testing.T) {
	fs := []finding.Finding{
		{ID: "f_contact", Area: checklist.Contact, Stance: finding.Risk, Quote: "a child predator might ask where you are",
			Claim: "A parent reports a predator asked where the child is", Source: finding.Source{Kind: KindReview, Index: 355}},
		{ID: "f_money", Area: checklist.Money, Stance: finding.Risk, Quote: "I got Robux and I got only 246 Robux instead of 1800",
			Claim: "A child reports a purchase that did not arrive in full", Source: finding.Source{Kind: KindReview, Index: 1}},
		{ID: "f_mit", Area: checklist.Money, Stance: finding.Mitigation, Quote: "Parents can turn on Ask to Buy so every purchase needs approval",
			Claim: "Purchases can be gated", Source: finding.Source{Kind: KindStore}},
		{ID: "f_content", Area: checklist.Content, Stance: finding.Risk, Quote: "It's also a very inappropriate game with all the horror games",
			Claim: "A reviewer reports inappropriate horror games", Source: finding.Source{Kind: KindReview, Index: 491}},
	}
	reply := gradesJSON(t,
		checklist.Grade{Area: checklist.Contact, Level: checklist.Flag, Findings: []string{"f_contact"}, Reason: "Strangers can reach the child."},
		checklist.Grade{Area: checklist.Money, Level: checklist.OKWithSetting, Setting: "Ask to Buy", Findings: []string{"f_money", "f_mit"}, Reason: "Ask to Buy gates purchases."},
		checklist.Grade{Area: checklist.Content, Level: checklist.Flag, Findings: []string{"f_content"}, Reason: "Reviewers report inappropriate games."},
	)
	entail := &superRecorder{replies: []any{
		`{"meets_definition": false, "reason_supported": true}`, // contact: not earned
		`{"meets_definition": true, "reason_supported": false}`, // money: reason beyond the quotes
		`{"meets_definition": true, "reason_supported": true}`,  // content: stands
	}, price: 7}
	m := openTestMeter(t)
	rec := &superRecorder{replies: []any{reply}, price: 40}
	j := &Judge{Model: rec, Meter: m, Entail: entail}
	got, err := j.Judge(context.Background(), "Roblox", 9, "check-entail", fs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entail.users) != 3 {
		t.Fatalf("%d entail calls, want one per standing grade (3)", len(entail.users))
	}
	by := map[checklist.Area]checklist.Grade{}
	for _, g := range got.Grades {
		by[g.Area] = g
	}
	if g := by[checklist.Contact]; g.Level != checklist.Unknown || g.Reason != NotEarnedReason || len(g.Findings) != 0 {
		t.Errorf("contact = %+v, want unknown with the not-earned reason", g)
	}
	if g := by[checklist.Money]; g.Level != checklist.Unknown || g.Setting != "" || g.Reason != UnsupportedReason || len(g.Findings) != 0 {
		t.Errorf("money = %+v, want unknown, setting cleared, unsupported reason", g)
	}
	if g := by[checklist.Content]; g.Level != checklist.Flag || len(g.Findings) != 1 || g.Findings[0] != "f_content" {
		t.Errorf("content = %+v, want the flag to stand on its citation", g)
	}
	if want := []checklist.Area{checklist.Contact, checklist.Money}; !reflect.DeepEqual(got.Thin, want) {
		t.Errorf("thin %v, want %v", got.Thin, want)
	}
	rules := map[checklist.Area]string{}
	for _, d := range got.Demotions {
		rules[d.Area] = d.Rule
	}
	if rules[checklist.Contact] != RuleNotEarned || rules[checklist.Money] != RuleReasonUnsupported {
		t.Errorf("demotion rules %v", rules)
	}
	if got.EntailErr != nil {
		t.Errorf("EntailErr = %v, want nil", got.EntailErr)
	}
	if got.Verdict != checklist.No {
		t.Errorf("verdict %s, want no on the standing content flag", got.Verdict)
	}
	// The entailment calls are metered with the judge's, under the check ref.
	spent, _ := m.Spent(context.Background(), spend.PoolModel, "check-entail")
	if spent != got.Cost || got.Cost != 40+3*7 {
		t.Errorf("cost %d, meter %d, want %d", got.Cost, spent, 40+3*7)
	}
	// The prompt shows the grade, the definition and the cited findings.
	if !strings.Contains(entail.users[0], "Flag definition: "+areaDefs[checklist.Contact]) ||
		!strings.Contains(entail.users[0], `quote: "a child predator might ask where you are"`) ||
		!strings.Contains(entail.users[0], "source=review 355") || !strings.Contains(entail.users[0], "Reason: Strangers can reach the child.") {
		t.Errorf("entail prompt:\n%s", entail.users[0])
	}
	if !strings.Contains(entail.users[1], "Setting: Ask to Buy") {
		t.Errorf("entail prompt misses the setting:\n%s", entail.users[1])
	}
}

// T3.10's failure policy: a failed or unparseable entailment call keeps the
// grade and records the reason in EntailErr, so an entailment failure can
// neither sink earned flags nor turn a budget refusal into unclear cards. A
// stand-in reader that answers {"findings": []} is such a failure.
func TestEntailmentFailureKeepsGrade(t *testing.T) {
	fs := []finding.Finding{
		{ID: "f_contact", Area: checklist.Contact, Stance: finding.Risk, Quote: "a child predator might ask where you are",
			Claim: "A parent reports a predator asked where the child is", Source: finding.Source{Kind: KindReview, Index: 355}},
	}
	reply := gradesJSON(t, checklist.Grade{Area: checklist.Contact, Level: checklist.Flag, Findings: []string{"f_contact"}, Reason: "Strangers can reach the child."})
	for _, bad := range []any{
		errors.New("rate limited"),
		`{"findings": []}`,           // the stand-in reader's answer
		`I cannot say.`,              // no JSON
		`{"meets_definition": true}`, // one answer missing
	} {
		entail := &superRecorder{replies: []any{bad}, price: 1}
		got, err := (&Judge{Model: &superRecorder{replies: []any{reply}, price: 10}, Meter: openTestMeter(t), Entail: entail}).
			Judge(context.Background(), "Roblox", 9, "c", fs, nil)
		if err != nil {
			t.Fatalf("%v: %v", bad, err)
		}
		if got.EntailErr == nil {
			t.Errorf("%v: the failed entailment call is not reported", bad)
		}
		if g := got.Grades[0]; g.Area != checklist.Contact || g.Level != checklist.Flag || len(g.Findings) != 1 {
			t.Errorf("%v: the grade did not stand: %+v", bad, g)
		}
		if len(got.Thin) != 0 || got.Verdict != checklist.No {
			t.Errorf("%v: thin %v, verdict %s; the grade must stand unchanged", bad, got.Thin, got.Verdict)
		}
	}
}

// A judge with no Entail wired skips the step: one call, grades unchanged.
// Tests that do not wire it keep compiling and passing.
func TestEntailNilSkips(t *testing.T) {
	decisive, _, money, _ := robloxFindings(t)
	fs := []finding.Finding{decisive, money} // no mitigation, so no conflict pass
	reply := gradesJSON(t,
		checklist.Grade{Area: checklist.Contact, Level: checklist.Flag, Findings: []string{decisive.ID}, Reason: "A parent reports a predator."},
		checklist.Grade{Area: checklist.Money, Level: checklist.Flag, Findings: []string{money.ID}},
	)
	rec := &superRecorder{replies: []any{reply}}
	got, err := (&Judge{Model: rec, Meter: openTestMeter(t)}).Judge(context.Background(), "Roblox", 9, "c", fs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.users) != 1 {
		t.Errorf("%d judge calls, want 1: nothing else may be called without an entailment model", len(rec.users))
	}
	if got.Grades[0].Level != checklist.Flag || got.EntailErr != nil {
		t.Errorf("grades changed without an entailment model: %+v, %v", got.Grades[0], got.EntailErr)
	}
}
