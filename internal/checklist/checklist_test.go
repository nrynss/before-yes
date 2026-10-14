package checklist

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func set(over map[Area]Level) []Grade {
	var gs []Grade
	for _, s := range Areas() {
		l := OK
		if v, ok := over[s.ID]; ok {
			l = v
		}
		g := Grade{Area: s.ID, Level: l, Findings: []string{}}
		if l == OKWithSetting {
			g.Setting = "setting for " + string(s.ID)
		}
		gs = append(gs, g)
	}
	return gs
}

func TestAreasFixed(t *testing.T) {
	want := []Area{Contact, Money, Data, AgeGating, Content, DeveloperRecord}
	got := Areas()
	if len(got) != len(want) {
		t.Fatalf("got %d areas", len(got))
	}
	for i, a := range want {
		if got[i].ID != a || got[i].Label == "" || got[i].Question == "" {
			t.Errorf("area %d = %+v, want %s with label and question", i, got[i], a)
		}
	}
	got[0].ID = "mutated"
	if Areas()[0].ID != Contact {
		t.Error("Areas returned shared slice")
	}
}

func TestDecideRules(t *testing.T) {
	cases := []struct {
		name string
		over map[Area]Level
		want Verdict
	}{
		{"all ok is yes", nil, Yes},
		{"one setting is yes with settings", map[Area]Level{Contact: OKWithSetting}, YesWithSettings},
		{"flag is no", map[Area]Level{Money: Flag}, No},
		{"flag beats setting", map[Area]Level{Contact: OKWithSetting, DeveloperRecord: Flag}, No},
		{"flag beats unknowns", map[Area]Level{Contact: Unknown, Content: Unknown, Data: Flag}, No},
		{"contact unknown is unclear", map[Area]Level{Contact: Unknown}, Unclear},
		{"content unknown is unclear", map[Area]Level{Content: Unknown}, Unclear},
		{"contact unknown beats setting", map[Area]Level{Contact: Unknown, Money: OKWithSetting}, Unclear},
		{"three unknowns is unclear", map[Area]Level{Money: Unknown, Data: Unknown, AgeGating: Unknown}, Unclear},
		{"two unknowns elsewhere is still yes", map[Area]Level{Money: Unknown, DeveloperRecord: Unknown}, Yes},
		{"two unknowns and a setting", map[Area]Level{Data: Unknown, AgeGating: Unknown, Money: OKWithSetting}, YesWithSettings},
		{"all unknown is unclear", map[Area]Level{Contact: Unknown, Money: Unknown, Data: Unknown, AgeGating: Unknown, Content: Unknown, DeveloperRecord: Unknown}, Unclear},
	}
	for _, c := range cases {
		gs := set(c.over)
		got, err := Decide(gs)
		if err != nil || got != c.want {
			t.Errorf("%s: got %q, %v; want %q", c.name, got, err, c.want)
		}
	}
}

func TestDecideRefuses(t *testing.T) {
	full := set(nil)
	cases := map[string][]Grade{
		"empty":        nil,
		"incomplete":   full[:5],
		"duplicated":   append(append([]Grade{}, full[:5]...), full[0]),
		"seven":        append(append([]Grade{}, full...), full[0]),
		"bad area":     append(append([]Grade{}, full[:5]...), Grade{Area: "vibes", Level: OK}),
		"bad level":    append(append([]Grade{}, full[:5]...), Grade{Area: DeveloperRecord, Level: "fine"}),
		"setting name": append(append([]Grade{}, full[:5]...), Grade{Area: DeveloperRecord, Level: OKWithSetting}),
	}
	for name, gs := range cases {
		if v, err := Decide(gs); !errors.Is(err, ErrGrades) || v != "" {
			t.Errorf("%s: got %q, %v; want ErrGrades", name, v, err)
		}
	}
}

func TestDecideDeterministic(t *testing.T) {
	gs := set(map[Area]Level{Money: OKWithSetting})
	rev := append([]Grade(nil), gs...)
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	a, _ := Decide(gs)
	b, _ := Decide(rev)
	if a != b {
		t.Errorf("order changed verdict: %q vs %q", a, b)
	}
}

