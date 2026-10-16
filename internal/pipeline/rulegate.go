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
//     quote reports that control failing. In money the control is a purchase
//     control and the failure test is narrower: a complaint about spending,
//     scams or billing is not the control failing, so only a quote that says
//     the control itself failed or was bypassed keeps the flag (rule D).
//   - A cited quote that reports a fine, settlement, lawsuit or regulator
//     action, names the developer or seller and does not name the title is
//     about another product: it supports no grade, is removed from the
//     citations, and is shown on the card as information (rule E). A regulator
//     action that names the title itself is still evidence.
//   - A data flag whose every qualifying quote is one user's report of their
//     own account being hacked, stolen or compromised falls to unknown: that
//     is account security, not how the company handles children's data. Two
//     or more independent reports, a breach or leak at the company, or a
//     quote about selling, tracking, sharing or consentless collection keeps
//     the flag (rule F).
//   - A tracking label caps data at ok_with_setting with Ask App Not to Track
//     when no quote reports sale, marketing sharing, or a company breach.
//     Those reports keep data at flag (rule G).
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
	RuleDataOwnAccount      = "data.own_account_hacked"
	RuleDataTrackingSetting = "data.tracking_setting"
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
	RuleContactKids         = "contact.kids_category_no_contact_report"
	RuleContentSingleMild   = "content.single_mild_report"
	RuleAgeGateNoBypass     = "age_gating.flag_without_bypass"
	RuleAgeGateCapped       = "age_gating.flag_capped_by_control"
	RuleAgeGateConfirmed    = "age_gating.flag_confirmed"
	// Rule E: the quote is shown as information and supports no grade. The
	// id's first part is not an area, because the fact is not a grade.
	RuleRegulatorOtherProduct = "information.regulator_action_other_product"
	ruleConfirmedNotePrefix   = "A quote behind this grade describes "
)

// Kinds of gate action. The empty kind is a grade action: a rule confirmed,
// vetoed, capped or settled a grade, and From and To say which. GateKindInfo
// reports a fact and changes no grade, so the card renders it as plain
// information under "Rule checks".
const GateKindInfo = "info"

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
	// Kind is empty for a grade action and GateKindInfo for rule E's
	// information. Older cards carry no kind; the web reads an absent kind
	// as a grade action.
	Kind string `json:"kind,omitempty"`
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
	g := newGate(age, title, o.names, fs)
	g.witness = o.witness
	out := make([]checklist.Grade, len(grades))
	for i, in := range grades {
		in.Findings = slices.Clone(in.Findings)
		out[i] = in
	}
	// Rule E runs first, so the per-area rules see the citations that are
	// left and decide whether each grade still stands on them.
	acts := g.otherProductInfo(out)
	for i, in := range out {
		out[i], acts = g.area(in, acts)
		if out[i].Area == checklist.Data {
			out[i], acts = g.trackingSetting(out[i], acts)
		}
	}
	return out, acts
}

// trackingSetting applies rule G after the area's flag rules, including rule
// F. Apple's tracking label can be blocked in iOS settings. Selling data,
// sharing it for marketing, or a company breach keeps data at flag.
func (g *gate) trackingSetting(in checklist.Grade, acts []GateAction) (checklist.Grade, []GateAction) {
	if in.Level != checklist.Flag && in.Level != checklist.Unknown {
		return in, acts
	}
	var label []finding.Finding
	for _, f := range g.fs {
		if f.Area == checklist.Data && f.Source.Kind == KindStore && f.Stance == finding.Risk && trackingLabel(g.w(f)) && g.onSubject(f) {
			label = append(label, f)
		}
	}
	if len(label) == 0 {
		return in, acts
	}
	var saleOrBreach []finding.Finding
	for _, f := range g.fs {
		if f.Area != checklist.Data || !g.onSubject(f) {
			continue
		}
		w := g.w(f)
		sold := dataSale.matches(w, true)
		marketing := dataMarketingShare.matches(w, true)
		breach := hitsAny(w, companyBreach, true) && !hitsAny(w, accountHacked, true)
		if sold || marketing || breach {
			saleOrBreach = append(saleOrBreach, f)
		}
	}
	if len(saleOrBreach) > 0 {
		from := in.Level
		all := append(slices.Clone(saleOrBreach), label...)
		in.Level, in.Setting, in.Findings = checklist.Flag, "", orderLike(ids(all), all)
		in.Reason = "A source reports data being sold or shared for marketing, or a company breach or leak."
		acts = append(acts, GateAction{Area: checklist.Data, Rule: RuleDataTrackingSetting, From: from, To: checklist.Flag, Findings: ids(all), Note: "The App Store label shows the app tracks for ads, and a source reports data being sold or shared for marketing, or a company breach or leak."})
		return in, acts
	}
	from := in.Level
	in.Level, in.Setting = checklist.OKWithSetting, "Ask App Not to Track"
	in.Findings = ids(label)
	in.Reason = "Turn on Ask App Not to Track in iOS Settings → Privacy & Security → Tracking."
	acts = append(acts, GateAction{Area: checklist.Data, Rule: RuleDataTrackingSetting, From: from, To: checklist.OKWithSetting, Findings: ids(label), Note: "The App Store label shows the app tracks for ads, and iOS lets a parent block it in Settings → Privacy & Security → Tracking."})
	return in, acts
}

