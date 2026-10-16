package pipeline

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/finding"
)

// The rule gate. It runs last in the judge step, after the judge, the
// citation and grounding checks, entailment, the age-gate witness and the
// parent-reason writer, and immediately before checklist.Decide. It calls no
// model. It reads the verified findings and moves a grade only on what the
// code can see in their quotes:
//
//   - A flag stands only if a cited risk quote says what that area's flag
//     needs. If none does, the flag is vetoed to unknown (the area is thin).
//   - A flag in money, contact or age gating is capped at ok_with_setting when
//     a quote in the area names a working control for it and no cited risk
//     quote reports that control failing.
//   - An unknown area is raised to ok when a verified fact settles it: the App
//     Store lists the title as free with no in-app purchases (money), its
//     privacy label shows no data linked to the user (data), its age rating is
//     at or below the child's age and a source says the content suits children
//     (content), the developer's own listing says there is no chat (contact),
//     or a source states the developer has no enforcement record.
//   - A developer-record ok stands only on such an explicit statement.
//
// The gate never invents a grade without a citation. Every grade it confirms,
// raises or caps cites at least one verified finding of that area that
// satisfies the rule, and a veto cites none. Every action is recorded in a
// GateAction so the card can say what the code did and why.
//
// What counts as evidence about the title. A risk quote counts only if it is
// about the title: a review, the store listing, the title's own policy or
// terms, or a source whose text or URL names the title or its developer. A
// news story about another company, a Common Sense page for a different app,
// or a list of apps in general is not counted. Where the title or developer is
// not known well enough to tell (a name under three letters), nothing is
// excluded on this ground.

// Gate rule ids. The first part is the area. "veto" rules send a flag to
// unknown, "capped" rules lower a flag to ok_with_setting, "settles" rules
// raise an unknown to ok, and "confirmed" rules record that a grade met its
// rule and stands.
const (
	RuleMoneyNoPurchase     = "money.flag_without_purchase"
	RuleMoneyCapped         = "money.flag_capped_by_control"
	RuleMoneyConfirmed      = "money.flag_confirmed"
	RuleMoneyFree           = "money.free_no_in_app_purchases"
	RuleDataNoBasis         = "data.flag_without_basis"
	RuleDataConfirmed       = "data.flag_confirmed"
	RuleDataNotLinked       = "data.label_not_linked"
	RuleContentNoHarm       = "content.flag_without_harm"
	RuleContentConfirmed    = "content.flag_confirmed"
	RuleContentRated        = "content.rated_for_age"
	RuleDeveloperNoAction   = "developer_record.flag_without_action"
	RuleDeveloperConfirmed  = "developer_record.flag_confirmed"
	RuleDeveloperNoStmt     = "developer_record.ok_without_statement"
	RuleDeveloperClean      = "developer_record.stated_clean"
	RuleContactNoStranger   = "contact.flag_without_stranger_contact"
	RuleContactCapped       = "contact.flag_capped_by_control"
	RuleContactConfirmed    = "contact.flag_confirmed"
	RuleContactNoChat       = "contact.no_chat_stated"
	RuleAgeGateNoBypass     = "age_gating.flag_without_bypass"
	RuleAgeGateCapped       = "age_gating.flag_capped_by_control"
	RuleAgeGateConfirmed    = "age_gating.flag_confirmed"
	ruleConfirmedNotePrefix = "A quote behind this grade describes "
)

// GateAction is one thing the rule gate did to a grade. The card carries the
// list, in the order the actions happened, so a parent can see which grades
// the code checked and which it changed. From equal to To is a confirmation.
type GateAction struct {
	Area checklist.Area  `json:"area"`
	Rule string          `json:"rule"`
	From checklist.Level `json:"from"`
	To   checklist.Level `json:"to"`
	// Findings are the ids of the verified findings the action rests on:
	// the quotes that satisfied the rule. A veto cites none.
	Findings []string `json:"findings"`
	// Note says what the rule found, in plain words for the card.
	Note string `json:"note"`
}