var ev = map[string]Cited{
	"c-risk": {Area: Contact, Risk: true},
	"c-mit":  {Area: Contact},
	"m-risk": {Area: Money, Risk: true},
	"m-mit":  {Area: Money},
}

func one(t *testing.T, out []Grade, a Area) Grade {
	t.Helper()
	for _, g := range out {
		if g.Area == a {
			return g
		}
	}
	t.Fatalf("area %s missing", a)
	return Grade{}
}

func rules(d []Demotion) []string {
	var r []string
	for _, x := range d {
		r = append(r, string(x.Area)+":"+x.Rule)
	}
	return r
}

func TestEnforce(t *testing.T) {
	cases := []struct {
		name      string
		in        []Grade
		area      Area
		wantLevel Level
		wantIDs   []string
		wantRules []string
	}{
		{"verified flag stands", []Grade{{Area: Contact, Level: Flag, Findings: []string{"c-risk"}}},
			Contact, Flag, []string{"c-risk"}, nil},
		{"missing id dropped, flag demoted", []Grade{{Area: Contact, Level: Flag, Findings: []string{"nope"}}},
			Contact, Unknown, []string{}, []string{"contact:missing_finding", "contact:flag_without_risk"}},
		{"other area id dropped, flag demoted", []Grade{{Area: Contact, Level: Flag, Findings: []string{"m-risk"}}},
			Contact, Unknown, []string{}, []string{"contact:wrong_area", "contact:flag_without_risk"}},
		{"flag on mitigation only demoted", []Grade{{Area: Contact, Level: Flag, Findings: []string{"c-mit"}}},
			Contact, Unknown, []string{"c-mit"}, []string{"contact:flag_without_risk"}},
		{"bad id dropped, good kept", []Grade{{Area: Contact, Level: Flag, Findings: []string{"nope", "c-risk"}}},
			Contact, Flag, []string{"c-risk"}, []string{"contact:missing_finding"}},
		{"setting without name demoted", []Grade{{Area: Money, Level: OKWithSetting, Findings: []string{"m-mit"}}},
			Money, Unknown, []string{"m-mit"}, []string{"money:setting_without_name"}},
		{"setting without finding demoted", []Grade{{Area: Money, Level: OKWithSetting, Setting: "Ask to Buy", Findings: []string{"c-mit"}}},
			Money, Unknown, []string{}, []string{"money:wrong_area", "money:setting_without_finding"}},
		{"setting with finding stands", []Grade{{Area: Money, Level: OKWithSetting, Setting: "Ask to Buy", Findings: []string{"m-mit"}}},
			Money, OKWithSetting, []string{"m-mit"}, nil},
		{"skipped area added unknown", nil, DeveloperRecord, Unknown, []string{}, []string{"developer_record:skipped_area"}},
		{"duplicate discarded", []Grade{{Area: Money, Level: OK, Findings: []string{"m-mit"}}, {Area: Money, Level: Flag, Findings: []string{"m-risk"}}},
			Money, OK, []string{"m-mit"}, []string{"money:duplicate_grade"}},
		{"contact ok without finding demoted", []Grade{{Area: Contact, Level: OK, Findings: []string{"nope"}}},
			Contact, Unknown, []string{}, []string{"contact:missing_finding", "contact:ok_without_finding"}},
		{"content ok without finding demoted", []Grade{{Area: Content, Level: OK}},
			Content, Unknown, []string{}, []string{"content:ok_without_finding"}},
		{"contact ok with mitigation stands", []Grade{{Area: Contact, Level: OK, Findings: []string{"c-mit"}}},
			Contact, OK, []string{"c-mit"}, nil},
		{"contact ok on risk only raised to flag", []Grade{{Area: Contact, Level: OK, Findings: []string{"c-risk"}}},
			Contact, Flag, []string{"c-risk"}, []string{"contact:ok_on_risk_only"}},
		{"contact ok on risk and mitigation stands", []Grade{{Area: Contact, Level: OK, Findings: []string{"c-risk", "c-mit"}}},
			Contact, OK, []string{"c-risk", "c-mit"}, nil},
		{"zero-width setting demoted", []Grade{{Area: Money, Level: OKWithSetting, Setting: "\u200b \u2060", Findings: []string{"m-mit"}}},
			Money, Unknown, []string{"m-mit"}, []string{"money:setting_without_name"}},
		{"other area ok without finding demoted", []Grade{{Area: Money, Level: OK}},
			Money, Unknown, []string{}, []string{"money:ok_without_finding"}},
		{"money ok on pay-to-win risk raised to flag", []Grade{{Area: Money, Level: OK, Findings: []string{"m-risk"}}},
			Money, Flag, []string{"m-risk"}, []string{"money:ok_on_risk_only"}},
		{"ok on missing id is unknown", []Grade{{Area: Money, Level: OK, Findings: []string{"nope"}}},
			Money, Unknown, []string{}, []string{"money:missing_finding", "money:ok_without_finding"}},
		{"money ok with mitigation stands", []Grade{{Area: Money, Level: OK, Findings: []string{"m-mit"}}},
			Money, OK, []string{"m-mit"}, nil},
		{"blank setting demoted", []Grade{{Area: Money, Level: OKWithSetting, Setting: "   ", Findings: []string{"m-mit"}}},
			Money, Unknown, []string{"m-mit"}, []string{"money:setting_without_name"}},
		{"repeated id kept once", []Grade{{Area: Contact, Level: Flag, Findings: []string{"c-risk", "c-risk"}}},
			Contact, Flag, []string{"c-risk"}, nil},
		{"unknown area discarded", []Grade{{Area: "vibes", Level: Flag, Findings: []string{"c-risk"}}},
			"vibes", "", nil, []string{"vibes:invalid_grade"}},
		{"bad level discarded", []Grade{{Area: Money, Level: "great"}},
			Money, Unknown, []string{}, []string{"money:invalid_grade", "money:skipped_area"}},
	}
	for _, c := range cases {
		out, dem := Enforce(c.in, ev)
		if len(out) != 6 {
			t.Fatalf("%s: %d grades", c.name, len(out))
		}
		for i, s := range Areas() {
			if out[i].Area != s.ID {
				t.Errorf("%s: not card order", c.name)
			}
		}
		if _, err := Decide(out); err != nil {
			t.Errorf("%s: Decide refused Enforce output: %v", c.name, err)
		}
		if !c.area.Valid() {
			var got []string
			for _, r := range rules(dem) {
				if strings.HasPrefix(r, string(c.area)+":") {
					got = append(got, r)
				}
			}
			if !reflect.DeepEqual(got, c.wantRules) {
				t.Errorf("%s: demotions %v; want %v", c.name, got, c.wantRules)
			}
			continue
		}
		g := one(t, out, c.area)
		if g.Level != c.wantLevel || !reflect.DeepEqual(g.Findings, c.wantIDs) {
			t.Errorf("%s: got %s %v; want %s %v", c.name, g.Level, g.Findings, c.wantLevel, c.wantIDs)
		}
		var got []string
		for _, r := range rules(dem) {
			if strings.HasPrefix(r, string(c.area)+":") {
				got = append(got, r)
			}
		}
		if !reflect.DeepEqual(got, c.wantRules) {
			t.Errorf("%s: demotions %v; want %v", c.name, got, c.wantRules)
		}
		if g.Level == Unknown && g.Setting != "" {
			t.Errorf("%s: unknown grade kept a setting", c.name)
		}
	}
}

