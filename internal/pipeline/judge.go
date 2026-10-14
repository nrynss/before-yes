package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/nrynss/keel/cost"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/finding"
	"github.com/nrynss/before-yes/internal/model"
)

func judgeSystem() string {
	var b strings.Builder
	b.WriteString(`You grade an app, game or channel for one child against a fixed parent checklist, using only the findings you are given. Every finding has an id, a stance, and a verbatim quote from a named source.

Areas:
`)
	for _, a := range checklist.Areas() {
		fmt.Fprintf(&b, "- %s: %s\n", a.ID, a.Question)
	}
	b.WriteString(`
For each area give one level:
- "ok": nothing in the findings is a concern for a child of this age.
- "ok_with_setting": a concern exists, and a specific setting or control answers it. Name the setting exactly as a parent would find it ("Parental Controls > Chat > Off", "Ask to Buy"). Cite the findings that show the concern and the setting.
- "flag": a concern for this age that no setting answers. Cite at least one "risk" finding.
- "unknown": the findings are too thin to say.

How to weigh evidence:
- First-hand reports of what actually happened outweigh what a policy or a store listing claims. One credible report that a filter or an age gate failed for a young child can decide an area.
- Grade against the child's age. What is fine at 13 may be a flag at 7.
- A source listed as unread was not read. Its silence is not evidence: do not grade an area "ok" because a policy that could not be read said nothing.
- Cite only finding ids from the list. Do not invent ids.

Reply with one JSON object:
{"grades": [{"area": "...", "level": "...", "reason": "one plain sentence for a parent", "setting": "...", "findings": ["id", ...]}, ...]}
with exactly one grade per area.`)
	return b.String()
}

func judgeUser(title string, age int, areas []checklist.Area, fs []finding.Finding, missing []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\nChild's age: %d\n", title, age)
	if len(missing) > 0 {
		fmt.Fprintf(&b, "Unread sources: %s\n", strings.Join(missing, ", "))
	}
	if len(areas) < len(checklist.Areas()) {
		names := make([]string, len(areas))
		for i, a := range areas {
			names[i] = string(a)
		}
		fmt.Fprintf(&b, "Grade only these areas: %s\n", strings.Join(names, ", "))
	}
	b.WriteString("\nFindings:\n")
	for _, f := range fs {
		if !slices.Contains(areas, f.Area) {
			continue
		}
		src := f.Source.Kind
		if f.Source.Kind == KindReview {
			src = fmt.Sprintf("review %d", f.Source.Index)
		}
		fmt.Fprintf(&b, "- id=%s area=%s stance=%s source=%s\n  claim: %s\n  quote: %q\n", f.ID, f.Area, f.Stance, src, f.Claim, f.Quote)
	}
	return b.String()
}

// parseGrades finds the answer in a reply: the last object with grades. A
// thinking reply can carry reasoning before the answer, braces and draft
// grades included, and a host may wrap the answer in a code fence, so the
// opening braces are tried from the end and the last one that decodes to an
// object with grades wins, unless a later grades block failed to decode,
// which is an error rather than a silent fall back to the draft. A grade object inside it has no grades key of
// its own, so it is skipped on the way back.
func parseGrades(reply string) ([]checklist.Grade, error) {
	for i := len(reply) - 1; i >= 0; i-- {
		if reply[i] != '{' {
			continue
		}
		var out struct {
			Grades []checklist.Grade `json:"grades"`
		}
		dec := json.NewDecoder(strings.NewReader(reply[i:]))
		if err := dec.Decode(&out); err != nil || len(out.Grades) == 0 {
			continue
		}
		// A grades key after this block is a later answer that did not
		// decode: cut off at the token cap, or malformed. Using the draft
		// before it would grade on an answer the model revised.
		if end := i + int(dec.InputOffset()); strings.Contains(reply[end:], `"grades"`) {
			return nil, errors.New("judge: the final grades block is truncated or unparseable")
		}
		for i := range out.Grades {
			out.Grades[i].Area = checklist.Area(strings.ToLower(strings.TrimSpace(string(out.Grades[i].Area))))
			out.Grades[i].Level = checklist.Level(strings.ToLower(strings.TrimSpace(string(out.Grades[i].Level))))
		}
		return out.Grades, nil
	}
	return nil, errors.New("judge: no grades in the reply")
}

