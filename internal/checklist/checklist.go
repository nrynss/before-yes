// Package checklist holds the fixed parent checklist, the grade levels and the
// rules that turn grades into a verdict.
//
// The judge grades each area. Code decides: Enforce holds the grades to the
// verified evidence, and Decide computes the verdict from them. Neither asks a
// model anything.
package checklist

import (
	"errors"
	"fmt"
)

// Version names the checklist and rules a card was judged under. Bump it when
// an area's wording or a rule in Decide or Enforce changes, because cached
// cards record the version they were judged under.
const Version = "v1"

// Area is one checklist area id. The ids are stable wire values.
type Area string

// The six areas, in card order.
const (
	Contact         Area = "contact"
	Money           Area = "money"
	Data            Area = "data"
	AgeGating       Area = "age_gating"
	Content         Area = "content"
	DeveloperRecord Area = "developer_record"
)

// AreaSpec is an area as it appears on the card.
type AreaSpec struct {
	ID       Area   `json:"id"`
	Label    string `json:"label"`
	Question string `json:"question"`
}

var areas = []AreaSpec{
	{Contact, "Contact", "Can strangers message or voice chat with your child, is it on by default, and can it be turned off?"},
	{Money, "Money", "Does it push in-app purchases, loot boxes, pay-to-win or subscription traps?"},
	{Data, "Data", "What does it collect, does it track for ads or share with third parties, and can the data be deleted?"},
	{AgeGating, "Age gating", "Does the age gate work in practice, going by what parents report?"},
	{Content, "Content", "What do reviewers actually report seeing?"},
	{DeveloperRecord, "Developer record", "Has the developer had fines, breaches, lawsuits or regulatory actions?"},
}

// Areas returns the six areas in card order. The slice is a copy.
func Areas() []AreaSpec { return append([]AreaSpec(nil), areas...) }

// Valid reports whether a is one of the six areas.
func (a Area) Valid() bool {
	for _, s := range areas {
		if s.ID == a {
			return true
		}
	}
	return false
}

// Level is an area's grade.
type Level string

// Grade levels.
const (
	// OK: nothing in the evidence calls for action.
	OK Level = "ok"
	// OKWithSetting: fine once the named setting is on.
	OKWithSetting Level = "ok_with_setting"
	// Flag: a verified risk finding stands against the title.
	Flag Level = "flag"
	// Unknown: the evidence does not settle the area.
	Unknown Level = "unknown"
)

// Valid reports whether l is one of the four levels.
func (l Level) Valid() bool {
	switch l {
	case OK, OKWithSetting, Flag, Unknown:
		return true
	}
	return false
}

// Grade is the judge's grade for one area. Findings holds the ids of the
// verified findings the grade rests on. Setting names the setting to turn on,
// and is required for OKWithSetting.
type Grade struct {
	Area     Area     `json:"area"`
	Level    Level    `json:"level"`
	Setting  string   `json:"setting,omitempty"`
	Findings []string `json:"findings"`
	Reason   string   `json:"reason,omitempty"`
}

// Verdict is the computed answer on a card.
type Verdict string

// Verdicts.
const (
	Yes             Verdict = "yes"
	YesWithSettings Verdict = "yes_with_settings"
	No              Verdict = "no"
	// Unclear: too little evidence to answer. It exists so that missing
	// evidence never reads as "yes".
	Unclear Verdict = "unclear"
)

// UnclearUnknowns is how many unknown areas make the verdict unclear: half of
// the six.
const UnclearUnknowns = 3

// ErrGrades is returned when a grade set is not exactly one valid grade per area.
var ErrGrades = errors.New("checklist: invalid grade set")

// Decide computes the verdict from a complete grade set. It is pure and
// deterministic. The rules, applied in order:
//
//  1. The set must hold exactly one grade per area, each with a valid level.
//     Anything else is refused with ErrGrades.
//  2. Any area graded flag: no.
//  3. Contact or content unknown: unclear. Those two areas are what a parent
//     most needs answered, and silence on them is not a yes.
//  4. UnclearUnknowns or more areas unknown: unclear.
//  5. Any area graded ok_with_setting: yes_with_settings.
//  6. Otherwise: yes.
func Decide(grades []Grade) (Verdict, error) {
	if err := validate(grades); err != nil {
		return "", err
	}
	by := map[Area]Level{}
	unknown, settings := 0, false
	flagged := false
	for _, g := range grades {
		by[g.Area] = g.Level
		switch g.Level {
		case Flag:
			flagged = true
		case Unknown:
			unknown++
		case OKWithSetting:
			settings = true
		}
	}
	switch {
	case flagged:
		return No, nil
	case by[Contact] == Unknown || by[Content] == Unknown:
		return Unclear, nil
	case unknown >= UnclearUnknowns:
		return Unclear, nil
	case settings:
		return YesWithSettings, nil
	}
	return Yes, nil
}

