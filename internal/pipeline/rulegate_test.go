package pipeline

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/finding"
)

// Hand-built findings for the rule gate. The title is "Sampleland"; a finding
// is about it when its kind is a review, store, policy or terms, or its text or
// URL names it.

const gateTitle = "Sampleland"

type gf struct {
	id     string
	area   checklist.Area
	stance finding.Stance
	kind   string
	url    string
	quote  string
}

func (g gf) f() finding.Finding {
	url := g.url
	if url == "" {
		url = "https://www.sampleland.example/page-" + g.id
	}
	return finding.Finding{ID: g.id, Area: g.area, Stance: g.stance, Quote: g.quote,
		Claim: "claim", Source: finding.Source{URL: url, Kind: g.kind}}
}

func findings(gs ...gf) []finding.Finding {
	out := make([]finding.Finding, len(gs))
	for i, g := range gs {
		out[i] = g.f()
	}
	return out
}

// all builds a complete grade set: every area unknown, then the given grades.
func all(gs ...checklist.Grade) []checklist.Grade {
	out := make([]checklist.Grade, 0, 6)
	for _, a := range checklist.Areas() {
		g := checklist.Grade{Area: a.ID, Level: checklist.Unknown, Findings: []string{}, Reason: parentUnknownReason}
		for _, in := range gs {
			if in.Area == a.ID {
				g = in
				if g.Findings == nil {
					g.Findings = []string{}
				}
			}
		}
		out = append(out, g)
	}
	return out
}

func grade(a checklist.Area, l checklist.Level, ids ...string) checklist.Grade {
	return checklist.Grade{Area: a, Level: l, Findings: ids, Reason: "the judge's reason"}
}

func byArea(gs []checklist.Grade, a checklist.Area) checklist.Grade {
	for _, g := range gs {
		if g.Area == a {
			return g
		}
	}
	return checklist.Grade{}
}

func actionsFor(acts []GateAction, a checklist.Area) []GateAction {
	var out []GateAction
	for _, x := range acts {
		if x.Area == a {
			out = append(out, x)
		}
	}
	return out
}

// want checks that area a ended at level l with the rules rs, in order.
func want(t *testing.T, gs []checklist.Grade, acts []GateAction, a checklist.Area, l checklist.Level, rs ...string) checklist.Grade {
	t.Helper()
	g := byArea(gs, a)
	if g.Level != l {
		t.Errorf("%s = %s, want %s (actions %+v)", a, g.Level, l, actionsFor(acts, a))
	}
	var got []string
	for _, x := range actionsFor(acts, a) {
		got = append(got, x.Rule)
	}
	if !slices.Equal(got, rs) {
		t.Errorf("%s rules = %v, want %v", a, got, rs)
	}
	return g
}

func run(grades []checklist.Grade, fs []finding.Finding, age int) ([]checklist.Grade, []GateAction) {
	return ApplyRules(gateTitle, age, grades, fs)
}

// ---------------------------------------------------------------- money

func TestMoneyFlagNeedsAPurchaseQuote(t *testing.T) {
	fs := findings(
		gf{"junk", checklist.Money, finding.Risk, KindReview, "", "please fix it, the game crashes every night"},
		gf{"bought", checklist.Money, finding.Risk, KindReview, "", "i bought 10,000 coins and spent all my birthday money"},
	)
	// A flag resting on a complaint with no purchase in it is vetoed.
	gs, acts := run(all(grade(checklist.Money, checklist.Flag, "junk")), fs, 9)
	g := want(t, gs, acts, checklist.Money, checklist.Unknown, RuleMoneyNoPurchase)
	if len(g.Findings) != 0 || g.Reason != parentUnknownReason || actionsFor(acts, checklist.Money)[0].From != checklist.Flag {
		t.Errorf("veto left %+v", g)
	}
	// One qualifying quote among the cited is enough, and is the one named.
	gs, acts = run(all(grade(checklist.Money, checklist.Flag, "junk", "bought")), fs, 9)
	want(t, gs, acts, checklist.Money, checklist.Flag, RuleMoneyConfirmed)
	if !slices.Equal(acts[0].Findings, []string{"bought"}) {
		t.Errorf("confirm rests on %v, want only the qualifying quote", acts[0].Findings)
	}
}

func TestMoneyNegatedPurchaseDoesNotQualify(t *testing.T) {
	for _, q := range []string{
		"there are no ads and no in-app purchases in this game",
		"Kids can play without any purchases at all",
		"the game is free from ads",
		"an ad-free experience for children",
	} {
		fs := findings(gf{"x", checklist.Money, finding.Risk, KindReview, "", q})
		gs, acts := run(all(grade(checklist.Money, checklist.Flag, "x")), fs, 9)
		want(t, gs, acts, checklist.Money, checklist.Unknown, RuleMoneyNoPurchase)
	}
	// A negator that does not reach the word leaves it standing.
	fs := findings(gf{"x", checklist.Money, finding.Risk, KindReview, "", "there are no limits on spending in the shop"})
	gs, acts := run(all(grade(checklist.Money, checklist.Flag, "x")), fs, 9)
	want(t, gs, acts, checklist.Money, checklist.Flag, RuleMoneyConfirmed)
}