// Option configures ApplyRules and Judge.Judge.
type Option func(*gateOptions)

type gateOptions struct {
	names   []string
	witness []string
}

// WithDeveloper names the developer, the store listing's name or a parent
// company, so the gate can tell a quote about the title from one about
// somebody else. The title is always one of the names.
func WithDeveloper(names ...string) Option {
	return func(o *gateOptions) { o.names = append(o.names, names...) }
}

// withAgeWitness passes the age-gate witness quotes the entailment step
// verified. A source quote that carries one counts as a bypass or wrong-band
// report whatever its wording, because the witness check (witnessedAgeGate) is
// the older, stricter rule and the gate keeps it.
func withAgeWitness(quote string) Option {
	return func(o *gateOptions) {
		if strings.TrimSpace(quote) != "" {
			o.witness = append(o.witness, quote)
		}
	}
}

// ApplyRules runs the rule gate over a complete grade set. age is the child's
// age, names are further names the title goes by (see WithDeveloper), and fs
// are all the verified findings. It returns a new grade set in the input's
// order and the actions it took. The input is not modified.
func ApplyRules(title string, age int, grades []checklist.Grade, fs []finding.Finding, opts ...Option) ([]checklist.Grade, []GateAction) {
	var o gateOptions
	for _, f := range opts {
		f(&o)
	}
	g := newGate(age, append([]string{title}, o.names...), fs)
	g.witness = o.witness
	out := make([]checklist.Grade, len(grades))
	var acts []GateAction
	for i, in := range grades {
		in.Findings = slices.Clone(in.Findings)
		out[i], acts = g.area(in, acts)
	}
	return out, acts
}

// gate is one run's view of the evidence.
type gate struct {
	age    int
	names  [][]string // each name as normalised words
	squash []string   // each name with every non-alphanumeric removed, for URLs
	fs     []finding.Finding
	words  map[string]words
	// witness holds the age-gate witness quotes entailment verified.
	witness []string
}

func newGate(age int, names []string, fs []finding.Finding) *gate {
	g := &gate{age: age, fs: fs, words: make(map[string]words, len(fs))}
	seen := map[string]bool{}
	for _, raw := range names {
		for _, n := range nameVariants(raw) {
			w := gateWords(n)
			sq := strings.Join(w, "")
			if len(sq) < 3 || seen[sq] {
				continue
			}
			seen[sq] = true
			g.names = append(g.names, w)
			g.squash = append(g.squash, sq)
		}
	}
	return g
}

var corporateSuffix = map[string]bool{
	"inc": true, "llc": true, "ltd": true, "limited": true, "corp": true, "corporation": true,
	"co": true, "company": true, "gmbh": true, "ab": true, "sa": true, "plc": true, "ag": true, "lp": true,
}

// nameVariants gives the forms of a name a quote might use: the whole name,
// the name before a store subtitle ("Minecraft: Play with Friends"), and the
// name without a corporate suffix ("Khan Academy, Inc." and "Khan Academy").
func nameVariants(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := []string{raw}
	head := raw
	for _, cut := range []string{":", " - ", " – ", " — ", "|"} {
		if i := strings.Index(head, cut); i > 0 {
			head = head[:i]
		}
	}
	w := gateWords(head)
	for len(w) > 1 && corporateSuffix[w[len(w)-1]] {
		w = w[:len(w)-1]
	}
	if len(w) > 1 && w[0] == "the" {
		w = w[1:]
	}
	if s := strings.Join(w, " "); s != "" && s != strings.ToLower(raw) {
		out = append(out, s)
	}
	return out
}

func (g *gate) w(f finding.Finding) words {
	if w, ok := g.words[f.ID]; ok {
		return w
	}
	w := gateWords(f.Quote)
	g.words[f.ID] = w
	return w
}

// named reports whether the words carry one of the title's names.
func (g *gate) named(w words) bool {
	for _, n := range g.names {
		for i := range w {
			if matchAt(w, i, n) {
				return true
			}
		}
	}
	return false
}

