package pipeline

import (
	"slices"
	"strings"
	"testing"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/finding"
)

// Tests for the three policy rules added to the gate: the Kids category for
// contact, content severity, and controls read from the text.

// ---------------------------------------------------------------- Kids category

func kidsFact(ages string) finding.Finding {
	return finding.Finding{ID: "kids", Area: checklist.Contact, Stance: finding.Neutral, Quote: "Made For Ages " + ages,
		Claim:  kidsCategoryClaim + ", made for ages " + ages + ".",
		Source: finding.Source{Kind: KindStore, URL: "https://apps.apple.com/us/app/sampleland/id1"}}
}

func TestKidsCategorySettlesContactOK(t *testing.T) {
	fs := []finding.Finding{kidsFact("0–5")}
	gs, acts := run(all(), fs, 5)
	g := want(t, gs, acts, checklist.Contact, checklist.OK, RuleContactKids)
	if !slices.Equal(g.Findings, []string{"kids"}) || g.Reason != "Listed in the App Store's Kids category, for ages 0–5; no source reports contact with strangers." {
		t.Errorf("raise = %+v", g)
	}
	// Any contact risk quote about the title stops it.
	risk := gf{"risk", checklist.Contact, finding.Risk, KindReview, "", "a stranger sent my daughter a message in chat"}.f()
	gs, acts = run(all(), append(slices.Clone(fs), risk), 5)
	want(t, gs, acts, checklist.Contact, checklist.Unknown)
	// A contact risk quote about another product does not.
	foreign := gf{"risk", checklist.Contact, finding.Risk, KindForum, "https://www.example.org/othergame", "strangers can message kids in othergame chat"}.f()
	gs, acts = run(all(), append(slices.Clone(fs), foreign), 5)
	want(t, gs, acts, checklist.Contact, checklist.OK, RuleContactKids)
	// Outside the Kids category (no such fact) nothing is raised, and a review
	// that merely quotes the words is not the store's fact.
	quoted := gf{"q", checklist.Contact, finding.Neutral, KindReview, "", "Made For Ages 0-5 it says"}.f()
	gs, acts = run(all(), []finding.Finding{quoted}, 5)
	want(t, gs, acts, checklist.Contact, checklist.Unknown)
	gs, acts = run(all(), nil, 5)
	want(t, gs, acts, checklist.Contact, checklist.Unknown)
	// A flag with no stranger contact in it is vetoed, then settled by the category.
	junk := gf{"junk", checklist.Contact, finding.Risk, KindReview, "", "setting up the login is next to impossible"}.f()
	gs, acts = run(all(grade(checklist.Contact, checklist.Flag, "junk")), []finding.Finding{kidsFact("0–5"), junk}, 5)
	want(t, gs, acts, checklist.Contact, checklist.OK, RuleContactNoStranger, RuleContactKids)
}

func TestKidsCategoryFactIsHiddenFromTheJudge(t *testing.T) {
	user := judgeUser(gateTitle, 5, []checklist.Area{checklist.Contact}, []finding.Finding{kidsFact("0–5")}, nil)
	if strings.Contains(user, "Made For Ages") {
		t.Errorf("the judge's prompt carries the Kids category fact:\n%s", user)
	}
}

// ---------------------------------------------------------------- content severity

func contentSet(rating string, extra ...gf) []finding.Finding {
	fs := findings(extra...)
	if rating != "" {
		fs = append(fs, finding.Finding{ID: "rating", Area: checklist.Content, Stance: finding.Neutral, Quote: "Age rating: " + rating,
			Claim: "rating", Source: finding.Source{Kind: KindStore, URL: "https://apps.apple.com/us/app/sampleland/id1"}})
	}
	return fs
}

