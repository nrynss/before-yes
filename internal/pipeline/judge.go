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

// areaDefs is each area's own flag definition, written from the brief's
// checklist table and the area questions: what a flag in that area takes, and
// what a setting must answer. The judge is told them (judgeSystem) and the
// entailment check reads the same text (entailUser), so the two steps cannot
// drift apart. internal/checklist is frozen and is not edited; the definitions
// live here (T3.10). The entailment prompt hashes them, so they are final
// before a live re-record.
var areaDefs = map[checklist.Area]string{
	checklist.Contact:         "strangers can reach this child by message, chat or voice, and no control a parent can set shuts that door. A comments-and-questions feature aimed at parents, or a support email address, is not strangers reaching the child. A setting answers it when a cited control actually closes the door (chat off, friends-only messaging).",
	checklist.Money:           "the title pushes purchases at children: loot boxes, pay-to-win pressure, subscription traps, or purchases a child can complete without a parent. A price on its own, or a plan a school pays for, is not purchase pressure on this child. A setting answers it when a cited control gates what a child can buy (Ask to Buy, a spending PIN).",
	checklist.Data:            "children's data is collected and used in ways a parent cannot control: ad tracking aimed at children, third-party sharing or selling, or no way to delete what was collected. A parent's email asked for when the account is set up is not a data flag. Sharing information with service providers who operate the app for the developer (hosting, analytics, support) is not a flag; selling personal data, behavioural advertising or ad tracking, or sharing it for others' marketing is. A setting answers it when a cited control actually stops the collection or sharing it gates.",
	checklist.AgeGating:       "the age gate fails in practice: a first-hand report that a child ended up on the wrong side of it, a child far under the stated age getting in, a check that accepts any birth date, or a check that places a child at the wrong age. What the listing or the policy claims about the rating is a claim, not proof the gate works; one credible first-hand report that it failed outweighs it. A setting answers it when a cited control locks the account to the right age band.",
	checklist.Content:         "parents or reviewers report children actually meeting unsuitable material on this title: sexual content, realistic violence, horror, harassment or self-harm. Material a reviewer merely dislikes, disagrees with or finds odd, with no harm of that list named, is not unsuitable material; nor is one odd or off-topic review among many, a report about another product, or material this age cannot reach. A setting answers it when a cited control keeps such material away from the child.",
	checklist.DeveloperRecord: "this title's developer has a record of enforcement over how it treats children or their data: fines, data breaches, lawsuits or regulatory actions naming this developer. Another company's enforcement, or the developer's own marketing about its values, is not a record. A setting cannot answer a record.",
}

// standingReason is the card reason for a grade that stands. It is the area
// definition's opening claim: the words of areaDefs before the colon that
// lists examples, or the first sentence when the definition has no colon.
// The example list stays in the prompt. It is not the card reason, because
// that list names harms and practices a cited quote may not show. A model's
// grading sentence is never published directly. The separate parent-prose
// pass can replace this reason only after its own quote-support check.
func standingReason(area checklist.Area) string {
	def := areaDefs[area]
	if head, _, ok := strings.Cut(def, ":"); ok {
		return strings.TrimSpace(head)
	}
	head, _, _ := strings.Cut(def, ".")
	return strings.TrimSpace(head)
}