// onSubject reports whether a finding is evidence about the title and not
// about somebody else. See the file comment.
func (g *gate) onSubject(f finding.Finding) bool {
	switch f.Source.Kind {
	case KindReview, KindStore, KindPolicy, KindTerms:
		return true
	}
	if len(g.names) == 0 || g.named(g.w(f)) {
		return true
	}
	u := strings.Join(gateWords(f.Source.URL), "")
	for _, sq := range g.squash {
		if strings.Contains(u, sq) {
			return true
		}
	}
	return false
}

// firstParty reports whether a finding is the developer's own words: the
// store listing, its policy or terms, or a page on the developer's own site.
func (g *gate) firstParty(f finding.Finding) bool {
	switch f.Source.Kind {
	case KindStore, KindPolicy, KindTerms:
		return true
	case KindReview:
		return false
	}
	host := strings.Join(gateWords(hostOf(f.Source.URL)), "")
	for _, sq := range g.squash {
		if strings.Contains(host, sq) {
			return true
		}
	}
	return false
}

// riskIn returns the risk findings of area a that are about the title and
// that satisfy ok.
func (g *gate) riskIn(a checklist.Area, ok func(finding.Finding) bool) []finding.Finding {
	var out []finding.Finding
	for _, f := range g.fs {
		if f.Area == a && f.Stance == finding.Risk && g.onSubject(f) && ok(f) {
			out = append(out, f)
		}
	}
	return out
}

func ids(fs []finding.Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.ID)
	}
	return out
}

// ---------------------------------------------------------------- per-area spec

// areaSpec is what the gate needs to know about one area.
type areaSpec struct {
	// risk: a risk quote that can support a flag in this area.
	risk func(g *gate, f finding.Finding) bool
	// vetoRule and vetoNote: the flag has no such quote.
	vetoRule, vetoNote string
	// confirmRule and confirmWhat: the flag has one. confirmWhat completes
	// "A quote behind this grade describes ...".
	confirmRule, confirmWhat string
	// controls and capRule: a quote in the area names a control that answers
	// the flag. Nil controls means the area has no cap.
	controls []control
	capRule  string
	// settle tries to settle an unknown area to ok. Nil means none.
	settle func(g *gate, cur checklist.Grade) *settlement
}

// settlement is a rule that raises an area to ok.
type settlement struct {
	rule     string
	findings []finding.Finding
	note     string
}

func hitsAny(w words, terms []string, neg bool) bool { return pattern{any: terms}.matches(w, neg) }

func hitsVocab(w words, v []pattern, neg bool) bool {
	for _, p := range v {
		if p.matches(w, neg) {
			return true
		}
	}
	return false
}

var specs map[checklist.Area]areaSpec

