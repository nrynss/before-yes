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
		got, err := Decide(set(c.over))
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
		{"duplicate discarded", []Grade{{Area: Money, Level: OK}, {Area: Money, Level: Flag, Findings: []string{"m-risk"}}},
			Money, OK, []string{}, []string{"money:duplicate_grade"}},
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