func TestContentSingleMildReport(t *testing.T) {
	mild := gf{"mild", checklist.Content, finding.Risk, KindReview, "https://itunes.example/review/1", "A grid of scary devil faces popped up while my kid was working on math."}
	fine := gf{"fine", checklist.Content, finding.Neutral, KindReview, "https://itunes.example/review/2", "My daughter loves the calm lessons and songs."}
	// One mild report, rating at or below the age, a non-risk finding to cite: ok.
	gs, acts := run(all(grade(checklist.Content, checklist.Flag, "mild")), contentSet("4+", mild, fine), 5)
	g := want(t, gs, acts, checklist.Content, checklist.OK, RuleContentSingleMild)
	if !slices.Equal(g.Findings, []string{"rating", "fine"}) || !strings.Contains(g.Reason, "4+") {
		t.Errorf("cap = %+v", g)
	}
	// Nothing but the rating to cite beside it: unknown, never an invented ok.
	gs, acts = run(all(grade(checklist.Content, checklist.Flag, "mild")), contentSet("4+", mild), 5)
	g = want(t, gs, acts, checklist.Content, checklist.Unknown, RuleContentSingleMild)
	if len(g.Findings) != 0 {
		t.Errorf("unknown cites %v", g.Findings)
	}
	// Two independent mild reports still flag.
	mild2 := gf{"mild2", checklist.Content, finding.Risk, KindReview, "https://itunes.example/review/3", "Creepy music and a scary clown made my son cry."}
	gs, acts = run(all(grade(checklist.Content, checklist.Flag, "mild")), contentSet("4+", mild, mild2, fine), 5)
	want(t, gs, acts, checklist.Content, checklist.Flag, RuleContentConfirmed)
	// Two findings from one source are one report.
	same := gf{"same", checklist.Content, finding.Risk, KindReview, "https://itunes.example/review/1", "It was disturbing and creepy."}
	gs, acts = run(all(grade(checklist.Content, checklist.Flag, "mild", "same")), contentSet("4+", mild, same, fine), 5)
	want(t, gs, acts, checklist.Content, checklist.OK, RuleContentSingleMild)
	// A rating above the child's age, or none, leaves the flag.
	gs, acts = run(all(grade(checklist.Content, checklist.Flag, "mild")), contentSet("12+", mild, fine), 5)
	want(t, gs, acts, checklist.Content, checklist.Flag, RuleContentConfirmed)
	gs, acts = run(all(grade(checklist.Content, checklist.Flag, "mild")), contentSet("", mild, fine), 5)
	want(t, gs, acts, checklist.Content, checklist.Flag, RuleContentConfirmed)
	// Mild peril in the cartoon register is mild, not serious violence.
	cartoon := gf{"cartoon", checklist.Content, finding.Risk, KindReview, "https://itunes.example/review/4", "Some mild cartoon violence when the characters fall over."}
	gs, acts = run(all(grade(checklist.Content, checklist.Flag, "cartoon")), contentSet("4+", cartoon, fine), 5)
	want(t, gs, acts, checklist.Content, checklist.OK, RuleContentSingleMild)
}

// A serious report decides the flag whatever the rating and however many
// sources there are. The brief's moment still decides: one first-hand report
// of a predator asking a young child, here on a 4+ title.
func TestContentSeriousReportStillFlags(t *testing.T) {
	fine := gf{"fine", checklist.Content, finding.Neutral, KindReview, "https://itunes.example/review/2", "My daughter loves the calm lessons and songs."}
	for _, q := range []string{
		"if your kid plays Sampleland high chance a child predator might ask where you are",
		"my 7 year old was shown realistic violence and blood",
		"a stranger sent sexual messages to my 7 year old",
		"kids are bullied and harassed in the lobby",
		"there is a casino mini game for children",
	} {
		serious := gf{"serious", checklist.Content, finding.Risk, KindReview, "https://itunes.example/review/9", q}
		mild := gf{"mild", checklist.Content, finding.Risk, KindReview, "https://itunes.example/review/1", "A scary face popped up once."}
		gs, acts := run(all(grade(checklist.Content, checklist.Flag, "serious", "mild")), contentSet("4+", serious, mild, fine), 7)
		want(t, gs, acts, checklist.Content, checklist.Flag, RuleContentConfirmed)
		gs, acts = run(all(grade(checklist.Content, checklist.Flag, "serious")), contentSet("4+", serious, fine), 7)
		want(t, gs, acts, checklist.Content, checklist.Flag, RuleContentConfirmed)
	}
}