// The hole T1.1 round 1 M1 found: a judge grading every area ok with no
// evidence must not reach yes.
func TestEvidenceFreeOKIsNotYes(t *testing.T) {
	var in []Grade
	for _, s := range Areas() {
		in = append(in, Grade{Area: s.ID, Level: OK, Findings: []string{"nope"}})
	}
	out, _ := Enforce(in, ev)
	if v, err := Decide(out); err != nil || v != Unclear {
		t.Errorf("got %q %v, want unclear", v, err)
	}
}

// T1.1 round 2 M1: ok resting only on risks is not a yes (now: no).
func TestRiskOnlyOKIsNotYes(t *testing.T) {
	in := []Grade{{Area: Contact, Level: OK, Findings: []string{"c-risk"}}}
	for _, s := range Areas()[1:] {
		in = append(in, Grade{Area: s.ID, Level: OK})
	}
	out, _ := Enforce(in, ev)
	if v, _ := Decide(out); v != No {
		t.Errorf("got %q, want no", v)
	}
}

// User decision (round 4): in every area, ok citing only risks becomes flag,
// so the verdict is no; ok citing nothing becomes unknown.
func TestOKNeedsSupportEveryArea(t *testing.T) {
	evAll := map[string]Cited{}
	for _, s := range Areas() {
		evAll["risk-"+string(s.ID)] = Cited{Area: s.ID, Risk: true}
		evAll["ok-"+string(s.ID)] = Cited{Area: s.ID}
	}
	build := func(target Area, targetIDs []string) []Grade {
		var in []Grade
		for _, s := range Areas() {
			ids := []string{"ok-" + string(s.ID)}
			if s.ID == target {
				ids = targetIDs
			}
			in = append(in, Grade{Area: s.ID, Level: OK, Findings: ids})
		}
		return in
	}
	for _, target := range Areas() {
		out, dem := Enforce(build(target.ID, []string{"risk-" + string(target.ID)}), evAll)
		if g := one(t, out, target.ID); g.Level != Flag || len(dem) != 1 || dem[0].Rule != RuleOKOnRisk {
			t.Errorf("%s ok on risk only: %s %v", target.ID, g.Level, dem)
		}
		if v, _ := Decide(out); v != No {
			t.Errorf("%s ok on risk only: verdict %s, want no", target.ID, v)
		}
		out, _ = Enforce(build(target.ID, nil), evAll)
		if g := one(t, out, target.ID); g.Level != Unknown {
			t.Errorf("%s ok on nothing: %s, want unknown", target.ID, g.Level)
		}
		out, dem = Enforce(build(target.ID, []string{"risk-" + string(target.ID), "ok-" + string(target.ID)}), evAll)
		if g := one(t, out, target.ID); g.Level != OK || len(dem) != 0 {
			t.Errorf("%s ok on risk and support: %s %v", target.ID, g.Level, dem)
		}
	}
	// Money ok citing a pay-to-win risk, rest supported: no.
	out, _ := Enforce(build(Money, []string{"risk-money"}), evAll)
	if v, _ := Decide(out); v != No {
		t.Errorf("money ok on pay-to-win: %s, want no", v)
	}
	// An evidence-free unknown outside contact and content still allows yes.
	out, _ = Enforce(build(Money, nil), evAll)
	if v, _ := Decide(out); v != Yes {
		t.Errorf("one evidence-free unknown in money: %s, want yes", v)
	}
}