// specs is set in init: the settle rules read it, so a package-level
// initialiser would be an initialisation cycle.
func init() {
	specs = map[checklist.Area]areaSpec{
		checklist.Money: {
			risk:     func(g *gate, f finding.Finding) bool { return hitsVocab(g.w(f), moneyRisk, true) },
			vetoRule: RuleMoneyNoPurchase, vetoNote: "None of the quotes behind this concern describes a purchase, a subscription or an ad.",
			confirmRule: RuleMoneyConfirmed, confirmWhat: "a purchase, a subscription or an ad.",
			controls: moneyControls, capRule: RuleMoneyCapped,
			settle: settleMoney,
		},
		checklist.Data: {
			risk:     func(g *gate, f finding.Finding) bool { return hitsVocab(g.w(f), dataRisk, true) },
			vetoRule: RuleDataNoBasis, vetoNote: "None of the quotes behind this concern describes data being sold, tracked for ads, shared for marketing or breached.",
			confirmRule: RuleDataConfirmed, confirmWhat: "data being sold, tracked for ads, shared for marketing, breached or collected without consent.",
			settle: settleData,
		},
		checklist.Content: {
			risk:     func(g *gate, f finding.Finding) bool { return hitsVocab(g.w(f), contentHarm, true) },
			vetoRule: RuleContentNoHarm, vetoNote: "None of the quotes behind this concern describes violence, sexual content, self-harm, predators, gambling, drugs or frightening material. A parent's objection to a belief or a value is not counted.",
			confirmRule: RuleContentConfirmed, confirmWhat: "violence, sexual content, self-harm, predators, gambling, drugs or frightening material.",
			settle: settleContent,
		},
		checklist.DeveloperRecord: {
			risk: func(g *gate, f finding.Finding) bool {
				return (f.Source.Kind == KindNews || f.Source.Kind == KindArticle) &&
					g.named(g.w(f)) && hitsVocab(g.w(f), legalAction, true)
			},
			vetoRule: RuleDeveloperNoAction, vetoNote: "None of the quotes behind this concern reports a fine, lawsuit, breach or regulator action against the developer.",
			confirmRule: RuleDeveloperConfirmed, confirmWhat: "a fine, lawsuit, breach or regulator action against the developer.",
			settle: settleDeveloper,
		},
		checklist.Contact: {
			risk:     func(g *gate, f finding.Finding) bool { return hitsVocab(g.w(f), contactRisk, true) },
			vetoRule: RuleContactNoStranger, vetoNote: "None of the quotes behind this concern shows strangers being able to message, chat with or reach a child.",
			confirmRule: RuleContactConfirmed, confirmWhat: "strangers being able to message, chat with or reach a child.",
			controls: contactControls, capRule: RuleContactCapped,
			settle: settleContact,
		},
		checklist.AgeGating: {
			risk: func(g *gate, f finding.Finding) bool {
				return hitsVocab(g.w(f), ageGateBypass, false) || slices.ContainsFunc(g.witness, func(q string) bool { return strings.Contains(f.Quote, q) })
			},
			vetoRule: RuleAgeGateNoBypass, vetoNote: "None of the quotes behind this concern reports the age check being bypassed or a child being placed in the wrong age group.",
			confirmRule: RuleAgeGateConfirmed, confirmWhat: "the age check being bypassed or a child being placed in the wrong age group.",
			controls: ageGateControls, capRule: RuleAgeGateCapped,
		},
	}
}

// area applies the rules to one grade and appends what it did to acts.
func (g *gate) area(in checklist.Grade, acts []GateAction) (checklist.Grade, []GateAction) {
	spec, ok := specs[in.Area]
	if !ok {
		return in, acts
	}
	out := in
	switch in.Level {
	case checklist.Flag:
		var support []finding.Finding
		for _, id := range in.Findings {
			if f, ok := g.find(id); ok && f.Area == in.Area && f.Stance == finding.Risk && g.onSubject(f) && spec.risk(g, f) {
				support = append(support, f)
			}
		}
		if len(support) > 0 {
			if c, label, ctl := g.cap(in, spec, support); ctl != nil {
				note := fmt.Sprintf("A source names a control that answers this concern (%s), and no cited quote says the control fails.", label)
				acts = append(acts, GateAction{Area: in.Area, Rule: spec.capRule, From: checklist.Flag, To: checklist.OKWithSetting, Findings: ids(append(slices.Clone(support), ctl...)), Note: note})
				c.Reason = note
				return c, acts
			}
			acts = append(acts, GateAction{Area: in.Area, Rule: spec.confirmRule, From: checklist.Flag, To: checklist.Flag, Findings: ids(support), Note: ruleConfirmedNotePrefix + spec.confirmWhat})
			return out, acts
		}
		acts = append(acts, GateAction{Area: in.Area, Rule: spec.vetoRule, From: checklist.Flag, To: checklist.Unknown, Findings: []string{}, Note: spec.vetoNote})
		out = unknownGrade(in)
	case checklist.OK:
		if in.Area == checklist.DeveloperRecord {
			if st := settleDeveloper(g, in); st != nil && g.cites(in, st.findings) {
				acts = append(acts, GateAction{Area: in.Area, Rule: st.rule, From: checklist.OK, To: checklist.OK, Findings: ids(st.findings), Note: st.note})
				return out, acts
			}
			acts = append(acts, GateAction{Area: in.Area, Rule: RuleDeveloperNoStmt, From: checklist.OK, To: checklist.Unknown, Findings: []string{}, Note: "No source states that the developer has no fines, lawsuits or breaches. Having no news is not evidence."})
			return unknownGrade(in), acts
		}
		if spec.settle != nil {
			if st := spec.settle(g, in); st != nil && g.cites(in, st.findings) {
				acts = append(acts, GateAction{Area: in.Area, Rule: st.rule, From: checklist.OK, To: checklist.OK, Findings: ids(st.findings), Note: st.note})
			}
		}
		return out, acts
	case checklist.OKWithSetting:
		return out, acts
	}
	// out is unknown: the judge left it so, or a veto just did.
	if out.Level == checklist.Unknown && spec.settle != nil {
		if st := spec.settle(g, out); st != nil {
			acts = append(acts, GateAction{Area: in.Area, Rule: st.rule, From: checklist.Unknown, To: checklist.OK, Findings: ids(st.findings), Note: st.note})
			out.Level, out.Setting, out.Findings, out.Reason = checklist.OK, "", ids(st.findings), st.note
		}
	}
	return out, acts
}

