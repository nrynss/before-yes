package main

// Diagnosis names, for each area, the grade the judge gave, the grade the
// card kept, the quote the judge cited, and which check changed the grade.
// It reads the judge's reply off the chatter and does not change a grade.
// The check names are the ones judge.go writes into the card reason.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/nrynss/keel/cost"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/finding"
	"github.com/nrynss/before-yes/internal/model"
	"github.com/nrynss/before-yes/internal/pipeline"
)

// tracingChatter records judge replies and entailment replies. Reader
// chunks are not kept. It delegates every call.
type tracingChatter struct {
	inner pipeline.Chatter
	role  string
	tr    *callTrace
}

type traced struct {
	user  string
	reply string
	// err is the call's error, for an entailment call that failed: the
	// judge then keeps the grade unchecked.
	err string
	// capped is set when the reply stopped at its token cap.
	capped bool
}

type callTrace struct {
	mu             sync.Mutex
	judge          []traced
	entail         []traced
	checkedReasons map[checklist.Area]string
}

func (t *callTrace) captureCheckedReasons(reasons map[checklist.Area]string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.checkedReasons = reasons
}

func bare(c pipeline.Chatter) pipeline.Chatter {
	if t, ok := c.(*tracingChatter); ok {
		return t.inner
	}
	return c
}

func (t *tracingChatter) Settings() model.Settings { return t.inner.Settings() }

func (t *tracingChatter) Estimate(msgs []model.Message, o model.Options) cost.Price {
	return t.inner.Estimate(msgs, o)
}

func (t *tracingChatter) Chat(ctx context.Context, msgs []model.Message, o model.Options) (model.Reply, error) {
	r, err := t.inner.Chat(ctx, msgs, o)
	if t.tr == nil {
		return r, err
	}
	var system, user string
	for _, m := range msgs {
		switch m.Role {
		case "system":
			system = m.Content
		case "user":
			user = m.Content
		}
	}
	t.tr.mu.Lock()
	switch {
	case strings.Contains(system, "meets_definition") || strings.HasPrefix(system, "Classify the observed event in one source quote about age checks."):
		// Entailment can run on the judge client. Classify it before the
		// role, or its JSON is read as a second judge pass.
		c := traced{user: user, reply: r.Text}
		if err != nil {
			c.err = err.Error()
		}
		t.tr.entail = append(t.tr.entail, c)
	case err != nil:
		// A failed grading call leaves no reply to read.
	case strings.HasPrefix(system, "Write explanations for a parent") || strings.HasPrefix(system, "You check the exact explanation a parent"):
		// Prose-only calls cannot replace a grading pass in the diagnosis.
	case t.role == "judge":
		max := o.MaxTokens
		if max <= 0 {
			max = t.inner.Settings().MaxTokens
		}
		t.tr.judge = append(t.tr.judge, traced{user: user, reply: r.Text, capped: r.Capped(max)})
	}
	t.tr.mu.Unlock()
	return r, err
}

type rawGrade struct {
	Area     string   `json:"area"`
	Level    string   `json:"level"`
	Reason   string   `json:"reason"`
	Setting  string   `json:"setting"`
	Findings []string `json:"findings"`
}

// parseReplyGrades mirrors pipeline.parseGrades: the last grades object
// wins, and a later grades key that does not decode is a truncated answer.
func parseReplyGrades(reply string) ([]rawGrade, error) {
	for i := len(reply) - 1; i >= 0; i-- {
		if reply[i] != '{' {
			continue
		}
		var out struct {
			Grades []rawGrade `json:"grades"`
		}
		dec := json.NewDecoder(strings.NewReader(reply[i:]))
		if err := dec.Decode(&out); err != nil || len(out.Grades) == 0 {
			continue
		}
		if end := i + int(dec.InputOffset()); strings.Contains(reply[end:], `"grades"`) {
			return nil, fmt.Errorf("truncated grades")
		}
		return out.Grades, nil
	}
	return nil, fmt.Errorf("no grades")
}