// T1.1 round 4 M1: a changed grade carries the system's reason, not the judge's.
func TestChangedGradeReason(t *testing.T) {
	out, _ := Enforce([]Grade{
		{Area: Money, Level: OK, Setting: "Ask to Buy", Reason: "looks fine", Findings: []string{"m-risk"}},
		{Area: Contact, Level: Flag, Reason: "strangers everywhere", Findings: []string{"c-mit"}},
		{Area: Content, Level: OK, Reason: "kept", Findings: []string{}},
	}, ev)
	m := one(t, out, Money)
	if m.Level != Flag || m.Setting != "" || m.Reason != Reasons[RuleOKOnRisk] {
		t.Errorf("money: %+v", m)
	}
	if c := one(t, out, Contact); c.Level != Unknown || c.Reason != Reasons[RuleFlagNoRisk] {
		t.Errorf("contact: %+v", c)
	}
	if c := one(t, out, Content); c.Reason != Reasons[RuleOKNoProof] {
		t.Errorf("content: %+v", c)
	}
	if d := one(t, out, DeveloperRecord); d.Reason != Reasons[RuleSkipped] {
		t.Errorf("skipped: %+v", d)
	}
	for _, r := range Reasons {
		if r == "" {
			t.Error("empty reason")
		}
	}
	// An unchanged grade keeps the judge's reason.
	out, _ = Enforce([]Grade{{Area: Money, Level: OK, Reason: "free, no ads", Findings: []string{"m-mit"}}}, ev)
	if g := one(t, out, Money); g.Reason != "free, no ads" {
		t.Errorf("unchanged reason replaced: %q", g.Reason)
	}
}