func trackingLabel(w words) bool {
	return hitsAny(w, []string{"data used to track you"}, false) ||
		(pattern{all: [][]string{{"app tracking"}, {"ad", "ads", "advertis*", "advertising", "marketing"}}}).matches(w, false)
}

// otherProductInfo applies rule E: a cited quote that reports a fine,
// settlement, lawsuit or regulator action, names the developer or seller and
// does not name the title is about another product. It supports no grade, so
// it is removed from the citations, and it becomes one information action on
// the card. What stands without it is the existing rules' decision: a flag is
// held to its remaining citations in area, and an ok or ok_with_setting left
// with nothing to cite falls to unknown, as checklist.Enforce's
// ok_without_finding does.
func (g *gate) otherProductInfo(gs []checklist.Grade) []GateAction {
	var acts []GateAction
	seen := map[string]bool{}
	for i := range gs {
		kept := make([]string, 0, len(gs[i].Findings))
		for _, id := range gs[i].Findings {
			f, ok := g.find(id)
			if ok {
				if name, other := g.otherProduct(f); other {
					if !seen[id] {
						seen[id] = true
						acts = append(acts, g.infoAction(f, gs[i].Level, name))
					}
					continue
				}
			}
			kept = append(kept, id)
		}
		if len(kept) == len(gs[i].Findings) {
			continue
		}
		gs[i].Findings = kept
		if len(kept) == 0 && (gs[i].Level == checklist.OK || gs[i].Level == checklist.OKWithSetting) {
			acts = append(acts, GateAction{Area: gs[i].Area, Rule: checklist.RuleOKNoProof, From: gs[i].Level, To: checklist.Unknown,
				Findings: []string{}, Note: checklist.Reasons[checklist.RuleOKNoProof]})
			gs[i] = unknownGrade(gs[i])
		}
	}
	return acts
}

// otherProduct reports whether a finding is a fine, settlement, lawsuit or
// regulator action about another product: it names the developer, the seller
// or a platform seller, and does not name the title. It returns the name the
// quote uses, for the card. A regulator action that names the title is
// evidence, as before.
func (g *gate) otherProduct(f finding.Finding) (string, bool) {
	w := g.w(f)
	if !hitsVocab(w, legalAction, true) {
		return "", false
	}
	if i := g.namedFrom(w, 0); i >= 0 && i < g.titleN {
		return "", false
	}
	if i := g.namedFrom(w, g.titleN); i >= 0 {
		return g.raw[i], true
	}
	for _, s := range platformSellers {
		if termHit(w, []string{s.term}, false) {
			return s.display, true
		}
	}
	return "", false
}

// infoAction is rule E's card line: the quote itself as information, with
// whose product it is about and what the gate did with it. The name leads
// only when the quote does not begin with it.
func (g *gate) infoAction(f finding.Finding, level checklist.Level, name string) GateAction {
	note := fmt.Sprintf("%s — a different product, so this is shown as information and is not evidence about %s.", f.Quote, g.title)
	if !matchAt(g.w(f), 0, parseTerm(name)) {
		note = name + ": " + note
	}
	return GateAction{
		Area: f.Area, Rule: RuleRegulatorOtherProduct, Kind: GateKindInfo,
		From: level, To: level, Findings: []string{f.ID},
		Note: note,
	}
}

