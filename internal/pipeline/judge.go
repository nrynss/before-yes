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
	"golang.org/x/text/unicode/norm"

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
- "ok_with_setting": a concern exists, and a specific setting or control answers it. Name the setting exactly as the quote of a cited "mitigation" finding names it ("Parental Controls > Chat > Off", "Ask to Buy"). Cite the "risk" finding that shows the concern and the "mitigation" finding that names the setting.
- "flag": a concern for this age that no setting answers. Cite only "risk" findings.
- "unknown": the findings are too thin to say.

How to weigh evidence:
- First-hand reports of what actually happened outweigh what a policy or a store listing claims. One credible report that a filter or an age gate failed for a young child can decide an area.
- Grade against the child's age. What is fine at 13 may be a flag at 7.
- A source listed as unread was not read. Its silence is not evidence: do not grade an area "ok" because a policy that could not be read said nothing.

How to cite. Every verdict a parent sees rests on the quotes of the findings you cite, so a citation is checked by code:
- "findings" lists the ids of the findings your grade rests on, copied exactly from the list. Cite the few that decide the area (usually one to four), never every finding in it. Do not invent ids.
- Every id you cite must be in that area. A "flag" may cite only findings whose stance is "risk". An "ok_with_setting" may cite only "risk" or "mitigation" findings, and at least one "mitigation" finding's quote must name the setting.
- One id that does not exist, is in another area, or has the wrong stance voids the whole grade: it becomes "unknown". If you cannot cite a finding that fits, grade the area "unknown".
- Write the reason in your own plain words for a parent. It must say only what the cited quotes say.