func TestMoneyControlCapsAFlag(t *testing.T) {
	base := []gf{
		{"risk", checklist.Money, finding.Risk, KindReview, "", "my son bought a lot of coins without asking me"},
		{"ctl", checklist.Money, finding.Mitigation, KindPolicy, "", "Parents can turn on Ask to Buy so every purchase needs approval"},
	}
	gs, acts := run(all(grade(checklist.Money, checklist.Flag, "risk")), findings(base...), 9)
	g := want(t, gs, acts, checklist.Money, checklist.OKWithSetting, RuleMoneyCapped)
	if g.Setting == "" || !slices.Equal(g.Findings, []string{"risk", "ctl"}) {
		t.Errorf("cap = %+v, want a named setting citing the risk and the control", g)
	}
	if !strings.Contains(g.Reason, g.Setting) {
		t.Errorf("reason %q does not name the setting %q", g.Reason, g.Setting)
	}
	if checklist.Settings(gs)[0] != g.Setting {
		t.Errorf("settings %v", checklist.Settings(gs))
	}
	if _, err := checklist.Decide(gs); err != nil {
		t.Errorf("the capped grade set is invalid: %v", err)
	}

	// The control does not cap when a risk quote says it failed ...
	failing := append(slices.Clone(base), gf{"fail", checklist.Money, finding.Risk, KindReview, "", "he bypassed ask to buy and bought coins anyway"})
	gs, acts = run(all(grade(checklist.Money, checklist.Flag, "risk")), findings(failing...), 9)
	want(t, gs, acts, checklist.Money, checklist.Flag, RuleMoneyConfirmed)
	// ... even when the flag does not cite that quote, and when it does.
	gs, acts = run(all(grade(checklist.Money, checklist.Flag, "risk", "fail")), findings(failing...), 9)
	want(t, gs, acts, checklist.Money, checklist.Flag, RuleMoneyConfirmed)

	// A control quote about another product does not count.
	other := []gf{base[0], {"ctl", checklist.Money, finding.Mitigation, KindForum, "https://www.example.org/guide-to-othergame", "Parents can turn on Ask to Buy so every purchase needs approval"}}
	gs, acts = run(all(grade(checklist.Money, checklist.Flag, "risk")), findings(other...), 9)
	want(t, gs, acts, checklist.Money, checklist.Flag, RuleMoneyConfirmed)

	// A quote that names the control only to say it is missing is not one.
	absent := []gf{base[0], {"ctl", checklist.Money, finding.Neutral, KindReview, "", "there is no ask to buy and no spending limit"}}
	gs, acts = run(all(grade(checklist.Money, checklist.Flag, "risk")), findings(absent...), 9)
	want(t, gs, acts, checklist.Money, checklist.Flag, RuleMoneyConfirmed)

	// A risk-stance quote that mentions a control may be describing its
	// absence, so it is not taken as one.
	stance := []gf{base[0], {"ctl", checklist.Money, finding.Risk, KindForum, "", "Sampleland has no way to require parent approval for purchases"}}
	gs, acts = run(all(grade(checklist.Money, checklist.Flag, "risk")), findings(stance...), 9)
	want(t, gs, acts, checklist.Money, checklist.Flag, RuleMoneyConfirmed)

	// Approval by the platform is not a parent control.
	platform := []gf{base[0], {"ctl", checklist.Money, finding.Neutral, KindTerms, "", "Robux may not be purchased except through channels approved by the platform"}}
	gs, acts = run(all(grade(checklist.Money, checklist.Flag, "risk")), findings(platform...), 9)
	want(t, gs, acts, checklist.Money, checklist.Flag, RuleMoneyConfirmed)
}

func TestMoneyFreeListingSettlesOK(t *testing.T) {
	free := gf{"free", checklist.Money, finding.Mitigation, KindStore, "https://apps.apple.com/us/app/sampleland/id1", "Reading games for kids\nFree"}
	// Raised from unknown, citing the listing.
	gs, acts := run(all(), findings(free), 5)
	g := want(t, gs, acts, checklist.Money, checklist.OK, RuleMoneyFree)
	if !slices.Equal(g.Findings, []string{"free"}) || !strings.HasPrefix(g.Reason, "The App Store lists this app as free") {
		t.Errorf("raise = %+v", g)
	}
	// A listing with the in-app purchases mark is a risk finding, not this.
	iap := gf{"iap", checklist.Money, finding.Mitigation, KindStore, "https://apps.apple.com/us/app/sampleland/id1", "Reading games for kids\nFree · In‑App Purchases"}
	gs, acts = run(all(), findings(iap), 5)
	want(t, gs, acts, checklist.Money, checklist.Unknown)
	// A risk quote naming a purchase or an ad stops the raise.
	for _, q := range []string{"the game keeps showing ads to my kid", "my child bought gems with my card"} {
		gs, acts = run(all(), findings(free, gf{"risk", checklist.Money, finding.Risk, KindReview, "", q}), 5)
		want(t, gs, acts, checklist.Money, checklist.Unknown)
	}
	// A risk quote that says there are no ads does not.
	gs, acts = run(all(), findings(free, gf{"risk", checklist.Money, finding.Risk, KindReview, "", "great, there are no ads at all"}), 5)
	want(t, gs, acts, checklist.Money, checklist.OK, RuleMoneyFree)
	// A risk quote about another product does not.
	gs, acts = run(all(), findings(free, gf{"risk", checklist.Money, finding.Risk, KindForum, "https://www.example.org/othergame", "othergame sells loot boxes"}), 5)
	want(t, gs, acts, checklist.Money, checklist.OK, RuleMoneyFree)
	// An ok that cites the listing is confirmed; one that does not is left.
	gs, acts = run(all(grade(checklist.Money, checklist.OK, "free")), findings(free), 5)
	want(t, gs, acts, checklist.Money, checklist.OK, RuleMoneyFree)
	other := gf{"other", checklist.Money, finding.Neutral, KindReview, "", "it is a lovely game and free to play"}
	gs, acts = run(all(grade(checklist.Money, checklist.OK, "other")), findings(free, other), 5)
	want(t, gs, acts, checklist.Money, checklist.OK)
	// A flag with no purchase quote is vetoed, then settled by the listing.
	gs, acts = run(all(grade(checklist.Money, checklist.Flag, "junk")), findings(free, gf{"junk", checklist.Money, finding.Risk, KindReview, "", "it crashes a lot"}), 5)
	g = want(t, gs, acts, checklist.Money, checklist.OK, RuleMoneyNoPurchase, RuleMoneyFree)
	if !slices.Equal(g.Findings, []string{"free"}) {
		t.Errorf("settled grade cites %v", g.Findings)
	}
}