// conflicted lists the areas where the verified findings pull both ways: a
// risk and a mitigation. Those get a second pass with thinking on.
func conflicted(fs []finding.Finding) []checklist.Area {
	risk, mit := map[checklist.Area]bool{}, map[checklist.Area]bool{}
	for _, f := range fs {
		switch f.Stance {
		case finding.Risk:
			risk[f.Area] = true
		case finding.Mitigation:
			mit[f.Area] = true
		}
	}
	var out []checklist.Area
	for _, a := range checklist.Areas() {
		if risk[a.ID] && mit[a.ID] {
			out = append(out, a.ID)
		}
	}
	return out
}

// Judge grades the checklist with the judge model.
type Judge struct {
	Model Chatter
	Meter Meter
	// Fast and Think are merged into the first pass and the conflict pass:
	// the host's thinking switch off and on (judge_fast and judge_think in
	// the settings file).
	Fast, Think map[string]any
	// ThinkMaxTokens caps the conflict pass's reply, thinking included.
	// Zero means 16000.
	ThinkMaxTokens int
}

// Judgement is the judge step's result.
type Judgement struct {
	Grades    []checklist.Grade
	Demotions []checklist.Demotion
	Verdict   checklist.Verdict
	// Conflicted lists the areas sent to the conflict pass, and
	// ConflictErr says why that pass did not count, when it did not.
	Conflicted  []checklist.Area
	ConflictErr error
	Cost        cost.Price
}

// Judge grades every area for a child of age, re-judges the areas whose
// findings pull both ways with thinking on, holds every grade to the
// verified findings with checklist.Enforce, and computes the verdict with
// checklist.Decide. missing names the source kinds that could not be read.
// A failed conflict pass keeps the first grades.
func (j *Judge) Judge(ctx context.Context, title string, age int, ref string, fs []finding.Finding, missing []string) (Judgement, error) {
	var out Judgement
	if j.Model == nil || j.Meter == nil {
		return out, errors.New("judge: a model and a meter are required")
	}
	system := judgeSystem()
	all := make([]checklist.Area, 0, len(checklist.Areas()))
	for _, a := range checklist.Areas() {
		all = append(all, a.ID)
	}
	reply, price, err := chat(ctx, j.Meter, j.Model, "judge", ref, []model.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: judgeUser(title, age, all, fs, missing)},
	}, model.Options{JSON: true, Extra: j.Fast})
	out.Cost += price
	if err != nil {
		return out, fmt.Errorf("judge: %w", err)
	}
	grades, err := parseGrades(reply.Text)
	if err != nil {
		return out, err
	}

	out.Conflicted = conflicted(fs)
	if len(out.Conflicted) > 0 {
		max := j.ThinkMaxTokens
		if max <= 0 {
			max = 16000
		}
		reply, price, err := chat(ctx, j.Meter, j.Model, "judge_think", ref, []model.Message{
			{Role: "system", Content: system},
			{Role: "user", Content: judgeUser(title, age, out.Conflicted, fs, missing)},
		}, model.Options{Extra: j.Think, MaxTokens: max})
		out.Cost += price
		if err == nil {
			var second []checklist.Grade
			if second, err = parseGrades(reply.Text); err == nil {
				if answers(second, out.Conflicted) {
					grades = replace(grades, second, out.Conflicted)
				} else {
					err = errors.New("judge: the conflict pass graded none of the conflicted areas")
				}
			}
		}
		out.ConflictErr = err
		if err := ctx.Err(); err != nil {
			return out, err
		}
	}

	out.Grades, out.Demotions = checklist.Enforce(grades, finding.Cited(fs))
	var thin []checklist.Demotion
	out.Grades, thin = ground(out.Grades, fs, title)
	out.Demotions = append(out.Demotions, thin...)
	out.Verdict, err = checklist.Decide(out.Grades)
	if err != nil {
		return out, fmt.Errorf("judge: %w", err)
	}
	return out, nil
}

// answers reports whether grades holds at least one of areas.
func answers(grades []checklist.Grade, areas []checklist.Area) bool {
	for _, g := range grades {
		if slices.Contains(areas, g.Area) {
			return true
		}
	}
	return false
}

