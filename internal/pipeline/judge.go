package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

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

// parseGrades finds the grades object in a reply. A thinking reply can
// carry reasoning, braces included, before the answer, and a host may wrap
// the answer in a code fence, so every opening brace is tried until one
// decodes to an object with grades.
func parseGrades(reply string) ([]checklist.Grade, error) {
	for i := 0; i < len(reply); i++ {
		if reply[i] != '{' {
			continue
		}
		var out struct {
			Grades []checklist.Grade `json:"grades"`
		}
		if err := json.NewDecoder(strings.NewReader(reply[i:])).Decode(&out); err != nil || len(out.Grades) == 0 {
			continue
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
				grades = replace(grades, second, out.Conflicted)
			}
		}
		out.ConflictErr = err
		if err := ctx.Err(); err != nil {
			return out, err
		}
	}

	out.Grades, out.Demotions = checklist.Enforce(grades, finding.Cited(fs))
	out.Verdict, err = checklist.Decide(out.Grades)
	if err != nil {
		return out, fmt.Errorf("judge: %w", err)
	}
	return out, nil
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