// ---------------------------------------------------------------- data

func TestDataFlagNeedsSaleTrackingMarketingOrBreach(t *testing.T) {
	for q, ok := range map[string]bool{
		"Data Used to Track You\nPurchases, Identifiers":                                  true,
		"We sell your personal information to third parties":                              true,
		"Your information is shared with partners for their own marketing":                true,
		"The company was hit by a data breach that exposed children's names":              true,
		"We use data we collect to decide which ads to show you":                          true,
		"The app collected children's emails without their parents' consent":              true,
		"We share personal information with service providers who host our servers":       false, // service providers never flag
		"Images used for age verification may be processed by third-party providers":      false,
		"We do not sell personal information":                                             false,
		"A partner stores the child's photos and does not say for how long":               false,
		"Read the developer's privacy policy for details on how information is collected": false,
	} {
		kind := KindPolicy
		fs := findings(gf{"x", checklist.Data, finding.Risk, kind, "", q})
		gs, acts := run(all(grade(checklist.Data, checklist.Flag, "x")), fs, 9)
		if ok {
			want(t, gs, acts, checklist.Data, checklist.Flag, RuleDataConfirmed)
		} else {
			want(t, gs, acts, checklist.Data, checklist.Unknown, RuleDataNoBasis)
		}
	}
}

func TestDataServiceProviderSharingNeverFlags(t *testing.T) {
	q := "We share information with service providers that help us operate the app, such as hosting and customer support."
	fs := findings(gf{"sp", checklist.Data, finding.Risk, KindPolicy, "", q})
	gs, acts := run(all(grade(checklist.Data, checklist.Flag, "sp")), fs, 7)
	g := want(t, gs, acts, checklist.Data, checklist.Unknown, RuleDataNoBasis)
	if len(g.Findings) != 0 {
		t.Errorf("veto cites %v", g.Findings)
	}
}

func TestDataNewsAboutAnotherCompanyIsNotCounted(t *testing.T) {
	q := "regulators allege that the app collected children's names and email addresses without their parents' consent."
	other := gf{"n", checklist.Data, finding.Risk, KindNews, "https://news.example/weight-watchers-kurbo", q}
	gs, acts := run(all(grade(checklist.Data, checklist.Flag, "n")), findings(other), 5)
	want(t, gs, acts, checklist.Data, checklist.Unknown, RuleDataNoBasis)
	// The same words in a story that names the title count.
	named := gf{"n", checklist.Data, finding.Risk, KindNews, "https://news.example/story", "Sampleland: " + q}
	gs, acts = run(all(grade(checklist.Data, checklist.Flag, "n")), findings(named), 5)
	want(t, gs, acts, checklist.Data, checklist.Flag, RuleDataConfirmed)
}

func TestDataLabelSettlesOK(t *testing.T) {
	label := gf{"label", checklist.Data, finding.Mitigation, KindStore, "https://apps.apple.com/us/app/sampleland/id1", "Data Not Linked to You\nThe following data may be collected but it is not linked to your identity:"}
	gs, acts := run(all(), findings(label), 5)
	g := want(t, gs, acts, checklist.Data, checklist.OK, RuleDataNotLinked)
	if !slices.Equal(g.Findings, []string{"label"}) {
		t.Errorf("raise cites %v", g.Findings)
	}
	// A quote that reports selling data stops it.
	sells := gf{"sells", checklist.Data, finding.Risk, KindPolicy, "", "We sell personal information to advertisers"}
	gs, acts = run(all(), findings(label, sells), 5)
	want(t, gs, acts, checklist.Data, checklist.Unknown)
	// A flag with no basis is vetoed and then settled by the label.
	junk := gf{"junk", checklist.Data, finding.Risk, KindReview, "", "the app asks for a name and age at setup"}
	gs, acts = run(all(grade(checklist.Data, checklist.Flag, "junk")), findings(label, junk), 5)
	want(t, gs, acts, checklist.Data, checklist.OK, RuleDataNoBasis, RuleDataNotLinked)
}

// ---------------------------------------------------------------- content