Reply with one JSON object:
{"grades": [{"area": "...", "level": "...", "reason": "one plain sentence for a parent", "setting": "...", "findings": ["id", ...]}, ...]}
with exactly one grade per area. Leave "setting" empty unless the level is "ok_with_setting".`)
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
	// Thin lists the areas dropped to unknown because a cited id did not
	// check out or no cited mitigation quote named the setting (T3.9,
	// T3.9b). The card carries it
	// as ThinAreas.
	Thin []checklist.Area
	Cost cost.Price
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

	// Citations are checked on the judge's own list, before Enforce drops
	// the ids it cannot use: one bad id voids a flag or a setting (T3.9b).
	var cited, grounded []checklist.Demotion
	var thinCited, thinGround []checklist.Area
	grades, cited, thinCited = checkCitations(grades, fs)
	out.Grades, out.Demotions = checklist.Enforce(grades, finding.Cited(fs))
	out.Demotions = append(cited, out.Demotions...)
	out.Grades, grounded, thinGround = ground(out.Grades, fs)
	out.Demotions = append(out.Demotions, grounded...)
	out.Thin = thinAreas(out.Grades, thinCited, thinGround)
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

// Demotions the judge step makes after Enforce. Each leaves the grade
// unknown and its area in Judgement.Thin.
const (
	// RuleBadCitation: a flag or ok_with_setting that cites no finding, or
	// cites an id that is not a verified finding, is in another area, or has
	// a stance the grade does not fit (T3.9b). One bad id voids the grade.
	RuleBadCitation = "citation_invalid"
	// RuleSettingUnbacked: an ok_with_setting whose setting appears in no
	// cited verified mitigation quote.
	RuleSettingUnbacked = "setting_without_quote"
)

// ThinReason is the card reason for a grade checkCitations dropped
// to unknown. The card's structured signal is Card.ThinAreas; this text is
// for the parent.
const ThinReason = "Changed to unknown: the evidence was thin. No verified quote backs the reason the judge gave."

// SettingThinReason is the card reason when only the setting was unbacked.
const SettingThinReason = "Changed to unknown: the evidence was thin. No verified quote names the setting."

// checkCitations holds every flag and ok_with_setting to the findings it
// cites (T3.9b). It reads the judge's own citation list, before
// checklist.Enforce drops the ids it cannot use. The grade stands only if
// it cites at least one finding and every cited id is a verified finding in
// fs (fs holds only findings that passed finding.Verify), belongs to the
// grade's area, and has a fitting stance: a risk for a flag; a risk or a
// mitigation for ok_with_setting. A grade that fails becomes unknown with
// no setting and no citations, a RuleBadCitation demotion, and its area in
// thin. ok and unknown grades pass through, and the input is not modified.
func checkCitations(grades []checklist.Grade, fs []finding.Finding) (out []checklist.Grade, dem []checklist.Demotion, thin []checklist.Area) {
	byID := make(map[string]finding.Finding, len(fs))
	for _, f := range fs {
		byID[f.ID] = f
	}
	out = make([]checklist.Grade, len(grades))
	for i, g := range grades {
		g.Findings = slices.Clone(g.Findings)
		out[i] = g
		if (g.Level != checklist.Flag && g.Level != checklist.OKWithSetting) || !g.Area.Valid() {
			continue
		}
		bad := ""
		seen := 0
		for _, id := range g.Findings {
			id = strings.TrimSpace(id)
			f, ok := byID[id]
			switch {
			case !ok:
				bad = id + " is not a verified finding"
			case f.Area != g.Area:
				bad = id + " is in area " + string(f.Area)
			case !stanceFits(g.Level, f.Stance):
				bad = id + " is a " + string(f.Stance) + " finding"
			default:
				seen++
				continue
			}
			break
		}
		if bad == "" && seen == 0 {
			bad = "no finding cited"
		}
		if bad == "" {
			continue
		}
		dem = append(dem, checklist.Demotion{Area: g.Area, Rule: RuleBadCitation, Detail: string(g.Level) + ": " + bad})
		thin = append(thin, g.Area)
		g.Level, g.Setting, g.Reason, g.Findings = checklist.Unknown, "", ThinReason, []string{}
		out[i] = g
	}
	return out, dem, thin
}

// ground holds every ok_with_setting to a setting a cited mitigation quote
// names (T3.9, kept by T3.9b; see settingIn). By now checkCitations has
// shown every cited id is a verified finding of a fitting stance in the
// grade's area. A setting no cited mitigation quote names falls to unknown
// with its setting cleared, a RuleSettingUnbacked demotion, and its area in
// thin. flag, ok and unknown, and the verdict rule, are untouched. The input
// is not modified.
func ground(grades []checklist.Grade, fs []finding.Finding) (out []checklist.Grade, dem []checklist.Demotion, thin []checklist.Area) {
	byID := make(map[string]finding.Finding, len(fs))
	for _, f := range fs {
		byID[f.ID] = f
	}
	out = make([]checklist.Grade, len(grades))
	for i, g := range grades {
		g.Findings = slices.Clone(g.Findings)
		out[i] = g
		if g.Level != checklist.OKWithSetting {
			continue
		}
		setting := normWords(g.Setting)
		backed := false
		for _, id := range g.Findings {
			if f, ok := byID[id]; ok && f.Area == g.Area && f.Stance == finding.Mitigation && settingIn(normWords(f.Quote), setting) {
				backed = true
				break
			}
		}
		if backed {
			continue
		}
		dem = append(dem, checklist.Demotion{Area: g.Area, Rule: RuleSettingUnbacked, Detail: string(g.Level)})
		thin = append(thin, g.Area)
		g.Level, g.Setting, g.Reason = checklist.Unknown, "", SettingThinReason
		out[i] = g
	}
	return out, dem, thin
}

// thinAreas merges the areas each step dropped, once each, in card order,
// keeping only those whose final grade is unknown (a duplicate grade for an
// area is discarded by Enforce, and its twin may stand).
func thinAreas(final []checklist.Grade, parts ...[]checklist.Area) []checklist.Area {
	var out []checklist.Area
	for _, g := range final {
		if g.Level != checklist.Unknown {
			continue
		}
		for _, p := range parts {
			if slices.Contains(p, g.Area) {
				out = append(out, g.Area)
				break
			}
		}
	}
	return out
}

// settingIn reports whether a quote backs a setting: the whole setting
// appears in it, or every content word of the setting does (settingWord:
// three letters or more, not glue such as "turn", "on" or "the"), with at
// least one such word. A shared phrase such as "made in the app" cannot
// carry a control the quote never names.
func settingIn(quote, setting []string) bool {
	var content []string
	for _, w := range setting {
		if settingWord(w) {
			content = append(content, w)
		}
	}
	if len(content) == 0 {
		return false // "Turn on" names no control
	}
	if carries(quote, setting, len(setting), 0) {
		return true
	}
	have := map[string]bool{}
	for _, w := range quote {
		have[w] = true
	}
	for _, w := range content {
		if !have[w] {
			return false
		}
	}
	return true
}

func stanceFits(l checklist.Level, s finding.Stance) bool {
	if l == checklist.Flag {
		return s == finding.Risk
	}
	return s == finding.Risk || s == finding.Mitigation
}

// carries reports whether text holds a run of n consecutive words of span,
// at least minContent of them content words. A span shorter than n words
// must appear whole, with at least one content word.
func carries(text, span []string, n, minContent int) bool {
	if len(span) == 0 || len(text) == 0 {
		return false
	}
	if len(span) < n {
		n = len(span)
		minContent = min(minContent, 1)
	}
	for i := 0; i+n <= len(span); i++ {
		run := span[i : i+n]
		c := 0
		for _, w := range run {
			if contentWord(w) {
				c++
			}
		}
		if c < minContent {
			continue
		}
		for j := 0; j+n <= len(text); j++ {
			if slices.Equal(text[j:j+n], run) {
				return true
			}
		}
	}
	return false
}

// stopWords are words too common to show that a run of words names a quote.
var stopWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`about above after again against also although among another because been before being below between both cannot could does doing down during each even every from further have having here into itself just like many more most much must only other over same several should since some such than that their them then there these they this those through under until very were what when where which while with within without would your yours dont doesnt isnt cant`) {
		stopWords[w] = true
	}
}

func settingWord(w string) bool {
	return len([]rune(w)) >= 3 && !stopWords[w] && !settingGlue[w]
}

// settingGlue are short words a setting shares with any sentence.
var settingGlue = map[string]bool{"the": true, "and": true, "for": true, "you": true, "can": true, "off": true, "all": true, "any": true, "are": true, "has": true, "not": true, "out": true, "use": true, "turn": true}

func contentWord(w string) bool {
	return len([]rune(w)) >= 4 && !stopWords[w]
}

// normWords lowercases s, drops apostrophes, and splits it into words of
// letters and digits.
func normWords(s string) []string {
	s = strings.NewReplacer("'", "", "’", "", "‘", "").Replace(strings.ToLower(norm.NFC.String(s)))
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