func (g *gate) find(id string) (finding.Finding, bool) {
	for _, f := range g.fs {
		if f.ID == id {
			return f, true
		}
	}
	return finding.Finding{}, false
}

// cites reports whether the grade cites at least one of fs.
func (g *gate) cites(in checklist.Grade, fs []finding.Finding) bool {
	for _, f := range fs {
		if slices.Contains(in.Findings, f.ID) {
			return true
		}
	}
	return false
}

func unknownGrade(in checklist.Grade) checklist.Grade {
	in.Level, in.Setting, in.Findings, in.Reason = checklist.Unknown, "", []string{}, parentUnknownReason
	return in
}

// cap lowers a flag to ok_with_setting when a quote in the area names a
// control that answers it. A flag cites risk quotes only (checkCitations), so
// the control is looked for in the whole area, not in the grade's citations.
// The control quote must be a mitigation or neutral finding (a risk-stance
// quote that mentions a control may be describing its absence), must be about
// the title, must not itself report a failure, and no risk quote may report
// the control failing: not a cited one, and not an uncited one that speaks of
// the area's harm. A cap relaxes a flag, so every test errs toward leaving
// the flag. The returned grade cites the supporting risks and the control
// quotes; ctl is nil when there is no cap.
func (g *gate) cap(in checklist.Grade, spec areaSpec, support []finding.Finding) (checklist.Grade, string, []finding.Finding) {
	if len(spec.controls) == 0 {
		return in, "", nil
	}
	for _, id := range in.Findings {
		if f, ok := g.find(id); ok && f.Stance == finding.Risk && hitsAny(g.w(f), controlFailure, false) {
			return in, "", nil
		}
	}
	// A report that the control fails also counts when the flag does not cite
	// it, if it is a risk quote about the title that speaks of this area's
	// harm: the judge cites few quotes, and a flag that leaves out the failing
	// control must not be lowered on that account.
	if len(g.riskIn(in.Area, func(f finding.Finding) bool { return spec.risk(g, f) && hitsAny(g.w(f), controlFailure, false) })) > 0 {
		return in, "", nil
	}
	for _, c := range spec.controls {
		var ctl []finding.Finding
		for _, f := range g.fs {
			if f.Area == in.Area && f.Stance != finding.Risk && g.onSubject(f) && c.pat.matches(g.w(f), true) && !hitsAny(g.w(f), controlFailure, false) {
				ctl = append(ctl, f)
			}
		}
		if len(ctl) == 0 {
			continue
		}
		if len(ctl) > 2 {
			ctl = ctl[:2]
		}
		out := in
		out.Level, out.Setting = checklist.OKWithSetting, c.label
		out.Findings = ids(append(slices.Clone(support), ctl...))
		return out, c.label, ctl
	}
	return in, "", nil
}