func TestContentValueObjectionNeverFlags(t *testing.T) {
	for _, q := range []string{
		"Yoga carries specific spiritual intentions that conflict entirely with our Catholic beliefs, and I do not want this introduced to my daughter.",
		"A game where the child has to put a pink bow on a man in manly clothes. Really?? Deleted immediately.",
		"They have begun to introduce liberal thinking and topics that oppose our child's best interests.",
		"One of the topics that shows up now is yoga and we do not participate in transcendental meditation.",
	} {
		fs := findings(gf{"v", checklist.Content, finding.Risk, KindReview, "", q})
		gs, acts := run(all(grade(checklist.Content, checklist.Flag, "v")), fs, 5)
		g := want(t, gs, acts, checklist.Content, checklist.Unknown, RuleContentNoHarm)
		if len(g.Findings) != 0 {
			t.Errorf("veto cites %v", g.Findings)
		}
	}
	// The same flag keeps a quote that does describe a harm.
	fs := findings(
		gf{"v", checklist.Content, finding.Risk, KindReview, "", "Yoga conflicts with our beliefs."},
		gf{"h", checklist.Content, finding.Risk, KindReview, "", "A grid of scary devil faces popped up while my kid was working on math."},
	)
	gs, acts := run(all(grade(checklist.Content, checklist.Flag, "v", "h")), fs, 5)
	want(t, gs, acts, checklist.Content, checklist.Flag, RuleContentConfirmed)
	if !slices.Equal(acts[0].Findings, []string{"h"}) {
		t.Errorf("confirm rests on %v", acts[0].Findings)
	}
}

func TestContentHarmCategories(t *testing.T) {
	for q, ok := range map[string]bool{
		"There is realistic violence and blood in the shooting mode":      true,
		"Some user games are sexual in nature":                            true,
		"my daughter was asked for photos by a predator":                  true,
		"the lootbox games are basically gambling for kids":               true,
		"it shows how to get drugs":                                       true,
		"a video about self harm autoplayed":                              true,
		"a terrifying jump scare popped up":                               true,
		"kids bully each other in the chat rooms":                         true,
		"There is no violence, no drugs and no sexual content in the app": false, // all negated
		"I do not like the music":                                         false,
	} {
		fs := findings(gf{"x", checklist.Content, finding.Risk, KindReview, "", q})
		gs, acts := run(all(grade(checklist.Content, checklist.Flag, "x")), fs, 9)
		if ok {
			want(t, gs, acts, checklist.Content, checklist.Flag, RuleContentConfirmed)
		} else {
			want(t, gs, acts, checklist.Content, checklist.Unknown, RuleContentNoHarm)
		}
	}
}

func TestContentRatingSettlesOKOnlyWithAWitnessAndNoHarm(t *testing.T) {
	rating := gf{"r", checklist.Content, finding.Neutral, KindStore, "https://apps.apple.com/us/app/sampleland/id1", "Age rating: 4+"}
	witness := gf{"w", checklist.Content, finding.Neutral, KindReview, "", "the content is appropriate and calm for a little one"}
	// Rating at or below the age, a witness, no harm: ok, citing both.
	gs, acts := run(all(), findings(rating, witness), 5)
	g := want(t, gs, acts, checklist.Content, checklist.OK, RuleContentRated)
	if !slices.Equal(g.Findings, []string{"r", "w"}) || !strings.Contains(g.Reason, "4+") {
		t.Errorf("raise = %+v", g)
	}
	// The rating alone is the developer's own claim.
	gs, acts = run(all(), findings(rating), 5)
	want(t, gs, acts, checklist.Content, checklist.Unknown)
	// A rating above the child's age settles nothing.
	gs, acts = run(all(), findings(rating, witness), 3)
	want(t, gs, acts, checklist.Content, checklist.Unknown)
	// A quote reporting harm stops it.
	harm := gf{"h", checklist.Content, finding.Risk, KindReview, "", "a scary clown popped up and frightened my kid"}
	gs, acts = run(all(), findings(rating, witness, harm), 5)
	want(t, gs, acts, checklist.Content, checklist.Unknown)
	// A witness that says the opposite is not one.
	not := gf{"w", checklist.Content, finding.Neutral, KindReview, "", "this is not appropriate for little ones"}
	gs, acts = run(all(), findings(rating, not), 5)
	want(t, gs, acts, checklist.Content, checklist.Unknown)
	// The store's own text cannot be the second witness.
	self := gf{"w", checklist.Content, finding.Neutral, KindStore, "https://apps.apple.com/us/app/sampleland/id1", "Age appropriate learning for kids"}
	gs, acts = run(all(), findings(rating, self), 5)
	want(t, gs, acts, checklist.Content, checklist.Unknown)
}

// ---------------------------------------------------------------- developer record