// ---------------------------------------------------------------- controls read from the text

func TestControlsAreReadFromTheTextNotTheStance(t *testing.T) {
	risk := gf{"risk", checklist.Money, finding.Risk, KindReview, "", "my son bought a lot of coins without asking me"}
	for _, q := range []string{
		"You can require approval for purchases in the parental settings.",
		"Parents can set a monthly spending limit for the account.",
		"Turn on Ask to Buy and every purchase needs a parent's approval.",
	} {
		for _, st := range []finding.Stance{finding.Mitigation, finding.Neutral, finding.Risk} {
			ctl := gf{"ctl", checklist.Money, st, KindPolicy, "", q}
			gs, acts := run(all(grade(checklist.Money, checklist.Flag, "risk")), findings(risk, ctl), 9)
			want(t, gs, acts, checklist.Money, checklist.OKWithSetting, RuleMoneyCapped)
		}
	}
	ctl := func(q string) gf { return gf{"ctl", checklist.Money, finding.Risk, KindPolicy, "", q} }
	for _, c := range []struct{ name, q string }{
		{"the same quote says it fails", "Kids bypass Ask to Buy by using the saved password."},
		{"the quote says it is missing", "There is no way to require approval for purchases."},
		{"the quote says it is not offered", "Sampleland does not offer a spending limit."},
	} {
		gs, _ := run(all(grade(checklist.Money, checklist.Flag, "risk")), findings(risk, ctl(c.q)), 9)
		if g := byArea(gs, checklist.Money); g.Level != checklist.Flag {
			t.Errorf("%s: money = %s, want flag", c.name, g.Level)
		}
	}
	// A generic page heading about spending, naming no title, is not a control.
	esrb := gf{"ctl", checklist.Money, finding.Mitigation, KindRating, "https://www.esrb.org/tools-for-parents/parental-controls", "You can require approval for purchases"}
	gs, acts := run(all(grade(checklist.Money, checklist.Flag, "risk")), findings(risk, esrb), 9)
	want(t, gs, acts, checklist.Money, checklist.Flag, RuleMoneyConfirmed)
	// The same words on a page that names the title are a control.
	named := gf{"ctl", checklist.Money, finding.Risk, KindRating, "https://www.esrb.org/sampleland-parental-controls", "You can require approval for purchases"}
	gs, acts = run(all(grade(checklist.Money, checklist.Flag, "risk")), findings(risk, named), 9)
	want(t, gs, acts, checklist.Money, checklist.OKWithSetting, RuleMoneyCapped)
	// An area risk quote about the title that reports the control failing keeps the flag.
	fail := gf{"fail", checklist.Money, finding.Risk, KindReview, "", "he bought coins even when ask to buy was on"}
	gs, acts = run(all(grade(checklist.Money, checklist.Flag, "risk")), findings(risk, ctl("Turn on Ask to Buy for every purchase."), fail), 9)
	want(t, gs, acts, checklist.Money, checklist.Flag, RuleMoneyConfirmed)
	// Age gating reads its control from the text too.
	arisk := gf{"risk", checklist.AgeGating, finding.Risk, KindReview, "", "my 8 year old entered a fake birthday and got in"}
	actl := gf{"ctl", checklist.AgeGating, finding.Risk, KindPolicy, "", "A child's birthday is locked and only a parent can change it."}
	gs, acts = run(all(grade(checklist.AgeGating, checklist.Flag, "risk")), findings(arisk, actl), 8)
	want(t, gs, acts, checklist.AgeGating, checklist.OKWithSetting, RuleAgeGateCapped)
}