// ---------------------------------------------------------------- settle rules

// noRisk reports whether no risk finding about the title in area a satisfies
// the area's qualifying test. A settle rule needs this: a fact does not
// outweigh a quote that says the opposite.
func (g *gate) noRisk(a checklist.Area) bool {
	return len(g.riskIn(a, func(f finding.Finding) bool { return specs[a].risk(g, f) })) == 0
}

func settleMoney(g *gate, _ checklist.Grade) *settlement {
	for _, f := range g.fs {
		if f.Area != checklist.Money || f.Stance != finding.Mitigation || f.Source.Kind != KindStore {
			continue
		}
		w := g.w(f)
		if len(w) == 0 || hitsAny(w, []string{"in app purchas*"}, false) {
			continue
		}
		last := gateWords(lastLine(f.Quote))
		if (len(last) > 0 && last[0] == "free") || w[len(w)-1] == "free" {
			if g.noRisk(checklist.Money) {
				return &settlement{RuleMoneyFree, []finding.Finding{f}, "The App Store lists this app as free with no in-app purchases, and no checked quote reports purchases or ads."}
			}
			return nil
		}
	}
	return nil
}

func settleData(g *gate, _ checklist.Grade) *settlement {
	for _, f := range g.fs {
		if f.Area == checklist.Data && f.Stance == finding.Mitigation && f.Source.Kind == KindStore && hitsAny(g.w(f), dataLabelNotLinked, false) {
			if g.noRisk(checklist.Data) {
				return &settlement{RuleDataNotLinked, []finding.Finding{f}, "The App Store privacy label says the developer reports no data linked to the user and none used to track them, and no checked quote reports data being sold, tracked for ads or shared for marketing."}
			}
			return nil
		}
	}
	return nil
}

var storeRating = regexp.MustCompile(`(?i)age rating:\s*(\d+)\+`)

// settleContent settles content to ok on a store rating at or below the
// child's age, with a second, independent witness. A rating is the
// developer's own questionnaire answer, so on its own it is a claim; it counts
// when no quote about the title reports unsuitable material and a source other
// than the store listing says the content suits children.
func settleContent(g *gate, _ checklist.Grade) *settlement {
	var rating *finding.Finding
	var n int
	for i, f := range g.fs {
		if f.Area != checklist.Content || f.Source.Kind != KindStore {
			continue
		}
		if m := storeRating.FindStringSubmatch(f.Quote); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil {
				rating, n = &g.fs[i], v
				break
			}
		}
	}
	if rating == nil || n > g.age || !g.noRisk(checklist.Content) {
		return nil
	}
	var benign []finding.Finding
	for _, f := range g.fs {
		if f.Area == checklist.Content && f.Source.Kind != KindStore && f.Stance != finding.Risk && g.onSubject(f) && hitsVocab(g.w(f), contentBenign, true) {
			benign = append(benign, f)
		}
	}
	if len(benign) == 0 {
		return nil
	}
	if len(benign) > 2 {
		benign = benign[:2]
	}
	return &settlement{RuleContentRated, append([]finding.Finding{*rating}, benign...),
		fmt.Sprintf("The App Store rates this app %d+, at or below the child's age, a source says the content suits children, and no checked quote reports unsuitable material.", n)}
}

func settleContact(g *gate, _ checklist.Grade) *settlement {
	for _, f := range g.fs {
		if f.Area == checklist.Contact && f.Stance != finding.Risk && g.firstParty(f) && hitsVocab(g.w(f), contactNoChat, false) {
			if g.noRisk(checklist.Contact) {
				return &settlement{RuleContactNoChat, []finding.Finding{f}, "The developer's own listing or policy says there is no chat or messaging, and no checked quote shows strangers reaching a child."}
			}
			return nil
		}
	}
	return nil
}