func TestDeveloperFlagNeedsANewsActionNamingTheDeveloper(t *testing.T) {
	news := gf{"n", checklist.DeveloperRecord, finding.Risk, KindNews, "https://news.example/a", "The attorney general sued Sampleland Corporation over children's data."}
	gs, acts := ApplyRules(gateTitle, 9, all(grade(checklist.DeveloperRecord, checklist.Flag, "n")), findings(news), WithDeveloper("Sampleland Corporation"))
	want(t, gs, acts, checklist.DeveloperRecord, checklist.Flag, RuleDeveloperConfirmed)

	// Another company's suit does not name this developer.
	other := gf{"n", checklist.DeveloperRecord, finding.Risk, KindNews, "https://news.example/a", "The attorney general sued Otherco over children's data."}
	gs, acts = run(all(grade(checklist.DeveloperRecord, checklist.Flag, "n")), findings(other), 9)
	want(t, gs, acts, checklist.DeveloperRecord, checklist.Unknown, RuleDeveloperNoAction)
	// A review saying "sued" is not news or a regulator.
	review := gf{"n", checklist.DeveloperRecord, finding.Risk, KindReview, "", "Sampleland got sued, I heard"}
	gs, acts = run(all(grade(checklist.DeveloperRecord, checklist.Flag, "n")), findings(review), 9)
	want(t, gs, acts, checklist.DeveloperRecord, checklist.Unknown, RuleDeveloperNoAction)
	// A news quote with no action in it is not a record.
	soft := gf{"n", checklist.DeveloperRecord, finding.Risk, KindNews, "https://news.example/a", "Sampleland announced a new update for children."}
	gs, acts = run(all(grade(checklist.DeveloperRecord, checklist.Flag, "n")), findings(soft), 9)
	want(t, gs, acts, checklist.DeveloperRecord, checklist.Unknown, RuleDeveloperNoAction)
	// The developer's name may come from the store listing.
	listing := gf{"n", checklist.DeveloperRecord, finding.Risk, KindNews, "https://news.example/a", "The FTC fined Mojang Studios for collecting data from children."}
	gs, acts = ApplyRules("Blocks", 9, all(grade(checklist.DeveloperRecord, checklist.Flag, "n")), findings(listing), WithDeveloper("Blocks: Play!", "Mojang Studios, Inc."))
	want(t, gs, acts, checklist.DeveloperRecord, checklist.Flag, RuleDeveloperConfirmed)
}

func TestDeveloperOKNeedsAnExplicitStatement(t *testing.T) {
	award := gf{"a", checklist.DeveloperRecord, finding.Neutral, KindStore, "", "Sampleland is a nonprofit and an Editor's Choice winner."}
	// An ok that rests on an award is not a clean record.
	gs, acts := run(all(grade(checklist.DeveloperRecord, checklist.OK, "a")), findings(award), 9)
	g := want(t, gs, acts, checklist.DeveloperRecord, checklist.Unknown, RuleDeveloperNoStmt)
	if len(g.Findings) != 0 {
		t.Errorf("veto cites %v", g.Findings)
	}
	// Absence of news is not evidence: nothing is raised.
	gs, acts = run(all(), findings(award), 9)
	want(t, gs, acts, checklist.DeveloperRecord, checklist.Unknown)
	// An explicit statement naming the developer is.
	clean := gf{"c", checklist.DeveloperRecord, finding.Mitigation, KindArticle, "https://news.example/profile", "Sampleland has no known data breaches or fines."}
	gs, acts = run(all(), findings(clean), 9)
	g = want(t, gs, acts, checklist.DeveloperRecord, checklist.OK, RuleDeveloperClean)
	if !slices.Equal(g.Findings, []string{"c"}) {
		t.Errorf("raise cites %v", g.Findings)
	}
	gs, acts = run(all(grade(checklist.DeveloperRecord, checklist.OK, "c")), findings(clean), 9)
	want(t, gs, acts, checklist.DeveloperRecord, checklist.OK, RuleDeveloperClean)
	// A reported suit outweighs it.
	suit := gf{"s", checklist.DeveloperRecord, finding.Risk, KindNews, "https://news.example/a", "Sampleland was sued by the state."}
	gs, acts = run(all(), findings(clean, suit), 9)
	want(t, gs, acts, checklist.DeveloperRecord, checklist.Unknown)
	// A review cannot make the statement.
	rev := gf{"c", checklist.DeveloperRecord, finding.Mitigation, KindReview, "", "Sampleland has no known data breaches."}
	gs, acts = run(all(), findings(rev), 9)
	want(t, gs, acts, checklist.DeveloperRecord, checklist.Unknown)
}

// ---------------------------------------------------------------- contact

func TestContactFlagNeedsStrangersReachingAChild(t *testing.T) {
	for q, ok := range map[string]bool{
		"the filter didn't catch it when someone asked my 7 year old for her number": true,
		"if your kid plays a child predator might ask where you are":                 true,
		"any user can send a message to any child who has chat on":                   true,
		"Chats built into some games cannot be turned off and strangers use them":    true,
		"Parents must allow access to their device's microphone":                     false,
		"my 4 year old loves the app and the social emotional content":               false,
		"Setting up and login is next to impossible":                                 false,
	} {
		fs := findings(gf{"x", checklist.Contact, finding.Risk, KindReview, "", q})
		gs, acts := run(all(grade(checklist.Contact, checklist.Flag, "x")), fs, 7)
		if ok {
			want(t, gs, acts, checklist.Contact, checklist.Flag, RuleContactConfirmed)
		} else {
			want(t, gs, acts, checklist.Contact, checklist.Unknown, RuleContactNoStranger)
		}
	}
}