// mapRawCitations maps a reply's short citation ids to the real finding ids,
// as the judge does before its checks, with pipeline.MapShortCitation.
func mapRawCitations(grades []rawGrade, shortToReal map[string]string) []rawGrade {
	for i := range grades {
		for n, id := range grades[i].Findings {
			grades[i].Findings[n] = pipeline.MapShortCitation(shortToReal, id)
		}
	}
	return grades
}

// judgeGrades is the grade list the checks saw: the first pass, with the
// conflict pass swapped in for the areas it answered. fs are the findings the
// judge was given; each pass's short ids map through the same map its prompt
// was built from.
func judgeGrades(tr *callTrace, conflicted []checklist.Area, fs []finding.Finding) (map[checklist.Area]rawGrade, map[checklist.Area]rawGrade, string) {
	if tr == nil {
		return nil, nil, "no judge reply captured"
	}
	tr.mu.Lock()
	replies := append([]traced{}, tr.judge...)
	tr.mu.Unlock()
	if len(replies) == 0 {
		return nil, nil, "no judge reply captured"
	}
	first, err := parseReplyGrades(replies[0].reply)
	if err != nil {
		return nil, nil, "first pass: " + err.Error()
	}
	all := make([]checklist.Area, 0, len(checklist.Areas()))
	for _, a := range checklist.Areas() {
		all = append(all, a.ID)
	}
	first = mapRawCitations(first, pipeline.ShortCitationIDs(all, fs))
	use := map[checklist.Area]rawGrade{}
	firsts := map[checklist.Area]rawGrade{}
	for _, g := range first {
		a := checklist.Area(strings.ToLower(strings.TrimSpace(g.Area)))
		g.Level = strings.ToLower(strings.TrimSpace(g.Level))
		use[a] = g
		firsts[a] = g
	}
	note := "first pass"
	if len(replies) == 1 {
		return use, firsts, note
	}
	second, err := parseReplyGrades(replies[len(replies)-1].reply)
	if err != nil {
		if replies[len(replies)-1].capped {
			return use, firsts, "conflict pass CAPPED at its token limit with no usable grades (" + err.Error() + "); first pass kept"
		}
		return use, firsts, "conflict pass unusable (" + err.Error() + "); first pass kept"
	}
	second = mapRawCitations(second, pipeline.ShortCitationIDs(conflicted, fs))
	answered := false
	for _, g := range second {
		a := checklist.Area(strings.ToLower(strings.TrimSpace(g.Area)))
		if slices.Contains(conflicted, a) {
			answered = true
			break
		}
	}
	if !answered {
		return use, firsts, "conflict pass graded none of the conflicted areas; first pass kept"
	}
	for _, g := range second {
		a := checklist.Area(strings.ToLower(strings.TrimSpace(g.Area)))
		if slices.Contains(conflicted, a) {
			g.Level = strings.ToLower(strings.TrimSpace(g.Level))
			use[a] = g
		}
	}
	return use, firsts, "conflict pass replaced the conflicted areas (an area the checks then made unknown may fall back to its first-pass grade)"
}

// citationFault is checkCitations' first reason to void a flag or a
// setting, or "" when the citation would stand. It stops at the first bad
// id, as checkCitations does.
func citationFault(g rawGrade, byID map[string]finding.Finding) string {
	if g.Level != string(checklist.Flag) && g.Level != string(checklist.OKWithSetting) {
		return ""
	}
	seen := 0
	for _, id := range g.Findings {
		id = strings.TrimSpace(id)
		f, ok := byID[id]
		switch {
		case !ok:
			if id == "" {
				return "no citation"
			}
			return "no citation: " + id + " is not a verified finding"
		case string(f.Area) != strings.ToLower(strings.TrimSpace(g.Area)):
			return "citation outside the area: " + id + " is in " + string(f.Area)
		case g.Level == string(checklist.Flag) && f.Stance != finding.Risk:
			return "wrong stance: " + id + " is a " + string(f.Stance) + " finding"
		case g.Level == string(checklist.OKWithSetting) && f.Stance != finding.Risk && f.Stance != finding.Mitigation:
			return "wrong stance: " + id + " is a " + string(f.Stance) + " finding"
		default:
			seen++
		}
	}
	if seen == 0 {
		return "no citation"
	}
	return ""
}