func settleDeveloper(g *gate, _ checklist.Grade) *settlement {
	for _, f := range g.fs {
		if f.Area == checklist.DeveloperRecord && f.Stance != finding.Risk && f.Source.Kind != KindReview && g.named(g.w(f)) && hitsVocab(g.w(f), developerClean, false) {
			if g.noRisk(checklist.DeveloperRecord) {
				return &settlement{RuleDeveloperClean, []finding.Finding{f}, "A source says the developer has no known fines, lawsuits or breaches, and no checked quote reports one."}
			}
			return nil
		}
	}
	return nil
}

// ---------------------------------------------------------------- matching

// words is a text as lower-case words of letters and digits.
type words []string

// gateWords normalises text the way finding.Normalize does, then drops
// apostrophes and splits on everything that is not a letter or a digit, so
// "Can't", "can’t" and "cant" are one word and "In‑App" is two.
func gateWords(s string) words {
	s = finding.Normalize(s)
	s = strings.NewReplacer("'", "").Replace(s)
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

var termCache sync.Map // string -> []string

// parseTerm splits an entry into words, keeping a trailing "*".
func parseTerm(t string) []string {
	if v, ok := termCache.Load(t); ok {
		return v.([]string)
	}
	var out []string
	for _, f := range strings.Fields(strings.ToLower(strings.NewReplacer("'", "", "-", " ").Replace(t))) {
		out = append(out, f)
	}
	termCache.Store(t, out)
	return out
}

// matchAt reports whether term (see parseTerm) matches w starting at i.
func matchAt(w words, i int, term []string) bool {
	if len(term) == 0 || i+len(term) > len(w) {
		return false
	}
	for k, t := range term {
		switch p, star := strings.CutSuffix(t, "*"); {
		case t == "#": // any number
			if !isNumber(w[i+k]) {
				return false
			}
		case star:
			if !strings.HasPrefix(w[i+k], p) {
				return false
			}
		case w[i+k] != t:
			return false
		}
	}
	return true
}

func isNumber(w string) bool {
	for _, r := range w {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return w != ""
}

var (
	negators = map[string]bool{
		"no": true, "not": true, "without": true, "never": true, "zero": true, "nor": true, "none": true,
		"nothing": true, "nobody": true, "dont": true, "doesnt": true, "didnt": true, "cant": true,
		"cannot": true, "wont": true, "isnt": true, "arent": true, "lacks": true,
	}
	// fillers sit between a negator and the word it negates: "no in app
	// purchases", "without any real money".
	fillers = map[string]bool{
		"in": true, "app": true, "any": true, "the": true, "a": true, "an": true, "real": true,
		"extra": true, "more": true, "paid": true, "additional": true, "other": true, "hidden": true,
	}
)

// negated reports whether the hit at w[i:i+n] is cancelled: a negator before
// it with only fillers between ("no ads", "without in app purchases"), "free
// from" or "free of" before it, or "free" right after it ("ad free").
func negated(w words, i, n int) bool {
	if i+n < len(w) && w[i+n] == "free" {
		return true
	}
	for j := i - 1; j >= 0 && i-j <= 4; j-- {
		switch {
		case (w[j] == "from" || w[j] == "of") && j > 0 && w[j-1] == "free":
			return true
		case negators[w[j]]:
			return true
		case fillers[w[j]]:
			continue
		}
		return false
	}
	return false
}

// termHit reports whether any phrase of terms occurs in w. With neg, an occurrence
// that is negated does not count.
func termHit(w words, terms []string, neg bool) bool {
	for _, t := range terms {
		pt := parseTerm(t)
		for i := range w {
			if matchAt(w, i, pt) && !(neg && negated(w, i, len(pt))) {
				return true
			}
		}
	}
	return false
}

// matches reports whether the pattern holds for w.
func (p pattern) matches(w words, neg bool) bool {
	if len(p.any) > 0 && termHit(w, p.any, neg) {
		return true
	}
	if len(p.all) == 0 {
		return false
	}
	for _, group := range p.all {
		if !termHit(w, group, neg) {
			return false
		}
	}
	return true
}