func judgeSystem() string {
	var b strings.Builder
	b.WriteString(`You grade an app, game or channel for one child against a fixed parent checklist, using only the findings you are given. Every finding has an id, a stance, and a verbatim quote from a named source.

Areas:
`)
	for _, a := range checklist.Areas() {
		fmt.Fprintf(&b, "- %s: %s\n", a.ID, a.Question)
	}
	b.WriteString(`
What each area's "flag" means. Grade "flag" only when the findings you cite show the area's own definition for a child of this age:
`)
	for _, a := range checklist.Areas() {
		fmt.Fprintf(&b, "- %s: %s\n", a.ID, areaDefs[a.ID])
	}
	b.WriteString(`
Proportion. A flag is a serious concern a parent of this age should act on, not whatever can be cited. A feature that merely sits in the area, a price, an email field, another product of the same developer or another company's story meets no flag definition: grade "ok", "ok_with_setting" or "unknown" instead. Grade "ok" when the findings you cite show the area is fine for this age; every level cites the findings it rests on, "ok" included, and an area with nothing to cite is "unknown". Grade "ok_with_setting" when the definition's concern is real and the quote of a cited "mitigation" finding names the control that answers it.

For each area give one level:
- "ok": nothing in the findings meets the area's flag definition for a child of this age.
- "ok_with_setting": a concern the area's definition names exists, and a specific setting or control answers it. Name the setting exactly as the quote of a cited "mitigation" finding names it ("Parental Controls > Chat > Off", "Ask to Buy"). Cite the "risk" finding that shows the concern and the "mitigation" finding that names the setting.
- "flag": the findings meet the area's flag definition, and no setting answers it. Cite only "risk" findings.
- "unknown": the findings are too thin to say.

How to weigh evidence:
- First-hand reports of what actually happened outweigh what a policy or a store listing claims. One credible report that a filter or an age gate failed for a young child can decide an area; but that report must be about this title and this age, not about a sibling product of the same developer and not another company's story.
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
		if !slices.Contains(areas, f.Area) || gateOnly(f) {
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
	// CheckedReasons observes the completed checks before parent prose replaces
	// their explanations. The supplied map is a copy for diagnostics only.
	CheckedReasons func(map[checklist.Area]string)

	Model Chatter
	Meter Meter
	// Entail is the judge model, with thinking off (Fast). It checks each
	// standing grade (age flags check cited risks independently) and its cited
	// findings against the area's flag definition and the reason against
	// the quotes (T3.10, fault 3). The reader model accepted quotes the
	// definitions exclude, so the server wires this client, the same one
	// as Model (T3.11). Nil skips the step, so tests that do not wire it
	// keep compiling and passing.
	Entail Chatter
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
	// EntailErr says why an entailment call did not count, when one failed
	// (a call error or an unparseable reply). Like ConflictErr it keeps the
	// non-age grades as they are (T3.10). Age flags require a verified
	// source-event witness and become unknown without one.
	EntailErr error
	// Thin lists the areas dropped to unknown because a cited id did not
	// check out, no cited mitigation quote named the setting (T3.9, T3.9b),
	// or the entailment check found the cited findings do not meet the
	// area's flag definition or the reason goes beyond the quotes (T3.10).
	// The card carries it as ThinAreas.
	Thin []checklist.Area
	// Gate lists what the rule gate did to the grades, in order (rulegate.go).
	Gate []GateAction
	Cost cost.Price
}

// Judge grades every area for a child of age, re-judges the areas whose
// findings pull both ways with thinking on, holds every grade to the
// verified findings with checklist.Enforce, and computes the verdict with
// checklist.Decide. missing names the source kinds that could not be read.
// A failed conflict pass keeps the first grades. The rule gate (ApplyRules)
// runs last, after the parent-reason writer and before Decide; opts reach it.
func (j *Judge) Judge(ctx context.Context, title string, age int, ref string, fs []finding.Finding, missing []string, opts ...Option) (Judgement, error) {
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
	firstPass := grades
	var conflictPass []checklist.Grade // the conflict pass's grades, when it counted
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
					conflictPass = second
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

	ageWitnesses := map[checklist.Area]ageGateWitness{}
	var stood []checklist.Area
	var standCost cost.Price
	out.Grades, out.Demotions, stood, out.EntailErr, standCost = j.standing(ctx, title, age, ref, grades, fs, ageWitnesses)
	out.Cost += standCost
	if err := ctx.Err(); err != nil {
		return out, err
	}
	// A conflict-pass grade the checks made unknown falls back to the first
	// pass's grade for its area, when the same checks let that one stand.
	var fbDem []checklist.Demotion
	var fbThin []checklist.Area
	var fbErr error
	var fbCost cost.Price
	out.Grades, fbDem, fbThin, fbErr, fbCost = j.conflictFallback(ctx, title, age, ref, firstPass, conflictPass, out.Conflicted, out.Grades, fs, ageWitnesses)
	out.Cost += fbCost
	out.Demotions = append(out.Demotions, fbDem...)
	if out.EntailErr == nil {
		out.EntailErr = fbErr
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	out.Thin = thinAreas(out.Grades, stood, fbThin)
	if j.CheckedReasons != nil {
		reasons := make(map[checklist.Area]string, len(out.Grades))
		for _, g := range out.Grades {
			reasons[g.Area] = g.Reason
		}
		j.CheckedReasons(reasons)
	}
	// Writing is deliberately separate from grading: it cannot change a
	// level, setting, citation or the independently checked age witness.
	if j.Entail != nil {
		var reasonCost cost.Price
		out.Grades, reasonCost = j.parentReasons(ctx, title, age, ref, out.Grades, fs, ageWitnesses)
		out.Cost += reasonCost
		if err := ctx.Err(); err != nil {
			return out, err
		}
	}
	// The rule gate decides last. It reads only the verified findings, so it
	// can move a grade either way on what the quotes show, and every move is
	// recorded.
	if w, ok := ageWitnesses[checklist.AgeGating]; ok {
		opts = append(slices.Clone(opts), withAgeWitness(w.Quote))
	}
	out.Grades, out.Gate = ApplyRules(title, age, out.Grades, fs, opts...)
	out.Thin = gateThin(out.Thin, out.Grades, out.Gate)
	out.Verdict, err = checklist.Decide(out.Grades)
	if err != nil {
		return out, fmt.Errorf("judge: %w", err)
	}
	return out, nil
}

// gateThin updates the thin areas after the rule gate: an area it vetoed to
// unknown is thin (no verified quote backs a concern), and an area it raised
// is no longer. Card order, each once.
func gateThin(thin []checklist.Area, final []checklist.Grade, acts []GateAction) []checklist.Area {
	vetoed := map[checklist.Area]bool{}
	for _, a := range acts {
		if a.To == checklist.Unknown && a.From != checklist.Unknown {
			vetoed[a.Area] = true
		}
	}
	var out []checklist.Area
	for _, g := range final {
		if g.Level == checklist.Unknown && (slices.Contains(thin, g.Area) || vetoed[g.Area]) {
			out = append(out, g.Area)
		}
	}
	return out
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

// standing applies the checks that decide whether each grade stands, in
// order: citations (checkCitations, which drops bad ids and voids a grade
// only when none is left), Enforce, grounding, then entailment. It returns
// the grades, the demotions in that order, and the areas the citation,
// grounding and entailment checks dropped to unknown. The input is not
// modified.
func (j *Judge) standing(ctx context.Context, title string, age int, ref string, grades []checklist.Grade, fs []finding.Finding, witnesses map[checklist.Area]ageGateWitness) (out []checklist.Grade, dem []checklist.Demotion, thin []checklist.Area, entailErr error, price cost.Price) {
	out, cited, thinCited := checkCitations(grades, fs)
	var enforced []checklist.Demotion
	out, enforced = checklist.Enforce(out, finding.Cited(fs))
	var grounded []checklist.Demotion
	var thinGround []checklist.Area
	out, grounded, thinGround = ground(out, fs)
	// The entailment check (T3.10, fault 3): one call per standing flag or
	// ok_with_setting, asking whether the cited findings meet the area's
	// flag definition for this age and whether the reason says only what
	// the quotes support. A failed or unparseable call keeps the grade and
	// is recorded in EntailErr, as a failed conflict pass keeps the first
	// grades in ConflictErr.
	var entDem []checklist.Demotion
	var thinEntail []checklist.Area
	if j.Entail != nil {
		out, entDem, thinEntail, entailErr, price = j.entail(ctx, title, age, ref, out, fs, witnesses)
	}
	dem = append(dem, cited...)
	dem = append(dem, enforced...)
	dem = append(dem, grounded...)
	dem = append(dem, entDem...)
	thin = append(thin, thinCited...)
	thin = append(thin, thinGround...)
	thin = append(thin, thinEntail...)
	return out, dem, thin, entailErr, price
}

// conflictFallback handles the areas the conflict pass re-graded. An area
// whose conflict-pass grade was a concern or ok, and which the checks made
// unknown, takes the first pass's grade for the area when the same checks let
// that grade stand. The first pass's grade goes through standing like any
// other grade, so it is entailed and reaches the parent-reason writer
// unchanged. A first-pass grade that also fails leaves the area unknown, and
// its demotions are recorded. Returns graded, with each fallback swapped in.
func (j *Judge) conflictFallback(ctx context.Context, title string, age int, ref string, firstPass, conflictPass []checklist.Grade, areas []checklist.Area, graded []checklist.Grade, fs []finding.Finding, witnesses map[checklist.Area]ageGateWitness) (out []checklist.Grade, dem []checklist.Demotion, thin []checklist.Area, entailErr error, price cost.Price) {
	out = graded
	var firsts []checklist.Grade
	conflictLevel := map[checklist.Area]checklist.Level{}
	for _, a := range areas {
		cur, ok := firstOf(graded, a)
		if !ok || cur.Level != checklist.Unknown {
			continue
		}
		cg, ok := firstOf(conflictPass, a)
		if !ok || cg.Level == checklist.Unknown {
			continue
		}
		fg, ok := firstOf(firstPass, a)
		if !ok || fg.Level == checklist.Unknown {
			continue
		}
		firsts = append(firsts, fg)
		conflictLevel[a] = cg.Level
	}
	if len(firsts) == 0 {
		return out, nil, nil, nil, 0
	}
	kept, dem, thin, entailErr, price := j.standing(ctx, title, age, ref, firsts, fs, witnesses)
	out = slices.Clone(graded)
	for _, g := range kept {
		if g.Level == checklist.Unknown {
			continue
		}
		for i := range out {
			if out[i].Area == g.Area {
				out[i] = g
				break
			}
		}
		dem = append(dem, checklist.Demotion{Area: g.Area, Rule: RuleConflictFallback, Detail: "conflict pass " + string(conflictLevel[g.Area]) + " made unknown by the checks; first pass " + string(g.Level) + " kept"})
	}
	return out, dem, thin, entailErr, price
}

// firstOf returns the first grade for area in grades.
func firstOf(grades []checklist.Grade, area checklist.Area) (checklist.Grade, bool) {
	for _, g := range grades {
		if g.Area == area {
			return g, true
		}
	}
	return checklist.Grade{}, false
}

// Demotions the judge step makes after Enforce. Each leaves the grade
// unknown and its area in Judgement.Thin.
const (
	// RuleBadCitation: a cited id of a flag or ok_with_setting that is not a
	// verified finding, is in another area, or has a stance the grade does
	// not fit is removed (T3.9b). A grade left with no id, because it cited
	// none or every id was bad, becomes unknown.
	RuleBadCitation = "citation_invalid"
	// RuleConflictFallback: a conflict-pass grade the checks made unknown was
	// replaced by the first pass's grade for its area, which the same checks
	// let stand.
	RuleConflictFallback = "conflict_fallback"
	// RuleSettingUnbacked: an ok_with_setting whose setting appears in no
	// cited verified mitigation quote.
	RuleSettingUnbacked = "setting_without_quote"
	// RuleNotEarned: a standing flag or ok_with_setting whose cited
	// findings, judged by the entailment model against the area's flag
	// definition for this age, do not meet the definition itself (for a
	// setting: no cited mitigation actually answers the cited concern). The
	// definition rule of T3.10, fault 1, enforced through fault 3.
	RuleNotEarned = "flag_not_earned"
	// RuleReasonUnsupported: a flag or setting whose published reason is not
	// supported by its cited quotes (T3.10, fault 3; T3.9's L1). Flags are
	// assessed using standingReason before the call.
	RuleReasonUnsupported = "reason_unsupported"
)

// ThinReason is the card reason for a grade checkCitations dropped
// to unknown. The card's structured signal is Card.ThinAreas; this text is
// for the parent.
const ThinReason = "Changed to unknown: the evidence was thin. No verified quote backs the reason the judge gave."

// SettingThinReason is the card reason when only the setting was unbacked.
const SettingThinReason = "Changed to unknown: the evidence was thin. No verified quote names the setting."

// NotEarnedReason is the card reason when the entailment check found the
// cited findings do not meet the area's flag definition (T3.10).
const NotEarnedReason = "Changed to unknown: the cited quotes do not meet this area's flag definition."

// UnsupportedReason is the card reason when the entailment check found the
// reason says more than the cited quotes support (T3.10).
const UnsupportedReason = "Changed to unknown: the reason says more than the cited quotes support."

// checkCitations holds every flag and ok_with_setting to the findings it
// cites (T3.9b, kept by the citation fix). It reads the judge's own citation
// list, before checklist.Enforce drops the ids it cannot use. A cited id is
// kept only if it is a verified finding in fs (fs holds only findings that
// passed finding.Verify), belongs to the grade's area, and has a fitting
// stance: a risk for a flag; a risk or a mitigation for ok_with_setting. Each
// id that fails is removed and recorded as a RuleBadCitation demotion with
// the id and reason. The grade stands with its level, setting and reason
// unchanged while at least one cited id is left, and the kept ids keep their
// order. A grade with no id left (none cited, or all bad) becomes unknown
// with no setting and no citations, one more RuleBadCitation demotion, and
// its area in thin. ok and unknown grades pass through, and the input is not
// modified.
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
		var keep []string
		for _, id := range g.Findings {
			f, ok := byID[strings.TrimSpace(id)]
			why := ""
			switch {
			case !ok:
				why = "is not a verified finding"
			case f.Area != g.Area:
				why = "is in area " + string(f.Area)
			case !stanceFits(g.Level, f.Stance):
				why = "is a " + string(f.Stance) + " finding"
			}
			if why == "" {
				keep = append(keep, id)
				continue
			}
			dem = append(dem, checklist.Demotion{Area: g.Area, Rule: RuleBadCitation, Detail: string(g.Level) + ": " + strings.TrimSpace(id) + " " + why})
		}
		if len(keep) > 0 {
			g.Findings = keep
			out[i] = g
			continue
		}
		detail := string(g.Level) + ": no finding cited"
		if len(g.Findings) > 0 {
			detail = string(g.Level) + ": no verified finding left"
		}
		dem = append(dem, checklist.Demotion{Area: g.Area, Rule: RuleBadCitation, Detail: detail})
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

// entailSystem is the entailment check's fixed instruction. The replies are
// recorded and replayed keyed on the exact messages, so this text is final
// before a live re-record. A quote meets the definition only by showing the
// harm areaDefs names. One quote that shows less does not cancel one that
// shows it. reason_supported is false when a free-form reason states a fact
// no cited quote shows. Naming the definition's other clauses is allowed
// only when the reason text is the definition's opening statement.
func entailSystem() string {
	return `You check one grade of a parent checklist against the findings its grade cites, for one child. You are given the area's flag definition, the child's age, the grade, and each cited finding with its verbatim quote. Answer two questions and nothing else.
(a) meets_definition. Judge the quotes only. The reason is not evidence: meets_definition is false when no cited quote shows the harm, even if the reason names it.
For a "flag", true when at least one cited quote itself shows what the area's flag definition requires for a child of this age. A quote that shows less does not meet the definition, and it does not cancel a cited quote that does.
For an "ok_with_setting", true only when both of these hold, and false if either fails:
1. A cited risk quote, in its own words, shows the harm the flag definition names. A cited mitigation does not supply a harm the risk quote lacks. A description of ordinary use, a list of what a player can do, a permission the product asks for, or the name of a screen does not show that harm.
2. A cited mitigation quote names a control that closes the hole that risk quote describes. Take the child the risk quote describes, doing the thing that quote says they do, and read the mitigation quote for whom the control applies to and what it stops. If that child is outside the control, the control does not close the hole. A control handed out only after a check the risk quote says the child can get past does not close that hole. A mitigation quote that only names a control, and does not say the control stops the thing the risk quote describes, does not close the hole.
Hold each definition to its own words. A quote must show the harm that definition names.
- Contact is strangers reaching this child by message, chat, or voice. The risk quote shows it when a person the child does not know reaches the child that way. Playing with people the child knows does not. A quote that lists what a player can do in the product, and never says a stranger messages, chats with, or speaks to the child, does not show contact. A mitigation that limits chat does not meet the definition when no cited risk quote shows that harm.
- Money is a child pushed into a purchase, or a child completing one. A price on its own, or a plan a school pays for, is not that. A mitigation meets it when its quote says the control stops the child completing the purchase the risk quote describes. The control's name alone does not.
- Data is children's information collected or shared in a way a parent cannot control: ad tracking aimed at children, a third party handling or receiving children's information, or no way to delete what was collected. A claim that the product is safe, private, or compliant, or an email address asked for when an account is set up, is not that. A quote that shows a third party holding the information, or no deletion period, meets data. That quote does not show a use or a recipient it does not name.
- Age gating, for a flag, is true when a cited quote shows a child getting past the gate, a birth date the gate accepted, or the check stating an age or a group that is not the child's. A wish that the product include more, or a complaint about what it teaches or how hard that teaching is, is not a failed gate.
- Age gating, for an ok_with_setting, also needs a mitigation that stops that bypass for the child who takes it. Read the mitigation for who receives the control. If the control is given only to accounts the age check has already treated as under a stated age, it does not reach a child the risk quote says can make that check record a different age. meets_definition is false for that ok_with_setting. This sentence is only for an ok_with_setting. A flag whose quote shows the check recorded an age or a group that is not the child's still meets the definition.
- Content is a child meeting sexual content, realistic violence, horror, harassment, or self-harm. A dislike, a startle, or a wish that names none of those harms is not that.
- A record of enforcement is a fine, a data breach, a lawsuit, or a regulatory action naming this developer. A quote that shows one filed or brought against this developer meets it: the action itself is the record, and the quote need not show its outcome. Another company's action is not a record. A demand that the developer be punished, or a page that only explains a policy, is not a record: the quote itself has to name the fine, the suit, the breach, or the regulator's action.
(b) reason_supported. Do these two steps in order. Stop when a step decides the field.
Step 1. The opening statement is the flag definition before its first colon, or before its first period when there is no colon. Step 1 applies only when the reason is those same words, with nothing changed but the case of the first letter. If you would add, drop, or replace any other word, it is not this step. A paraphrase of the opening statement is step 2. When step 1 does apply, reason_supported is true when the cited quotes show the part of that sentence the grade rests on, and clauses of that copied sentence that the quotes do not show are allowed. Stop.
Step 2. The reason is the judge's own words. List every practice, use, recipient, harm, number, or control it names. A use or a recipient attached to a shown fact is its own item. The flag definition is not a quote: a practice the definition lists is not shown unless a cited quote shows it. A different word for the same fact a cited quote shows is supported. The definition's name for a child who paid is a purchase that child completed without a parent, and that name is supported when any cited quote shows the child paying. Calling that purchase a push, or a purchase inside the product, is the same fact. The word "pushes" is that name. It is not an extra practice, and it must not be why reason_supported is false, when a cited quote shows the child paid. The currency, the amount, and a scam that quote names are the same fact. Another cited quote that names a control a parent could turn on does not erase the purchase. A kind of purchase no quote names is still an extra practice. For that money shape, when a cited quote shows the child paid and the reason names only that purchase, reason_supported is true. That money rule is only for a money grade. It does not make a use supported on any other area.
An extra practice is a concrete thing the reason names that no cited quote shows: a buyer, a kind of purchase, a kind of tracking, a use, a recipient, or a harm the quote does not name. Each harm a free-form reason names is its own item. One extra item fails the reason even when every other item is shown.
A free-form reason does not get that allowance. Naming another clause of the definition is allowed only in step 1.
A cited quote can meet the definition while reason_supported is false. meets_definition does not forgive an extra practice, and the grade does not stand.
Worked split, for a different grade than the one you were given. Reason: "The company shares children's information for advertising, and a partner keeps their photos with no stated deletion period." Quote: "A partner stores the child's photos and does not say for how long." The storage and the missing deletion period are in the quote, so meets_definition is true. reason_supported is false, because advertising is not in the quote. The matching storage does not carry the advertising use. One unshown use fails the reason. A short name for that same use is the same failure. The flag definition listing the use is not a quote. Do not answer true for reason_supported on that shape, on a data grade or any other grade.
Use only the findings given. A quote is the source's own words; a claim is the reader's reading of them. Reply with one JSON object: {"meets_definition": true, "reason_supported": true}, with false where the answer is no. Set each field from the steps above. Do not copy the two trues. A data reason that names a use no cited quote contains has reason_supported false.`
}

// Age gating distinguishes observed identity/access failures from educational
// placement. Classify a source event without a discarded generated reason or
// another child's target age; one credible failure report can earn the flag.
func ageGateSystem() string {
	return `Classify the observed event in one source quote about age checks.
An age gate restricts access by age or assigns an account's age. Choose one criterion:
- access_bypass: the source reports a child passing an age restriction on entry or features meant for older users, or an age check accepting a fictitious birth date.
- wrong_age_assignment: the source reports a displayed or recorded age or age band inconsistent with the person's stated age. Non-overlapping reported ranges suffice; neither an exact birthday nor an explanation of the verification mechanism is required.
- none: the source reports neither failure.
A school grade, course selection, curriculum's intended age range, lessons below a child's ability, an age recommendation, or inability to enter is not an age-gate failure. A generic opinion that age limits do not suffice does not report an observed failure.
For wrong_age_assignment, also extract actual_age and assigned_age: two different exact source spans giving the person's actual age or band and the account's recorded age or band. Each must state a concrete value or age band, not a variable, a hypothetical condition, or a reference to an unspecified correct age. If either value is not stated, criterion is none. Education or school-grade levels are not account age bands.
Return one JSON object with criterion, quote, actual_age and assigned_age strings. Use empty age strings for other criteria. For a failure, quote an exact source span containing the assertion that the failure happened or the bypass works, together with the age mismatch or bypass. A span describing only a conditional correction procedure is not a witness. For none, quote is empty. Do not invent or paraphrase source text.`
}

// entailUser shows one grade and the findings it cites. It mirrors judgeUser's
// finding lines so the two prompts stay recognizable to the same reader model.
func entailUser(title string, age int, g checklist.Grade, fs []finding.Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\nChild's age: %d\nArea: %s\nFlag definition: %s\nLevel: %s\n", title, age, g.Area, areaDefs[g.Area], g.Level)
	if g.Setting != "" {
		fmt.Fprintf(&b, "Setting: %s\n", g.Setting)
	}
	reason := g.Reason
	if g.Level == checklist.Flag {
		// Assess the claim the card will actually publish, independently of
		// unsupported additions in the generator's discarded prose.
		reason = standingReason(g.Area)
	}
	if strings.TrimSpace(reason) == "" {
		reason = "(no reason given)"
	}
	fmt.Fprintf(&b, "Reason: %s\nCited findings:\n", reason)
	byID := make(map[string]finding.Finding, len(fs))
	for _, f := range fs {
		byID[f.ID] = f
	}
	for _, id := range g.Findings {
		f, ok := byID[id]
		if !ok {
			continue
		}
		src := f.Source.Kind
		if f.Source.Kind == KindReview {
			src = fmt.Sprintf("review %d", f.Source.Index)
		}
		fmt.Fprintf(&b, "- stance=%s source=%s\n  quote: %q\n", f.Stance, src, f.Quote)
	}
	return b.String()
}

// entailVerdict is one entailment reply.
type entailVerdict struct {
	Meets   bool
	Reason  bool
	AgeGate *ageGateWitness
}

type ageGateWitness struct {
	ActualAge   string `json:"actual_age"`
	AssignedAge string `json:"assigned_age"`
	Criterion   string `json:"criterion"`
	Quote       string `json:"quote"`
}

// An age-gate acceptance must expose its supporting source span. The model
// chooses the semantic criterion; code verifies its witness is source text,
// in an in-area cited risk, rather than another generated assertion.
// Wrong-age assignment must also expose both actual and assigned age spans
// from that same risk; an unspecified correct age supplies neither side.
func witnessedAgeGate(g checklist.Grade, fs []finding.Finding, w *ageGateWitness) bool {
	if w == nil || (w.Criterion != "access_bypass" && w.Criterion != "wrong_age_assignment") || len(strings.Fields(w.Quote)) < 5 {
		return false
	}
	if w.Criterion == "wrong_age_assignment" && (w.ActualAge == "" || w.AssignedAge == "" || w.ActualAge == w.AssignedAge) {
		return false
	}
	for _, f := range fs {
		if f.Area == checklist.AgeGating && f.Stance == finding.Risk && slices.Contains(g.Findings, f.ID) && strings.Contains(f.Quote, w.Quote) {
			if w.Criterion == "wrong_age_assignment" && (!strings.Contains(f.Quote, w.ActualAge) || !strings.Contains(f.Quote, w.AssignedAge)) {
				continue
			}
			return true
		}
	}
	return false
}

// parseEntail finds the verdict in a reply, trying opening braces from the
// end as parseGrades does. Both answers must be present; a missing one makes
// the call unparseable, which keeps the grade rather than failing it.
func parseEntail(reply string) (entailVerdict, error) {
	for i := len(reply) - 1; i >= 0; i-- {
		if reply[i] != '{' {
			continue
		}
		var out struct {
			Meets   *bool           `json:"meets_definition"`
			Reason  *bool           `json:"reason_supported"`
			AgeGate *ageGateWitness `json:"age_gate_witness"`
		}
		dec := json.NewDecoder(strings.NewReader(reply[i:]))
		if err := dec.Decode(&out); err != nil || out.Meets == nil || out.Reason == nil {
			continue
		}
		return entailVerdict{Meets: *out.Meets, Reason: *out.Reason, AgeGate: out.AgeGate}, nil
	}
	return entailVerdict{}, errors.New("judge: no entailment verdict in the reply")
}

// entail holds every standing flag and ok_with_setting grade to its area's
// flag definition, one call per grade through the judge model (Entail),
// thinking off via Fast, metered like any other call (T3.10, fault 3).
// meets_definition false drops the grade to unknown (RuleNotEarned). An
// unsupported published claim also drops (RuleReasonUnsupported). Flags
// are checked using standingReason, independently of discarded generated
// prose. A grade that stands keeps standingReason at this grading step;
// parentReasons subsequently writes and verifies the card explanation.
// Generic entailment errors retain the grade. Age flags require an exact source
// event witness; without one they become unknown even if classification failed.
// Errors and measured costs remain visible. The input is not modified.
func (j *Judge) entail(ctx context.Context, title string, age int, ref string, grades []checklist.Grade, fs []finding.Finding, witnesses ...map[checklist.Area]ageGateWitness) (out []checklist.Grade, dem []checklist.Demotion, thin []checklist.Area, entailErr error, price cost.Price) {
	out = make([]checklist.Grade, len(grades))
	for i, g := range grades {
		g.Findings = slices.Clone(g.Findings)
		out[i] = g
		if (g.Level != checklist.Flag && g.Level != checklist.OKWithSetting) || !g.Area.Valid() || len(g.Findings) == 0 {
			continue
		}
		v, p, err := j.checkEntail(ctx, title, age, ref, g, fs)
		price += p
		// Without any verified event an age flag cannot stand, even when
		// one of the independent classifications failed. Keep the error
		// visible while treating the missing witness as thin evidence.
		if err != nil && g.Area == checklist.AgeGating && g.Level == checklist.Flag {
			if entailErr == nil {
				entailErr = fmt.Errorf("judge: entailment for %s: %w", g.Area, err)
			}
			err = nil
		}
		if err == nil {
			if g.Area == checklist.AgeGating && g.Level == checklist.Flag && v.Meets && !witnessedAgeGate(g, fs, v.AgeGate) {
				v.Meets = false
			}
			switch {
			case !v.Meets:
				dem = append(dem, checklist.Demotion{Area: g.Area, Rule: RuleNotEarned, Detail: string(g.Level) + ": " + string(g.Area)})
				thin = append(thin, g.Area)
				g.Level, g.Setting, g.Reason, g.Findings = checklist.Unknown, "", NotEarnedReason, []string{}
			case !v.Reason:
				dem = append(dem, checklist.Demotion{Area: g.Area, Rule: RuleReasonUnsupported, Detail: string(g.Level) + ": " + string(g.Area)})
				thin = append(thin, g.Area)
				g.Level, g.Setting, g.Reason, g.Findings = checklist.Unknown, "", UnsupportedReason, []string{}
			default:
				g.Reason = standingReason(g.Area)
				if len(witnesses) > 0 && witnesses[0] != nil && g.Area == checklist.AgeGating && g.Level == checklist.Flag && v.AgeGate != nil {
					witnesses[0][g.Area] = *v.AgeGate
				}
			}
			out[i] = g
			continue
		}
		if entailErr == nil {
			entailErr = fmt.Errorf("judge: entailment for %s: %w", g.Area, err)
		}
	}
	return out, dem, thin, entailErr, price
}

// checkEntail evaluates age-gating risks independently because its definition
// explicitly makes one credible first-hand failure sufficient. An unrelated
// citation must not dilute a source witness. Each request is cached and metered
// through the same path as other entailment checks; the first valid witness ends
// the search. Settings still require the combined risk and mitigation check.
func (j *Judge) checkEntail(ctx context.Context, title string, age int, ref string, g checklist.Grade, fs []finding.Finding) (entailVerdict, cost.Price, error) {
	check := func(one checklist.Grade, system string) (entailVerdict, cost.Price, error) {
		user := entailUser(title, age, one, fs)
		if one.Area == checklist.AgeGating && one.Level == checklist.Flag {
			var evidence strings.Builder
			fmt.Fprintf(&evidence, "Title: %s\nArea: age_gating\nCited source quotes:\n", title)
			for _, f := range fs {
				if slices.Contains(one.Findings, f.ID) {
					fmt.Fprintf(&evidence, "Source: %s\nQuote: %q\n", f.Source.Kind, f.Quote)
				}
			}
			user = evidence.String()
		}
		r, p, err := chat(ctx, j.Meter, j.Entail, "entail", ref, []model.Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		}, model.Options{JSON: true, Extra: j.Fast})
		if err != nil {
			return entailVerdict{}, p, err
		}
		if one.Area == checklist.AgeGating && one.Level == checklist.Flag {
			var witness ageGateWitness
			if err := json.Unmarshal([]byte(r.Text), &witness); err != nil {
				return entailVerdict{}, p, err
			}
			if witness.Criterion == "none" {
				return entailVerdict{}, p, nil
			}
			valid := witnessedAgeGate(one, fs, &witness)
			return entailVerdict{Meets: valid, Reason: valid, AgeGate: &witness}, p, nil
		}
		v, err := parseEntail(r.Text)
		return v, p, err
	}
	if g.Area != checklist.AgeGating || g.Level != checklist.Flag {
		return check(g, entailSystem())
	}
	var total cost.Price
	var firstErr error
	for _, id := range g.Findings {
		one := g
		one.Findings = []string{id}
		v, p, err := check(one, ageGateSystem())
		total += p
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if err == nil && v.Meets && v.Reason && witnessedAgeGate(one, fs, v.AgeGate) {
			return v, total, nil
		}
		if ctx.Err() != nil {
			return entailVerdict{}, total, ctx.Err()
		}
	}
	return entailVerdict{}, total, firstErr
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

// Parent prose has no authority over grades. Unknown areas make no factual
// claim about a title; checked sources remain available in the evidence view.
const parentUnknownReason = "We don’t have enough evidence to assess this area. Don’t assume it is safe."
const parentConcernFallback = "The cited sources raise a concern here. Read the source quotes before deciding."
const parentOKFallback = "Read the cited source quotes before deciding; we couldn’t verify a plain-language explanation for this area."

func parentReasonSystem() string {
	return `Write explanations for a parent deciding about one title for a child of the stated age. The grades and citations are already checked and fixed. You write reasons only; do not change grades, settings or citations.
Use one or two short sentences with concrete actions and familiar words. Describe what THIS title's cited sources report, not the area's general definition.
Begin with explicit source attribution. Prefer "A review reports" over "A parent reports" unless the quote identifies its author as a parent. Describe one decisive report rather than joining every citation. For an age-check concern, describe the concrete bypass or the reported actual and recorded ages when supplied; a conditional correction procedure is not itself an observed failure.
State the source report by itself. Do not append an interpretation such as "showing" or "indicating" the grade is earned. For a self-report, write "A reviewer who says they are [quoted age] reports..." rather than presenting the person’s age as independently verified.
Select evidence before writing. For age gating, use a concrete bypass or a quote with explicit actual and assigned ages. A group number without the child's actual age does not establish a wrong placement; two events joined by "plus" do not establish cause. Do not select that vague report when a precise age event is available. For an age mismatch, report only the stated actual and displayed ages; do not explain how a birthday field, scan, update or other mechanism caused it unless the quote explicitly states that cause.
For content flags, select a concrete encounter: say what a child or investigator actually saw or reached. A statement that content is possible, likely, or generally available is not an encounter and cannot explain this flag. Prefer a past-tense report of something that was shown or accessed, and retain that past-tense event in the explanation. Name who encountered it when the quote says so; a description of a game or its tutorial alone does not show anyone encountering it.
For money, describe the quoted purchase mechanism or completed child purchase, preserving qualifications such as "may" and "optional". Do not add pressure, lack of parental approval, or a particular purchase type unless the quote shows it.
For data flags, describe only the named information, recipient and retention limit. For a positive data grade supported by a compliance label, say the source claims that label and that this quote does not verify its practices; do not infer actual compliance.
For OK grades, keep only the positive description the quote supplies. Never list risks that it does not mention. A free-platform description needs no checklist of absent purchase types.
Attribute claims: say "A parent reports...", "A reviewer says...", "The privacy policy says...", "The store listing says...", or name the supplied source type. A report is not proof that every child has that experience; a policy promise is not proof that a control works.
Explain uncertainty directly. Say what the cited quotes do not establish for this child's age. Do not turn missing evidence into reassurance. Do not claim a control works unless the cited quotes show it.
Do not use internal terms such as "finding", "mitigation", "stance", "flag definition", "standing grade", or citation ids in the reason.
Name only the specific practice, harm, recipient, purchase, or control the cited quotes establish. Do not copy a definition's list of possible harms. Do not add motives, causes, event order, frequency, defaults, ages, or uses of data the quotes do not establish. In particular, do not turn a purchase and a scam report into a claim the purchase happened after the scam. A self-report by a reviewer who states their age can be described as that reviewer’s purchase.
For a flag, explain the concrete concern that meets the supplied area definition; a related product feature on its own does not explain why the area is a concern. The definition is a criterion, never evidence. Prefer one concise concrete report; do not combine many harms or add an explanation of causes.
For a setting grade, explain the reported concern and name the quoted control a parent should change, without promising it prevents a reported bypass. For OK, describe the limited positive evidence without calling the whole title safe or treating silence as reassurance.
Do not turn a compliance label, rating, certification or marketing promise into proof of actual compliance, safety or effectiveness. Attribute it as a claim and state that the quote alone does not verify it.
Use only the exact cited source quotes, not a reader claim or a grade definition as evidence. Return JSON {"reasons":[{"area":"...","reason":"..."}]} with one reason per supplied area.`
}
func parentReasonCheckSystem() string {
	return `You check the exact explanation a parent will see against only its cited source quotes. A fixed grade and an area definition are not evidence. The fixed grade has already been judged separately. Verify factual support and relevance of this explanation, not whether the source earns that grade again. The explanation must describe concrete source evidence relevant to the supplied area focus, rather than an unrelated or merely adjacent feature. Do not demand an extra detail the explanation does not claim, and do not approve unsupported text just because the grade is fixed. Answer reason_supported true only when every concrete claim in the reason is supported by a cited quote and attribution matches the supplied source type. Do not grant an allowance for text copied from a grade definition.
Check every practice, harm, recipient, purchase, control, age, number, default, frequency, cause and use of data separately. One unsupported item makes the reason false. A broad category does not support extra examples. A source claiming a policy or setting does not prove it works in practice. A report about one child does not prove all children experience it. Silence about a risk does not establish safety or absence of enforcement. An age recommendation or educational level does not prove an age gate works. A first-hand claim is supported when carefully attributed as that source's report, without presenting it as independently established fact.
An age failure must name a shown bypass or explicit actual/assigned-age mismatch. A group number alone cannot prove the group is wrong: reject "wrong group" when the quote gives neither actual age nor group meaning. Reject a cause inferred merely from two statements joined by "plus" or "and". A quote reporting a birthday and a displayed age does not by itself show the mechanism that caused a mismatch; reject an added claim that the field accepted it or made the system calculate it.
For content flags, reject generalized possibility or likelihood even if quoted accurately: the explanation must describe a concrete encounter with unsuitable material, supported by a cited report of what was seen or reached. Describing a game or its tutorial does not establish an encounter unless the source reports someone actually encountering it; the explanation should retain that reported encounter.
For money, a qualified source warning about a named purchase mechanism (such as optional loot boxes) is a concrete concern; preserve its qualifications rather than requiring an unshown completed purchase. A report of a completed child purchase supports an attributed account of that purchase. For this explanation, that concrete purchase is sufficient: do not require details of purchase method or parental oversight the explanation does not claim. It does not by itself prove extra pressure, lack of permission, or unrelated purchase types. Reject invented temporal order, including saying a purchase happened after a scam when the quote only reports both.
For an OK grade, a source's positive description may be explained with explicit limits. A store's compliance claim can be attributed as a label with a clear statement that the quote does not verify actual practices; that is not a claim of actual compliance. Reject lists of risks a source does not mention: retain the positive description only.
The explanation must begin with explicit source attribution, use plain parent-facing language, attribute factual claims, and explain limits without reassurance based on missing evidence. A review is not necessarily a parent: do not invent the author’s role. Attribution to the supplied source kind is sufficient; never require an unknown speaker identity. A reviewer’s stated own age is supported when described as a self-report, and ordinary paraphrase of that self-report does not invent a second child. The reported person’s age need not match the child being assessed: accurately attributing a report about someone else is not a claim that this child had that experience. Do not require independent verification of an attributed self-report. A compliance label, rating, certification or marketing promise does not demonstrate actual compliance, safety or effectiveness. Reject an inference of adherence from the mere presence of a label. Reject an unqualified factual opening even if later parts mention a source. Reject internal terms such as finding, mitigation, stance, flag definition, standing grade or citation ids. A statement that the evidence is insufficient is acceptable; a concrete claim added to that statement still needs its own quote.
When rejecting, identify the exact unsupported words or missing decisive detail, and explain which quote fails to support them. Return JSON {"reason_supported":true,"explanation":"why each claim is supported"} or {"reason_supported":false,"explanation":"exact unsupported claim or missing decisive detail"}. Do not change or re-evaluate the grade.`
}

// These are writing focuses, not grading definitions. The calibrated areaDefs
// and checks above remain the sole grading criteria. Repeating those thresholds
// here caused the prose checker to demand practices the prose did not claim.
func parentReasonFocus(area checklist.Area) string {
	switch area {
	case checklist.Contact:
		return "a quoted stranger message, chat or voice channel and the quoted control or bypass; a report of harm without a communication channel does not explain this concern"
	case checklist.Money:
		return "a quoted loot box, pay-to-win or subscription mechanism, or a completed child spending event; preserve qualifications and do not infer permission or pressure"
	case checklist.Data:
		return "for a concern: a quoted third party handling children’s information, ad tracking, or stated retention/deletion gap. Access to user content alone is adjacent evidence. For OK: attribute the specific positive source description or compliance label with its limits, without claiming verified compliance"
	case checklist.AgeGating:
		return "a reported bypass or explicit actual/assigned-age mismatch; do not infer a wrong age from an undefined group or invent a cause"
	case checklist.Content:
		return "a concrete reported encounter with unsuitable material, not a generic possibility or description of available content"
	case checklist.DeveloperRecord:
		return "the specific reported enforcement, lawsuit, breach or other developer event; distinguish allegations from findings"
	}
	return "only the concrete source evidence for this area"
}

func parentReasonUser(title string, age int, grades []checklist.Grade, fs []finding.Finding, witnesses ...map[checklist.Area]ageGateWitness) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\nChild's age: %d\n", title, age)
	for _, g := range grades {
		if len(witnesses) > 0 {
			if w, ok := witnesses[0][g.Area]; ok {
				fmt.Fprintf(&b, "\nRequired age-event focus (verbatim source quote): %q\nActual age reported: %s\nAssigned age reported: %s\nWhen both ages are supplied, describe only those reported actual and assigned ages. Do not mention birthday dates, support conversations, or mechanisms behind the mismatch. For a bypass, describe only the quoted bypass.\n", w.Quote, w.ActualAge, w.AssignedAge)
			}
		}
		fmt.Fprintf(&b, "\nArea: %s\nLevel: %s\nSetting: %s\nExplanation focus (grade fixed; not regraded): %s\n", g.Area, g.Level, g.Setting, parentReasonFocus(g.Area))
		for _, f := range fs {
			if slices.Contains(g.Findings, f.ID) {
				fmt.Fprintf(&b, "Source: %s\nQuote: %q\n", f.Source.Kind, f.Quote)
			}
		}
	}
	if len(grades) == 1 && grades[0].Area == checklist.Content && grades[0].Level == checklist.Flag {
		b.WriteString("\nContent evidence selection: choose a report of something someone actually saw, was shown, or accessed. Do not select a generic statement about what a game contains or encourages. Preserve the reported event in past tense.\n")
	}
	return b.String()
}
func (j *Judge) parentReasons(ctx context.Context, title string, age int, ref string, grades []checklist.Grade, fs []finding.Finding, witnesses ...map[checklist.Area]ageGateWitness) ([]checklist.Grade, cost.Price) {
	out := slices.Clone(grades)
	var writable []checklist.Grade
	for i, g := range out {
		if g.Level == checklist.Unknown {
			out[i].Reason = parentUnknownReason
			continue
		}
		if g.Level == checklist.OK {
			out[i].Reason = parentOKFallback
		} else {
			out[i].Reason = parentConcernFallback
		}
		if len(g.Findings) > 0 {
			writable = append(writable, g)
		}
	}
	if len(writable) == 0 {
		return out, 0
	}
	r, price, err := chat(ctx, j.Meter, j.Model, "reason", ref, []model.Message{{Role: "system", Content: parentReasonSystem()}, {Role: "user", Content: parentReasonUser(title, age, writable, fs, witnesses...)}}, model.Options{JSON: true, Extra: j.Fast, MaxTokens: 2048})
	if err != nil {
		return out, price
	}
	var parsed struct {
		Reasons []struct {
			Area   checklist.Area `json:"area"`
			Reason string         `json:"reason"`
		} `json:"reasons"`
	}
	if json.Unmarshal([]byte(r.Text), &parsed) != nil {
		return out, price
	}
	candidates := map[checklist.Area]string{}
	counts := map[checklist.Area]int{}
	for _, v := range parsed.Reasons {
		counts[v.Area]++
		candidates[v.Area] = strings.TrimSpace(v.Reason)
	}
	for i, g := range out {
		if g.Level == checklist.Unknown || counts[g.Area] != 1 || candidates[g.Area] == "" || len(g.Findings) == 0 {
			continue
		}
		candidate := g
		candidate.Reason = candidates[g.Area]
		accepted, feedback, p := j.checkParentReason(ctx, title, age, ref, candidate, fs)
		price += p
		if accepted {
			out[i].Reason = candidate.Reason
			continue
		}
		// One bounded repair can make the reason narrower, but has no
		// authority to rescue or change the grade. The repaired prose must
		// pass the same independent support check or the fallback remains.
		r, p, err := chat(ctx, j.Meter, j.Model, "reason_repair", ref, []model.Message{
			{Role: "system", Content: parentReasonSystem()},
			{Role: "user", Content: parentReasonUser(title, age, []checklist.Grade{g}, fs, witnesses...) + "\nThe previous explanation failed verification: " + candidate.Reason + "\nChecker feedback: " + feedback + "\nIf the selected quote cannot show the decisive event, select a different cited quote that does. Write a narrower explanation from one concrete source quote. Attribute what that quote reports, omit inferences and do not infer failure from a hypothetical procedure. Return the same reasons JSON format."},
		}, model.Options{JSON: true, Extra: j.Fast, MaxTokens: 1024})
		price += p
		if err != nil {
			continue
		}
		var repair struct {
			Reasons []struct {
				Area   checklist.Area `json:"area"`
				Reason string         `json:"reason"`
			} `json:"reasons"`
		}
		if json.Unmarshal([]byte(r.Text), &repair) != nil || len(repair.Reasons) != 1 || repair.Reasons[0].Area != g.Area || strings.TrimSpace(repair.Reasons[0].Reason) == "" {
			continue
		}
		candidate.Reason = strings.TrimSpace(repair.Reasons[0].Reason)
		accepted, _, p = j.checkParentReason(ctx, title, age, ref, candidate, fs)
		price += p
		if accepted {
			out[i].Reason = candidate.Reason
		}
	}
	return out, price
}

const parentPurchaseQualification = "Preserve the difference between attempts or intentions and implemented controls: an attempt to limit spending does not establish that purchase limits were in place, worked, or were bypassed. Reject prose that drops that qualification."

func (j *Judge) checkParentReason(ctx context.Context, title string, age int, ref string, g checklist.Grade, fs []finding.Finding) (bool, string, cost.Price) {
	user := parentReasonUser(title, age, []checklist.Grade{g}, fs) + "\nReason: " + g.Reason
	if g.Area == checklist.Money {
		user += "\n" + parentPurchaseQualification
	}
	r, p, err := chat(ctx, j.Meter, j.Entail, "reason_check", ref, []model.Message{{Role: "system", Content: parentReasonCheckSystem()}, {Role: "user", Content: user}}, model.Options{JSON: true, Extra: j.Fast, MaxTokens: 1024})
	if err != nil {
		return false, "The support check did not complete.", p
	}
	var verdict struct {
		Supported   *bool  `json:"reason_supported"`
		Explanation string `json:"explanation"`
	}
	return json.Unmarshal([]byte(r.Text), &verdict) == nil && verdict.Supported != nil && *verdict.Supported, verdict.Explanation, p
}