// T1.1 round 5: only a final ok_with_setting keeps a setting.
func TestSettingClearedUnlessSettingGrade(t *testing.T) {
	out, _ := Enforce([]Grade{
		{Area: Contact, Level: Unknown, Setting: "Chat off"},
		{Area: Money, Level: OK, Setting: "Ask to Buy", Findings: []string{"m-mit"}},
		{Area: Data, Level: Flag, Setting: "Limit tracking", Findings: []string{"d-risk"}},
		{Area: Content, Level: OKWithSetting, Setting: "Restricted mode", Findings: []string{"t-mit"}},
	}, map[string]Cited{"m-mit": {Area: Money}, "d-risk": {Area: Data, Risk: true}, "t-mit": {Area: Content}})
	for _, a := range []Area{Contact, Money, Data} {
		if g := one(t, out, a); g.Setting != "" {
			t.Errorf("%s kept setting %q at level %s", a, g.Setting, g.Level)
		}
	}
	if g := one(t, out, Content); g.Setting != "Restricted mode" {
		t.Errorf("content lost its setting: %+v", g)
	}
	if g := one(t, out, Money); g.Level != OK {
		t.Errorf("money level changed: %s", g.Level)
	}
}

// T1.1 round 4 L1: ok_with_setting citing only risks stands; the setting
// answers the risk.
func TestSettingOnRiskStands(t *testing.T) {
	in := []Grade{{Area: Money, Level: OKWithSetting, Setting: "Ask to Buy", Findings: []string{"m-risk"}}}
	for _, s := range Areas() {
		if s.ID != Money {
			in = append(in, Grade{Area: s.ID, Level: OK, Findings: []string{"ok"}})
		}
	}
	evx := map[string]Cited{"m-risk": {Area: Money, Risk: true}}
	// Give each other area its own supporting finding.
	for i := range in {
		if in[i].Area != Money {
			id := "ok-" + string(in[i].Area)
			in[i].Findings = []string{id}
			evx[id] = Cited{Area: in[i].Area}
		}
	}
	out, dem := Enforce(in, evx)
	if g := one(t, out, Money); g.Level != OKWithSetting || g.Setting != "Ask to Buy" || len(dem) != 0 {
		t.Errorf("got %+v %v", g, dem)
	}
	if v, _ := Decide(out); v != YesWithSettings {
		t.Errorf("verdict %s", v)
	}
}

// T1.1 round 3 L1: invisible characters at a setting's edges are trimmed and
// the setting dedupes with its clean form.
func TestSettingInvisibleEdges(t *testing.T) {
	out, _ := Enforce([]Grade{
		{Area: Contact, Level: OKWithSetting, Setting: "\u200bRestricted mode\u200b", Findings: []string{"c-mit"}},
		{Area: Money, Level: OKWithSetting, Setting: "\ufeffRestricted mode ", Findings: []string{"m-mit"}},
	}, ev)
	if g := one(t, out, Contact); g.Setting != "Restricted mode" {
		t.Errorf("setting %q", g.Setting)
	}
	if got := Settings(out); len(got) != 1 || got[0] != "Restricted mode" {
		t.Errorf("settings %q", got)
	}
}

func TestSettingTrimmed(t *testing.T) {
	out, _ := Enforce([]Grade{{Area: Money, Level: OKWithSetting, Setting: "  Ask to Buy ", Findings: []string{"m-mit"}}}, ev)
	if g := one(t, out, Money); g.Setting != "Ask to Buy" {
		t.Errorf("setting %q", g.Setting)
	}
	gs := set(map[Area]Level{Money: OKWithSetting})
	for _, b := range []string{" ", "\u200b", "\ufeff\u00a0"} {
		gs[1].Setting = b
		if _, err := Decide(gs); !errors.Is(err, ErrGrades) {
			t.Errorf("Decide accepted blank setting %q", b)
		}
	}
}

func TestEnforceDoesNotMutate(t *testing.T) {
	in := []Grade{{Area: Contact, Level: Flag, Findings: []string{"nope"}}}
	Enforce(in, ev)
	if in[0].Level != Flag || len(in[0].Findings) != 1 {
		t.Error("input mutated")
	}
}

func TestSettings(t *testing.T) {
	gs := set(map[Area]Level{Money: OKWithSetting, Contact: OKWithSetting})
	gs[1].Setting = gs[0].Setting // money repeats contact's setting
	got := Settings(gs)
	if !reflect.DeepEqual(got, []string{"setting for contact"}) {
		t.Errorf("got %v", got)
	}
}