// replace swaps in the second pass's grade for each re-judged area it
// answered. An area the second pass left out keeps its first grade.
func replace(first, second []checklist.Grade, areas []checklist.Area) []checklist.Grade {
	answered := map[checklist.Area]checklist.Grade{}
	for _, g := range second {
		if _, dup := answered[g.Area]; !dup && slices.Contains(areas, g.Area) {
			answered[g.Area] = g
		}
	}
	out := slices.Clone(first)
	seen := map[checklist.Area]bool{}
	for i, g := range out {
		if ng, ok := answered[g.Area]; ok {
			out[i] = ng
			seen[g.Area] = true
		}
	}
	// An area the first pass left out takes the second pass's grade.
	for _, a := range areas {
		if ng, ok := answered[a]; ok && !seen[a] {
			out = append(out, ng)
		}
	}
	return out
}

// RuleThinEvidence is the demotion ground makes: a flag or ok_with_setting
// whose reason names none of the verified findings it cites.
const RuleThinEvidence = "reason_without_quote"

// ThinReason is the card reason for a grade ground dropped to unknown.
const ThinReason = "Changed to unknown: the evidence was thin. No verified quote backs the reason the judge gave."

// minShared is how many content words a reason (and, for ok_with_setting,
// its setting) must share with a cited finding's verified quote to name it.
const minShared = 2

// ground holds every flag and ok_with_setting to the findings its reason
// names (T3.9). Enforce has already kept only verified citations in the
// grade's own area. A cited finding is named when it has the right stance
// (a risk for flag; a risk or mitigation for ok_with_setting) and its
// verified quote shares at least minShared content words with the reason (the
// claim is the reader's paraphrase, so it does not count). The
// grade keeps only the findings its reason names. A grade whose reason names
// none falls to unknown with ThinReason, so its flag no longer counts toward
// the verdict. ok, unknown, and the verdict rule are untouched. The input is
// not modified.
func ground(grades []checklist.Grade, fs []finding.Finding, title string) ([]checklist.Grade, []checklist.Demotion) {
	byID := make(map[string]finding.Finding, len(fs))
	for _, f := range fs {
		byID[f.ID] = f
	}
	skip := map[string]bool{}
	for w := range contentWords(title, nil) {
		skip[w] = true
	}
	var dem []checklist.Demotion
	out := make([]checklist.Grade, len(grades))
	for i, g := range grades {
		g.Findings = slices.Clone(g.Findings)
		out[i] = g
		if g.Level != checklist.Flag && g.Level != checklist.OKWithSetting {
			continue
		}
		said := contentWords(g.Reason+" "+g.Setting, skip)
		var named []string
		for _, id := range g.Findings {
			f, ok := byID[id]
			if !ok || f.Area != g.Area || !stanceFits(g.Level, f.Stance) {
				continue
			}
			if shared(said, contentWords(f.Quote, skip)) >= minShared {
				named = append(named, id)
			}
		}
		if len(named) == 0 {
			dem = append(dem, checklist.Demotion{Area: g.Area, Rule: RuleThinEvidence, Detail: string(g.Level)})
			g.Level, g.Setting, g.Reason = checklist.Unknown, "", ThinReason
			out[i] = g
			continue
		}
		out[i].Findings = named
	}
	return out, dem
}

func stanceFits(l checklist.Level, s finding.Stance) bool {
	if l == checklist.Flag {
		return s == finding.Risk
	}
	return s == finding.Risk || s == finding.Mitigation
}

func shared(a, b map[string]bool) int {
	n := 0
	for w := range a {
		if b[w] {
			n++
		}
	}
	return n
}

// stopWords are words too common in reasons and quotes to show that a
// reason speaks to a quote.
var stopWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`about above after again against also although among another because been before being below between both cannot could does doing down during each even every from further have having here into itself just like many more most much must only other over same several should since some such than that their them then there these they this those through under until very were what when where which while with within without would your yours
	parent parents child children kids users user people game games play playing player players report reports reported says said app apps make makes made allow allows allowed multiple there's isn't doesn't don't can't`) {
		stopWords[w] = true
	}
}

// contentWords returns the stems of the words in s that carry meaning: four
// letters or more, not a stop word or in skip, cut to five letters so
// "message" and "messages" meet.
func contentWords(s string, skip map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\''
	}) {
		w = strings.Trim(w, "'")
		r := []rune(w)
		if len(r) < 4 || stopWords[w] {
			continue
		}
		if len(r) > 5 {
			r = r[:5]
		}
		if st := string(r); !skip[st] {
			out[st] = true
		}
	}
	return out
}