func TestContactControlCapsAFlag(t *testing.T) {
	risk := gf{"risk", checklist.Contact, finding.Risk, KindReview, "", "a stranger sent my daughter a message in chat"}
	ctl := gf{"ctl", checklist.Contact, finding.Mitigation, KindForum, "https://guides.example/sampleland-parents", `For younger kids, consider setting chat to "friends only" or turning it off completely.`}
	gs, acts := run(all(grade(checklist.Contact, checklist.Flag, "risk")), findings(risk, ctl), 7)
	g := want(t, gs, acts, checklist.Contact, checklist.OKWithSetting, RuleContactCapped)
	if g.Setting == "" || !slices.Equal(g.Findings, []string{"risk", "ctl"}) {
		t.Errorf("cap = %+v", g)
	}
	// A quote reporting that the setting is bypassed keeps the flag.
	fail := gf{"fail", checklist.Contact, finding.Risk, KindForum, "", "even when chat is set to no one, strangers can reach kids through built-in game chat"}
	gs, acts = run(all(grade(checklist.Contact, checklist.Flag, "risk")), findings(risk, ctl, fail), 7)
	want(t, gs, acts, checklist.Contact, checklist.Flag, RuleContactConfirmed)
	// A control about another title does not cap.
	foreign := ctl
	foreign.url = "https://guides.example/othergame-parents"
	gs, acts = run(all(grade(checklist.Contact, checklist.Flag, "risk")), findings(risk, foreign), 7)
	want(t, gs, acts, checklist.Contact, checklist.Flag, RuleContactConfirmed)
	// A control that the reader labelled a risk is not taken as one.
	asRisk := ctl
	asRisk.stance = finding.Risk
	gs, acts = run(all(grade(checklist.Contact, checklist.Flag, "risk")), findings(risk, asRisk), 7)
	want(t, gs, acts, checklist.Contact, checklist.Flag, RuleContactConfirmed)
}

func TestContactNoChatStatedByTheDeveloperSettlesOK(t *testing.T) {
	stated := gf{"s", checklist.Contact, finding.Neutral, KindPolicy, "", "The app has no chat and no messaging between players."}
	gs, acts := run(all(), findings(stated), 6)
	g := want(t, gs, acts, checklist.Contact, checklist.OK, RuleContactNoChat)
	if !slices.Equal(g.Findings, []string{"s"}) {
		t.Errorf("raise cites %v", g.Findings)
	}
	// A reviewer saying so is not the developer.
	review := gf{"s", checklist.Contact, finding.Neutral, KindReview, "", "The app has no chat and no messaging between players."}
	gs, acts = run(all(), findings(review), 6)
	want(t, gs, acts, checklist.Contact, checklist.Unknown)
	// A report of strangers reaching a child stops it.
	risk := gf{"risk", checklist.Contact, finding.Risk, KindReview, "", "a stranger messaged my son"}
	gs, acts = run(all(), findings(stated, risk), 6)
	want(t, gs, acts, checklist.Contact, checklist.Unknown)
	// No chat feature evidenced: the judge's grade is left alone.
	gs, acts = run(all(grade(checklist.Contact, checklist.OK, "x")), findings(gf{"x", checklist.Contact, finding.Neutral, KindReview, "", "my kid plays with the family"}), 6)
	want(t, gs, acts, checklist.Contact, checklist.OK)
}

// ---------------------------------------------------------------- age gating

func TestAgeGatingFlagNeedsABypassOrAWrongBand(t *testing.T) {
	for q, ok := range map[string]bool{
		"A kid who wants to dodge the checks can simply enter any birthdate before 2007.": true,
		"My friend is 9 and she is in the 21+ age group!":                                 true,
		"Im 14 but it said 9-12 on the age check":                                         true,
		"the face verification said my four year old is twenty five":                      true,
		"I'm 10 and I lied about my birthday to get in":                                   true,
		"It says 8+ and it only goes up to 2nd grade":                                     false, // a lesson level
		"the age range is too small, I am 11 and the highest it goes is 2nd grade":        false,
		"Where is 5th grade?! You need to change this age thing":                          false,
	} {
		fs := findings(gf{"x", checklist.AgeGating, finding.Risk, KindReview, "", q})
		gs, acts := run(all(grade(checklist.AgeGating, checklist.Flag, "x")), fs, 9)
		if ok {
			want(t, gs, acts, checklist.AgeGating, checklist.Flag, RuleAgeGateConfirmed)
		} else {
			want(t, gs, acts, checklist.AgeGating, checklist.Unknown, RuleAgeGateNoBypass)
		}
	}
}

func TestAgeGatingWitnessIsKept(t *testing.T) {
	// The wording has no bypass term, but the older witness check verified it.
	f := gf{"x", checklist.AgeGating, finding.Risk, KindReview, "", "My nephew is five and the app put him with the teenagers"}
	gs, acts := ApplyRules(gateTitle, 5, all(grade(checklist.AgeGating, checklist.Flag, "x")), findings(f))
	want(t, gs, acts, checklist.AgeGating, checklist.Unknown, RuleAgeGateNoBypass)
	gs, acts = ApplyRules(gateTitle, 5, all(grade(checklist.AgeGating, checklist.Flag, "x")), findings(f), withAgeWitness("the app put him with the teenagers"))
	want(t, gs, acts, checklist.AgeGating, checklist.Flag, RuleAgeGateConfirmed)
}

func TestAgeGatingControlCapsAFlag(t *testing.T) {
	risk := gf{"risk", checklist.AgeGating, finding.Risk, KindReview, "", "my 8 year old entered a fake birthday and got in"}
	ctl := gf{"ctl", checklist.AgeGating, finding.Mitigation, KindPolicy, "", "The birthday on a child account is locked and only a parent can change it."}
	gs, acts := run(all(grade(checklist.AgeGating, checklist.Flag, "risk")), findings(risk, ctl), 8)
	g := want(t, gs, acts, checklist.AgeGating, checklist.OKWithSetting, RuleAgeGateCapped)
	if g.Setting == "" || !slices.Equal(g.Findings, []string{"risk", "ctl"}) {
		t.Errorf("cap = %+v", g)
	}
	bypass := gf{"b", checklist.AgeGating, finding.Risk, KindReview, "", "kids get around the locked birthday by making a new account"}
	gs, acts = run(all(grade(checklist.AgeGating, checklist.Flag, "risk")), findings(risk, ctl, bypass), 8)
	want(t, gs, acts, checklist.AgeGating, checklist.Flag, RuleAgeGateConfirmed)
}