func validate(grades []Grade) error {
	seen := map[Area]bool{}
	for _, g := range grades {
		if !g.Area.Valid() {
			return fmt.Errorf("%w: unknown area %q", ErrGrades, g.Area)
		}
		if seen[g.Area] {
			return fmt.Errorf("%w: area %q graded twice", ErrGrades, g.Area)
		}
		seen[g.Area] = true
		if !g.Level.Valid() {
			return fmt.Errorf("%w: area %q has unknown level %q", ErrGrades, g.Area, g.Level)
		}
		if g.Level == OKWithSetting && g.Setting == "" {
			return fmt.Errorf("%w: area %q is ok_with_setting with no setting", ErrGrades, g.Area)
		}
	}
	if len(seen) != len(areas) {
		return fmt.Errorf("%w: %d of %d areas graded", ErrGrades, len(seen), len(areas))
	}
	return nil
}

// Cited is what Enforce needs to know about one verified finding.
type Cited struct {
	Area Area
	// Risk is true when the finding's stance is risk.
	Risk bool
}

// Demotion records one change Enforce made, for the card's small print and tests.
type Demotion struct {
	Area   Area   `json:"area"`
	Rule   string `json:"rule"`
	Detail string `json:"detail,omitempty"`
}

// Demotion rules.
const (
	RuleMissingFinding = "missing_finding" // a cited id is not a verified finding
	RuleWrongArea      = "wrong_area"      // a cited finding belongs to another area
	RuleFlagNoRisk     = "flag_without_risk"
	RuleSettingNoName  = "setting_without_name"
	RuleSettingNoProof = "setting_without_finding"
	RuleSkipped        = "skipped_area"
	RuleDuplicate      = "duplicate_grade"
	RuleBadGrade       = "invalid_grade" // unknown area or level from the judge
)

// Enforce holds the judge's grades to the verified evidence, keyed by finding
// id. It returns exactly one grade per area, in card order, and the demotions
// it made:
//
//   - a cited finding id that does not exist, or belongs to another area, is dropped;
//   - a flag left with no verified risk finding becomes unknown;
//   - an ok_with_setting with no setting named, or no remaining finding, becomes unknown;
//   - an area the judge skipped is added as unknown;
//   - a grade for an unknown area, or with an unknown level, is discarded
//     (its area then counts as skipped); a second grade for an area is discarded.
//
// The input slice is not modified.
func Enforce(grades []Grade, evidence map[string]Cited) ([]Grade, []Demotion) {
	var dem []Demotion
	got := map[Area]Grade{}
	for _, in := range grades {
		if !in.Area.Valid() || !in.Level.Valid() {
			dem = append(dem, Demotion{Area: in.Area, Rule: RuleBadGrade, Detail: string(in.Level)})
			continue
		}
		if _, dup := got[in.Area]; dup {
			dem = append(dem, Demotion{Area: in.Area, Rule: RuleDuplicate})
			continue
		}
		g := in
		g.Findings = []string{}
		risk := false
		for _, id := range in.Findings {
			c, ok := evidence[id]
			switch {
			case !ok:
				dem = append(dem, Demotion{Area: g.Area, Rule: RuleMissingFinding, Detail: id})
				continue
			case c.Area != g.Area:
				dem = append(dem, Demotion{Area: g.Area, Rule: RuleWrongArea, Detail: id})
				continue
			}
			g.Findings = append(g.Findings, id)
			risk = risk || c.Risk
		}
		switch g.Level {
		case Flag:
			if !risk {
				dem = append(dem, Demotion{Area: g.Area, Rule: RuleFlagNoRisk})
				g.Level = Unknown
			}
		case OKWithSetting:
			if g.Setting == "" {
				dem = append(dem, Demotion{Area: g.Area, Rule: RuleSettingNoName})
				g.Level = Unknown
			} else if len(g.Findings) == 0 {
				dem = append(dem, Demotion{Area: g.Area, Rule: RuleSettingNoProof})
				g.Level = Unknown
			}
		}
		if g.Level == Unknown {
			g.Setting = ""
		}
		got[g.Area] = g
	}
	out := make([]Grade, 0, len(areas))
	for _, s := range areas {
		g, ok := got[s.ID]
		if !ok {
			dem = append(dem, Demotion{Area: s.ID, Rule: RuleSkipped})
			g = Grade{Area: s.ID, Level: Unknown, Findings: []string{}}
		}
		out = append(out, g)
	}
	return out, dem
}

// Settings returns the settings named by ok_with_setting grades, in card order,
// without repeats.
func Settings(grades []Grade) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, s := range areas {
		for _, g := range grades {
			if g.Area == s.ID && g.Level == OKWithSetting && g.Setting != "" && !seen[g.Setting] {
				seen[g.Setting] = true
				out = append(out, g.Setting)
			}
		}
	}
	return out
}