// checkActed names the check whose reason the card carries, or "stood".
func checkActed(final checklist.Grade, fault string) string {
	switch final.Reason {
	case pipeline.ThinReason:
		if fault != "" {
			return "checkCitations: " + fault
		}
		return "checkCitations"
	case pipeline.SettingThinReason:
		return "ground: setting_without_quote"
	case pipeline.NotEarnedReason:
		return "entail: flag_not_earned"
	case pipeline.UnsupportedReason:
		return "entail: reason_unsupported"
	case checklist.Reasons[checklist.RuleOKNoProof]:
		return "ok_without_finding"
	case checklist.Reasons[checklist.RuleFlagNoRisk]:
		return "flag_without_risk"
	case checklist.Reasons[checklist.RuleSettingNoName]:
		return "setting_without_name"
	case checklist.Reasons[checklist.RuleSettingNoProof]:
		return "setting_without_finding"
	case checklist.Reasons[checklist.RuleSkipped]:
		return "skipped_area"
	default:
		return "stood"
	}
}

func entailNote(tr *callTrace, area checklist.Area) string {
	if tr == nil {
		return ""
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	mark := "Area: " + string(area) + "\n"
	var notes []string
	for _, c := range tr.entail {
		if !strings.Contains(c.user, mark) {
			continue
		}
		if c.err != "" {
			notes = append(notes, "entail ERROR: "+c.err+" (the grade is kept unchecked)")
			continue
		}
		v, ok := parseEntailReply(c.reply)
		if !ok {
			var w struct {
				Criterion   string `json:"criterion"`
				Quote       string `json:"quote"`
				ActualAge   string `json:"actual_age"`
				AssignedAge string `json:"assigned_age"`
			}
			if json.Unmarshal([]byte(c.reply), &w) == nil && w.Criterion != "" {
				notes = append(notes, fmt.Sprintf("age-gate event=%s witness=%q actual_age=%q assigned_age=%q", w.Criterion, w.Quote, w.ActualAge, w.AssignedAge))
			} else {
				notes = append(notes, "entail reply unparsed")
			}
			continue
		}
		line := fmt.Sprintf("entail meets_definition=%t reason_supported=%t", v.meets, v.reason)
		if v.witness != nil {
			line += fmt.Sprintf(" witness=%s %q", v.witness.Criterion, v.witness.Quote)
		}
		notes = append(notes, line)
	}
	return strings.Join(notes, "; ")
}

// diagnose writes one line per area. Quotes are the judge's citations, not
// only the ones the card kept, so a demoted grade still shows what it cited.
func diagnose(w io.Writer, card pipeline.Card, ev pipeline.Evidence, tr *callTrace) {
	byID := map[string]finding.Finding{}
	counts := map[checklist.Area][3]int{}
	for _, f := range ev.Findings {
		byID[f.ID] = f
		c := counts[f.Area]
		switch f.Stance {
		case finding.Risk:
			c[0]++
		case finding.Mitigation:
			c[1]++
		default:
			c[2]++
		}
		counts[f.Area] = c
	}
	given, firsts, pass := judgeGrades(tr, card.Stats.ConflictedAreas, ev.Findings)
	fmt.Fprintf(w, "diagnose %s age %d verdict %s findings %d (%s)\n", card.Title, card.Age, card.Verdict, len(ev.Findings), pass)
	got := map[checklist.Area]checklist.Grade{}
	for _, g := range card.Grades {
		got[g.Area] = g
	}
	for _, a := range checklist.Areas() {
		g := got[a.ID]
		in, has := given[a.ID]
		// A conflicted area whose card grade is not the conflict pass's, and
		// is the first pass's, took the first-pass fallback: the conflict
		// pass's grade was made unknown by the checks and the first pass's
		// grade stood in its place.
		fallback, cpLine := false, ""
		if has && slices.Contains(card.Stats.ConflictedAreas, a.ID) && g.Level != checklist.Unknown {
			if fg, ok := firsts[a.ID]; ok && string(g.Level) == fg.Level && (in.Level != fg.Level || !withinIDs(g.Findings, in.Findings)) {
				fallback = true
				cpLine = fmt.Sprintf("    conflict pass %s setting %q cited %d %v\n", in.Level, in.Setting, len(in.Findings), in.Findings)
				in = fg
			}
		}
		fault := ""
		if has {
			fault = citationFault(in, byID)
		}
		c := counts[a.ID]
		fmt.Fprintf(w, "  %s  risks %d mitigations %d other %d\n", a.ID, c[0], c[1], c[2])
		if has && fallback {
			fmt.Fprint(w, cpLine)
			fmt.Fprintf(w, "    first-pass fallback: the conflict pass's grade was made unknown by the checks, so the first pass's grade stands\n")
			fmt.Fprintf(w, "    judge (first pass) %s setting %q cited %d %v\n", in.Level, in.Setting, len(in.Findings), in.Findings)
		} else if has {
			fmt.Fprintf(w, "    judge %s setting %q cited %d %v\n", in.Level, in.Setting, len(in.Findings), in.Findings)
			if fault != "" {
				fmt.Fprintf(w, "    citation %s\n", fault)
			}
		} else {
			fmt.Fprintf(w, "    judge (not in the reply)\n")
		}
		fmt.Fprintf(w, "    card %s setting %q\n", g.Level, g.Setting)
		for _, act := range card.Gate {
			if act.Area == a.ID {
				fmt.Fprintf(w, "    gate %s %s -> %s findings %v\n", act.Rule, act.From, act.To, act.Findings)
			}
		}
		checked := g
		if tr != nil {
			tr.mu.Lock()
			if reason, ok := tr.checkedReasons[a.ID]; ok {
				checked.Reason = reason
			}
			tr.mu.Unlock()
		}
		fmt.Fprintf(w, "    check %s\n", checkActed(checked, fault))
		if note := entailNote(tr, a.ID); note != "" {
			fmt.Fprintf(w, "    %s\n", note)
		}
		fmt.Fprintf(w, "    reason %s\n", oneLine(g.Reason))
		ids := g.Findings
		if has && (g.Level == checklist.Unknown || len(ids) == 0) {
			ids = in.Findings
		}
		if len(ids) == 0 && has {
			ids = in.Findings
		}
		for _, id := range ids {
			f, ok := byID[strings.TrimSpace(id)]
			if !ok {
				fmt.Fprintf(w, "    quote %s (missing)\n", id)
				continue
			}
			fmt.Fprintf(w, "    quote %s %s %s: %s\n", f.ID, f.Stance, f.Source.Kind, oneLine(f.Quote))
		}
	}
}

type diagnosticWitness struct {
	Criterion string `json:"criterion"`
	Quote     string `json:"quote"`
}
type entailBits struct {
	witness       *diagnosticWitness
	meets, reason bool
}

func parseEntailReply(reply string) (entailBits, bool) {
	for i := len(reply) - 1; i >= 0; i-- {
		if reply[i] != '{' {
			continue
		}
		var v struct {
			Meets   *bool              `json:"meets_definition"`
			Reason  *bool              `json:"reason_supported"`
			Witness *diagnosticWitness `json:"age_gate_witness"`
		}
		if err := json.NewDecoder(strings.NewReader(reply[i:])).Decode(&v); err != nil || v.Meets == nil || v.Reason == nil {
			continue
		}
		return entailBits{meets: *v.Meets, reason: *v.Reason, witness: v.Witness}, true
	}
	return entailBits{}, false
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 500 {
		return s[:500] + "…"
	}
	return s
}

// withinIDs reports whether every id in sub is in all (a card grade keeps
// the ids of the pass it came from, minus any the checks removed).
func withinIDs(sub, all []string) bool {
	for _, id := range sub {
		if !slices.Contains(all, id) {
			return false
		}
	}
	return true
}