// ---------------------------------------------------------------- invariants

// Whatever the findings and grades, the gate never leaves a grade without a
// verified citation of its own area, never a flag without a risk, never a
// setting grade without a setting, and every action cites findings that exist
// (a veto cites none).
func TestGateNeverInventsAGradeWithoutCitations(t *testing.T) {
	quotes := map[checklist.Area][]string{
		checklist.Money:           {"i bought coins with my birthday money", "please fix it", "Reading games\nFree", "Parents can turn on Ask to Buy so purchases need approval", "he bypassed ask to buy", "the game shows ads"},
		checklist.Data:            {"We sell personal information to advertisers", "Data Not Linked to You", "We share information with service providers to host the app", "Data Used to Track You", "it is fun"},
		checklist.Content:         {"Age rating: 4+", "the content is appropriate for young kids", "a scary clown frightened my kid", "yoga conflicts with our beliefs", "violence and blood"},
		checklist.DeveloperRecord: {"The FTC fined Sampleland over children's data.", "Sampleland has no known data breaches.", "Sampleland won an award.", "a lawsuit against Otherco"},
		checklist.Contact:         {"strangers can chat with kids", "set chat to friends only", "even when chat is off strangers get through", "The app has no chat", "kids love it"},
		checklist.AgeGating:       {"enter any birthday to get in", "the birthday is locked, only a parent can change it", "it only goes up to 2nd grade", "my 9 year old is in the 21+ age group"},
	}
	kinds := []string{KindReview, KindStore, KindPolicy, KindNews, KindForum, KindRating}
	stances := []finding.Stance{finding.Risk, finding.Mitigation, finding.Neutral}
	levels := []checklist.Level{checklist.OK, checklist.OKWithSetting, checklist.Flag, checklist.Unknown}
	rng := rand.New(rand.NewSource(7))
	for iter := 0; iter < 4000; iter++ {
		var fs []finding.Finding
		for _, a := range checklist.Areas() {
			for n := rng.Intn(5); n > 0; n-- {
				qs := quotes[a.ID]
				fs = append(fs, finding.Finding{
					ID: fmt.Sprintf("f%d", len(fs)), Area: a.ID, Stance: stances[rng.Intn(3)], Quote: qs[rng.Intn(len(qs))],
					Source: finding.Source{Kind: kinds[rng.Intn(len(kinds))], URL: fmt.Sprintf("https://x.example/%d", rng.Intn(3))},
				})
			}
		}
		var in []checklist.Grade
		for _, a := range checklist.Areas() {
			g := checklist.Grade{Area: a.ID, Level: levels[rng.Intn(4)], Findings: []string{}, Reason: "judge"}
			if g.Level == checklist.OKWithSetting {
				g.Setting = "a setting"
			}
			if g.Level != checklist.Unknown {
				// What the earlier checks leave: cited ids of this area and a fitting stance.
				for _, f := range fs {
					if f.Area != a.ID || rng.Intn(2) == 0 {
						continue
					}
					if g.Level == checklist.Flag && f.Stance != finding.Risk {
						continue
					}
					g.Findings = append(g.Findings, f.ID)
				}
				supported := false
				for _, id := range g.Findings {
					for _, f := range fs {
						supported = supported || (f.ID == id && f.Stance != finding.Risk)
					}
				}
				if len(g.Findings) == 0 || (g.Level == checklist.OK && !supported) {
					g.Level, g.Setting, g.Findings = checklist.Unknown, "", []string{}
				}
			}
			in = append(in, g)
		}
		before := reflect.ValueOf(in).Interface()
		out, acts := ApplyRules(gateTitle, 3+rng.Intn(10), in, fs)
		if !reflect.DeepEqual(before, in) {
			t.Fatal("the input grades were modified")
		}
		if _, err := checklist.Decide(out); err != nil {
			t.Fatalf("invalid grade set: %v", err)
		}
		byID := map[string]finding.Finding{}
		for _, f := range fs {
			byID[f.ID] = f
		}
		for _, g := range out {
			if g.Level == checklist.Unknown {
				if len(g.Findings) != 0 || g.Setting != "" {
					t.Fatalf("%s unknown with %v / %q", g.Area, g.Findings, g.Setting)
				}
				continue
			}
			if len(g.Findings) == 0 {
				t.Fatalf("%s %s cites nothing", g.Area, g.Level)
			}
			risk, support := false, false
			for _, id := range g.Findings {
				f, ok := byID[id]
				if !ok || f.Area != g.Area {
					t.Fatalf("%s %s cites %s, not a finding of its area", g.Area, g.Level, id)
				}
				risk = risk || f.Stance == finding.Risk
				support = support || f.Stance != finding.Risk
			}
			if g.Level == checklist.Flag && !risk {
				t.Fatalf("%s flag without a risk finding", g.Area)
			}
			if g.Level == checklist.OKWithSetting && g.Setting == "" {
				t.Fatalf("%s setting grade without a setting", g.Area)
			}
			if g.Level != checklist.OKWithSetting && g.Setting != "" {
				t.Fatalf("%s %s keeps a setting", g.Area, g.Level)
			}
			if g.Level == checklist.OK && !support {
				t.Fatalf("%s ok citing only risks", g.Area)
			}
		}
		for _, a := range acts {
			if a.From == a.To && a.Rule == "" {
				t.Fatalf("action %+v", a)
			}
			if a.Note == "" || a.Findings == nil {
				t.Fatalf("action %+v lacks a note or findings", a)
			}
			if a.To == checklist.Unknown && len(a.Findings) != 0 {
				t.Fatalf("veto %+v cites findings", a)
			}
			if a.To != checklist.Unknown && len(a.Findings) == 0 {
				t.Fatalf("action %+v rests on nothing", a)
			}
			for _, id := range a.Findings {
				if f, ok := byID[id]; !ok || f.Area != a.Area {
					t.Fatalf("action %+v cites %s", a, id)
				}
			}
			if !strings.HasPrefix(a.Rule, string(a.Area)+".") {
				t.Fatalf("rule %q is not in area %s", a.Rule, a.Area)
			}
		}
		// The last action on an area ends at the grade the area carries.
		last := map[checklist.Area]checklist.Level{}
		for _, a := range acts {
			last[a.Area] = a.To
		}
		for _, g := range out {
			if l, ok := last[g.Area]; ok && l != g.Level {
				t.Fatalf("%s ends %s but the last action says %s", g.Area, g.Level, l)
			}
		}
	}
}