// gate is one run's view of the evidence.
type gate struct {
	age   int
	title string
	// names holds the title's variants first, then the developer's and
	// seller's; titleN is how many are the title's. raw parallels names with
	// the name each variant came from, for the card.
	names  [][]string // each name as normalised words
	squash []string   // each name with every non-alphanumeric removed, for URLs
	titleN int
	raw    []string
	fs     []finding.Finding
	words  map[string]words
	// witness holds the age-gate witness quotes entailment verified.
	witness []string
}

func newGate(age int, title string, developer []string, fs []finding.Finding) *gate {
	g := &gate{age: age, title: title, fs: fs, words: make(map[string]words, len(fs))}
	seen := map[string]bool{}
	add := func(raw string) {
		for _, n := range nameVariants(raw) {
			w := gateWords(n)
			sq := strings.Join(w, "")
			if len(sq) < 3 || seen[sq] {
				continue
			}
			seen[sq] = true
			g.names = append(g.names, w)
			g.squash = append(g.squash, sq)
			g.raw = append(g.raw, raw)
		}
	}
	add(title)
	g.titleN = len(g.names)
	for _, n := range developer {
		add(n)
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

// namedFrom reports the index of the first name, from variant lo upwards,
// that the words carry, or -1. Variants below g.titleN are the title's own.
func (g *gate) namedFrom(w words, lo int) int {
	for i := lo; i < len(g.names); i++ {
		for j := range w {
			if matchAt(w, j, g.names[i]) {
				return i
			}
		}
	}
	return -1
}

// named reports whether the words carry one of the title's names.
func (g *gate) named(w words) bool { return g.namedFrom(w, 0) >= 0 }

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
	// purchaseControl marks the money cap (rule D): a complaint about
	// spending, scams or billing does not stop it, only a quote that says
	// the control itself failed or was bypassed does.
	purchaseControl bool
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
			controls: moneyControls, capRule: RuleMoneyCapped, purchaseControl: true,
			settle: settleMoney,
		},
		checklist.Data: {
			risk: func(g *gate, f finding.Finding) bool {
				return trackingLabel(g.w(f)) || hitsVocab(g.w(f), dataRisk, true)
			},
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
		if len(support) > 0 && in.Area == checklist.Content {
			if g2, act, done := g.singleMild(in, support); done {
				return g2, append(acts, act)
			}
		}
		if len(support) > 0 && in.Area == checklist.Data {
			if g2, act, done := g.ownAccountReport(in, support); done {
				return g2, append(acts, act)
			}
		}
		if len(support) > 0 {
			if c, label, ctl := g.cap(in, spec, support); ctl != nil {
				note := fmt.Sprintf("A source names a control that answers this concern (%s), and no cited quote says the control fails.", label)
				if spec.purchaseControl {
					note = fmt.Sprintf("A source names a purchase control that answers this concern (%s), and no verified quote says the control failed or was bypassed. Complaints about spending, scams or billing are not counted against it.", label)
				}
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
// A control is read from the quote's text, whatever stance the reader gave it:
// the quote must describe the control (a pattern in the area's control list),
// must be about the title, must not itself report a failure or say the control
// is missing (controlAbsent), and no risk quote may report
// the control failing: not a cited one, and not an uncited one that speaks of
// the area's harm. A cap relaxes a flag, so every test errs toward leaving
// the flag. The returned grade cites the supporting risks and the control
// quotes; ctl is nil when there is no cap.
//
// Rule D narrows the failure test for money: a complaint about spending,
// scams or billing ("charged three times", "the add-on never turned up") is
// not the control failing, so the cited-failure check below is skipped and
// only a quote that speaks of a control and says it failed or was bypassed
// ("bought without asking even with approval on") blocks the cap.
func (g *gate) cap(in checklist.Grade, spec areaSpec, support []finding.Finding) (checklist.Grade, string, []finding.Finding) {
	if len(spec.controls) == 0 {
		return in, "", nil
	}
	if !spec.purchaseControl {
		for _, id := range in.Findings {
			if f, ok := g.find(id); ok && f.Stance == finding.Risk && hitsAny(g.w(f), controlFailure, false) {
				return in, "", nil
			}
		}
	}
	// A report that the control fails also counts when the flag does not cite
	// it, if it is a risk quote about the title that speaks of this area's
	// harm: the judge cites few quotes, and a flag that leaves out the failing
	// control must not be lowered on that account.
	if len(g.riskIn(in.Area, func(f finding.Finding) bool {
		return spec.risk(g, f) && hitsAny(g.w(f), controlFailure, false) && g.speaksOfAControl(spec, f)
	})) > 0 {
		return in, "", nil
	}
	for _, c := range spec.controls {
		var ctl []finding.Finding
		for _, f := range g.fs {
			if f.Area == in.Area && g.onSubject(f) && c.pat.matches(g.w(f), true) && !hitsAny(g.w(f), controlFailure, false) && !hitsAny(g.w(f), controlAbsent, false) {
				ctl = append(ctl, f)
			}
		}
		// A quote the reader labelled mitigation or neutral is named first.
		slices.SortStableFunc(ctl, func(a, b finding.Finding) int {
			return btoi(a.Stance == finding.Risk) - btoi(b.Stance == finding.Risk)
		})
		if len(ctl) == 0 {
			continue
		}
		if len(ctl) > 2 {
			ctl = ctl[:2]
		}
		out := in
		out.Level, out.Setting = checklist.OKWithSetting, c.label
		all := append(slices.Clone(support), ctl...)
		out.Findings = orderLike(ids(all), all)
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
	rating, n := g.rating()
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

func settleContact(g *gate, cur checklist.Grade) *settlement {
	if st := settleContactStated(g, cur); st != nil {
		return st
	}
	// Apple's Kids category: the listing's "Made For Ages X-Y" line.
	for _, f := range g.fs {
		if gateOnly(f) && f.Stance == finding.Neutral {
			m := ageSpan.FindStringSubmatch(f.Quote)
			if m == nil || !g.noRisk(checklist.Contact) {
				return nil
			}
			return &settlement{RuleContactKids, []finding.Finding{f}, "Listed in the App Store's Kids category, for ages " + m[1] + "–" + m[2] + "; no source reports contact with strangers."}
		}
	}
	return nil
}

var ageSpan = regexp.MustCompile(`(\d+)\s*[–-]\s*(\d+)`)

func settleContactStated(g *gate, _ checklist.Grade) *settlement {
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

// speaksOfAControl reports whether a quote talks about a control of the area
// (one of its control patterns, or a control word), so that a failure word in
// an unrelated complaint ("the add-on still didn't work") is not read as the
// control failing.
func (g *gate) speaksOfAControl(spec areaSpec, f finding.Finding) bool {
	w := g.w(f)
	for _, c := range spec.controls {
		if c.pat.matches(w, false) {
			return true
		}
	}
	return hitsAny(w, controlTopic, false)
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// orderLike returns the ids of ids in the order of fs, each once.
func orderLike(ids []string, fs []finding.Finding) []string {
	out := make([]string, 0, len(ids))
	for _, f := range fs {
		if slices.Contains(ids, f.ID) && !slices.Contains(out, f.ID) {
			out = append(out, f.ID)
		}
	}
	return out
}

// rating returns the store's age rating finding and its number, or nil.
func (g *gate) rating() (*finding.Finding, int) {
	for i, f := range g.fs {
		if f.Area != checklist.Content || f.Source.Kind != KindStore {
			continue
		}
		if m := storeRating.FindStringSubmatch(f.Quote); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil {
				return &g.fs[i], v
			}
		}
	}
	return nil, 0
}

// severity reads how serious a content quote is. Words of the mild phrases
// ("cartoon violence") are set aside before the serious vocabulary is read.
func (g *gate) severity(f finding.Finding) (serious, mild bool) {
	w := g.w(f)
	masked := slices.Clone(w)
	for _, t := range contentMildPhrases {
		pt := parseTerm(t)
		for i := range w {
			if matchAt(w, i, pt) {
				for k := range pt {
					masked[i+k] = ""
				}
			}
		}
	}
	return hitsVocab(masked, contentSerious, true), hitsVocab(w, contentMild, true)
}

// singleMild is content.single_mild_report. A content flag that rests only on
// mild harm (frightening imagery, mild peril, crude humour), from one distinct
// source among all the title's mild reports, in a title whose store age rating
// is at or below the child's age, does not stand. One serious quote among the
// flag's qualifying citations, two independent mild sources, or no rating at
// or below the age, and the flag is left to the other rules. With a non-risk
// content finding to cite besides the rating, the area becomes ok on the
// rating and that finding; without one it becomes unknown. An ok is never
// made without a citation.
func (g *gate) singleMild(in checklist.Grade, support []finding.Finding) (checklist.Grade, GateAction, bool) {
	for _, f := range support {
		if s, _ := g.severity(f); s {
			return in, GateAction{}, false
		}
	}
	sources := map[string]bool{}
	for _, f := range g.riskIn(checklist.Content, func(f finding.Finding) bool { _, m := g.severity(f); return m }) {
		sources[f.Source.URL+"#"+strconv.Itoa(f.Source.Index)] = true
	}
	rating, n := g.rating()
	if len(sources) >= 2 || rating == nil || n > g.age {
		return in, GateAction{}, false
	}
	var witness *finding.Finding
	for i, f := range g.fs {
		if f.Area == checklist.Content && f.Stance != finding.Risk && f.ID != rating.ID && g.onSubject(f) {
			if witness == nil || (hitsVocab(g.w(f), contentBenign, true) && !hitsVocab(g.w(*witness), contentBenign, true)) {
				witness = &g.fs[i]
			}
		}
	}
	if witness == nil {
		return unknownGrade(in), GateAction{Area: in.Area, Rule: RuleContentSingleMild, From: checklist.Flag, To: checklist.Unknown, Findings: []string{},
			Note: fmt.Sprintf("Only one source reports mild frightening or crude material, and the App Store rates this app %d+, at or below the child's age. Nothing else was found to show the content is fine, so the area is left open.", n)}, true
	}
	note := fmt.Sprintf("Only one source reports mild frightening or crude material, and the App Store rates this app %d+, at or below the child's age.", n)
	out := in
	out.Level, out.Setting, out.Reason = checklist.OK, "", note
	out.Findings = []string{rating.ID, witness.ID}
	return out, GateAction{Area: in.Area, Rule: RuleContentSingleMild, From: checklist.Flag, To: checklist.OK, Findings: []string{rating.ID, witness.ID}, Note: note}, true
}

// ownAccountReport is data.own_account_hacked. A data flag whose every
// qualifying citation is a user's report of their own account being hacked,
// stolen or compromised does not stand: one person's hacked account is about
// account security, not about how the company handles children's data, so the
// area is left open rather than flagged. The flag stays when two or more
// independent hack reports exist anywhere in the area's verified risk quotes
// about the title, or when a quote behind the flag itself describes a breach
// or leak at the company, or data being sold, tracked for ads, shared for
// marketing or collected without consent — a quote that reads "hacked" and
// "leaked" together is reporting both.
func (g *gate) ownAccountReport(in checklist.Grade, support []finding.Finding) (checklist.Grade, GateAction, bool) {
	for _, f := range support {
		if !hitsAny(g.w(f), accountHacked, true) {
			return in, GateAction{}, false
		}
	}
	sources := map[string]bool{}
	for _, f := range g.riskIn(checklist.Data, func(f finding.Finding) bool { return hitsAny(g.w(f), accountHacked, true) }) {
		sources[f.Source.URL+"#"+strconv.Itoa(f.Source.Index)] = true
	}
	if len(sources) >= 2 {
		return in, GateAction{}, false
	}
	for _, id := range in.Findings {
		f, ok := g.find(id)
		if !ok || f.Stance != finding.Risk || !g.onSubject(f) {
			continue
		}
		if hitsAny(g.w(f), companyBreach, true) || hitsVocab(g.w(f), dataMisuse, true) {
			return in, GateAction{}, false
		}
	}
	return unknownGrade(in), GateAction{Area: in.Area, Rule: RuleDataOwnAccount, From: checklist.Flag, To: checklist.Unknown, Findings: []string{},
		Note: "One user reports that their own account was hacked, stolen or compromised. That is about the account's security, not about how the company handles children's data, so it is not enough evidence to concern the app."}, true
}
