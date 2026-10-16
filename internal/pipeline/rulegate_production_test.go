package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nrynss/before-yes/internal/checklist"
)

// The saved production cards carry their findings (the ones their grades cite,
// plus every risk finding), so the rule gate can be applied to them offline,
// with no model call. This measures what the gate would have done to the two
// cards the live run produced; the pins record the answer.
//
// What the pins say. Roblox: every flag stands on a quote that satisfies its
// rule (the age-gating flag on the four-year-old the check called twenty-five,
// the data flag on the policy's ad personalisation, the developer-record flag
// on the Texas attorney general's suit), and the verdict stays no. Khan: money
// is confirmed on the listing's price line; the content flag stands on the
// review of scary faces that popped up, not on the yoga and gender-role
// objections cited beside it; the developer-record ok rested on a nonprofit
// description and awards, which are not a record, so it is vetoed to unknown.
// The verdict stays no. The saved Khan card holds only the findings its grades
// cite plus the risks, so the listing's privacy label is not in it and data
// cannot be raised here as it is in the full evidence.
func TestRuleGateOnProductionCards(t *testing.T) {
	type want struct {
		verdict checklist.Verdict
		levels  map[checklist.Area]checklist.Level
		rules   []string
	}
	cases := map[string]want{
		"roblox": {checklist.No, map[checklist.Area]checklist.Level{
			checklist.Contact: checklist.OKWithSetting, checklist.Money: checklist.Unknown, checklist.Data: checklist.Flag,
			checklist.AgeGating: checklist.Flag, checklist.Content: checklist.Flag, checklist.DeveloperRecord: checklist.Flag,
		}, []string{RuleDataConfirmed, RuleAgeGateConfirmed, RuleContentConfirmed, RuleDeveloperConfirmed}},
		"khan-academy-kids": {checklist.No, map[checklist.Area]checklist.Level{
			checklist.Contact: checklist.Unknown, checklist.Money: checklist.OK, checklist.Data: checklist.Unknown,
			checklist.AgeGating: checklist.Unknown, checklist.Content: checklist.Flag, checklist.DeveloperRecord: checklist.Unknown,
		}, []string{RuleMoneyFree, RuleContentConfirmed, RuleDeveloperNoStmt}},
	}
	for slug, w := range cases {
		b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "eval", "production", slug+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var card Card
		if err := json.Unmarshal(b, &card); err != nil {
			t.Fatal(err)
		}
		var names []string
		if card.App != nil {
			names = append(names, card.App.Name, card.App.Seller)
		}
		got, acts := ApplyRules(card.Title, card.Age, card.Grades, card.Findings, WithDeveloper(names...))
		verdict, err := checklist.Decide(got)
		if err != nil {
			t.Fatal(err)
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "%s age %d: %s -> %s\n", slug, card.Age, card.Verdict, verdict)
		for i, g := range got {
			fmt.Fprintf(&sb, "  %-17s %s(%d) -> %s(%d)\n", g.Area, card.Grades[i].Level, len(card.Grades[i].Findings), g.Level, len(g.Findings))
		}
		for _, a := range acts {
			fmt.Fprintf(&sb, "  gate %s %s %s -> %s %v\n", a.Area, a.Rule, a.From, a.To, a.Findings)
		}
		t.Log("\n" + sb.String())
		if verdict != w.verdict {
			t.Errorf("%s: verdict %s, pinned %s", slug, verdict, w.verdict)
		}
		for _, g := range got {
			if g.Level != w.levels[g.Area] {
				t.Errorf("%s %s: %s, pinned %s", slug, g.Area, g.Level, w.levels[g.Area])
			}
		}
		var rules []string
		for _, a := range acts {
			rules = append(rules, a.Rule)
		}
		if !slices.Equal(rules, w.rules) {
			t.Errorf("%s: rules %v, pinned %v", slug, rules, w.rules)
		}
	}
}