func TestMatchingBasics(t *testing.T) {
	w := gateWords("We don’t sell kids' data; In-App Purchases are OFF. 12+")
	if !reflect.DeepEqual([]string(w), []string{"we", "dont", "sell", "kids", "data", "in", "app", "purchases", "are", "off", "12"}) {
		t.Fatalf("words = %v", w)
	}
	for _, c := range []struct {
		terms []string
		neg   bool
		want  bool
	}{
		{[]string{"in app purchas*"}, false, true},
		{[]string{"in-app"}, false, true}, // hyphens read as spaces
		{[]string{"purchase"}, false, false},
		{[]string{"sell"}, true, false}, // "dont sell": negated
		{[]string{"sell"}, false, true},
		{[]string{"#"}, false, true},
	} {
		if got := hitsAny(w, c.terms, c.neg); got != c.want {
			t.Errorf("hitsAny(%v, neg=%v) = %v, want %v", c.terms, c.neg, got, c.want)
		}
	}
	// An all pattern needs every group.
	p := pattern{all: [][]string{{"chat"}, {"stranger*"}}}
	if p.matches(gateWords("chat is fun"), false) || !p.matches(gateWords("strangers can chat"), false) {
		t.Error("all pattern")
	}
}

// ---------------------------------------------------------------- the judge step

// The judge step runs the gate last: a flag on a quote that names no harm is
// vetoed after the judge, entailment and the parent-reason writer, the area is
// thin, and the verdict is computed from the gated grades.
func TestJudgeRunsTheGateLast(t *testing.T) {
	fs := findings(
		gf{"junk", checklist.Content, finding.Risk, KindReview, "", "Yoga conflicts with our beliefs and I do not want it near my daughter."},
		gf{"label", checklist.Data, finding.Mitigation, KindStore, "https://apps.apple.com/us/app/sampleland/id1", "Data Not Linked to You\nThe following data may be collected but it is not linked to your identity:"},
	)
	reply := gradesJSON(t,
		checklist.Grade{Area: checklist.Content, Level: checklist.Flag, Findings: []string{"junk"}, Reason: "A parent objects."},
		checklist.Grade{Area: checklist.Data, Level: checklist.Unknown, Findings: []string{}, Reason: "Nothing."},
	)
	j := &Judge{Model: &superRecorder{replies: []any{reply}}, Meter: openTestMeter(t)}
	got, err := j.Judge(context.Background(), gateTitle, 5, "c", fs, nil)
	if err != nil {
		t.Fatal(err)
	}
	by := map[checklist.Area]checklist.Grade{}
	for _, g := range got.Grades {
		by[g.Area] = g
	}
	if by[checklist.Content].Level != checklist.Unknown || by[checklist.Content].Reason != parentUnknownReason {
		t.Errorf("content = %+v, want the veto", by[checklist.Content])
	}
	if by[checklist.Data].Level != checklist.OK || !slices.Equal(by[checklist.Data].Findings, []string{"label"}) {
		t.Errorf("data = %+v, want the raise on the label", by[checklist.Data])
	}
	if !slices.Contains(got.Thin, checklist.Content) || slices.Contains(got.Thin, checklist.Data) {
		t.Errorf("thin = %v", got.Thin)
	}
	rules := []string{}
	for _, a := range got.Gate {
		rules = append(rules, a.Rule)
	}
	if !slices.Equal(rules, []string{RuleDataNotLinked, RuleContentNoHarm}) && !slices.Equal(rules, []string{RuleContentNoHarm, RuleDataNotLinked}) {
		t.Errorf("gate actions %v", rules)
	}
	// The verdict is computed from the gated grades: with the flag gone, it
	// is no longer "no" (contact is unknown, so unclear).
	if got.Verdict != checklist.Unclear {
		t.Errorf("verdict %s, want unclear", got.Verdict)
	}
}
