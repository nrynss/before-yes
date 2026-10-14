package main

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/nrynss/keel/cost"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/finding"
	"github.com/nrynss/before-yes/internal/model"
)

// The stand-ins below are deterministic and offline. They are not Nano or
// Super and say nothing about either model's quality. They exist so the eval
// runs end to end before T3.6b records real replies, and so the numbers they
// produce are a floor any real model should beat: a keyword reader and a
// judge that reads only stances.

// Token Factory's published rates from before-yes.toml.example, in dollars
// per million tokens, so a stand-in run projects what the same prompts would
// cost there. The token count is the model client's estimate, three bytes a
// token.
var (
	nanoRates  = [2]float64{0.06, 0.24}
	superRates = [2]float64{0.30, 0.90}
)

func project(rates [2]float64, in, out int) cost.Price {
	d := (float64(in)*rates[0] + float64(out)*rates[1]) / 1e6
	return cost.Price(d * 1e9)
}

func msgBytes(msgs []model.Message) int {
	n := 0
	for _, m := range msgs {
		n += len(m.Content)
	}
	return n
}

// keywordReader quotes every sentence of 10 to 40 words that names an area's
// keyword, with a stance from a second keyword list.
type keywordReader struct{}

var segHead = regexp.MustCompile(`(?m)^\[([RD]\d+)\] \([^\n]*\)\n`)

var areaWords = []struct {
	area  checklist.Area
	words []string
}{
	{checklist.Contact, []string{"stranger", "predator", "chat", "message", "friend request", "groom", "talk to"}},
	{checklist.Money, []string{"robux", "purchase", "spend", "spent", "in-app", "subscription", "ads", "credit card", "pay "}},
	{checklist.Data, []string{"personal data", "personal information", "privacy", "track", "third parties", "collect"}},
	{checklist.AgeGating, []string{"age check", "age verification", "under 13", "parental consent", "child account", "ages "}},
	{checklist.Content, []string{"inappropriate", "violence", "horror", "sexual", "swear", "scary"}},
	{checklist.DeveloperRecord, []string{"lawsuit", "sued", "fined", "ftc", "settlement", "breach", "regulator"}},
}

var mitigationWords = []string{"turn off", "turning it off", "parental control", "friends only", "ask to buy", "child account", "disable", "restrict", "manage", "no ads", "don't let", "free"}
var riskWords = []string{"predator", "stranger", "inappropriate", "scam", "lawsuit", "sued", "fined", "breach", "horror", "spent", "charged", "without consent", "e-dater", "instead of"}

func (keywordReader) Chat(_ context.Context, msgs []model.Message, _ model.Options) (model.Reply, error) {
	user := msgs[len(msgs)-1].Content
	heads := segHead.FindAllStringSubmatchIndex(user, -1)
	raws := []finding.Raw{}
	for i, h := range heads {
		ref := user[h[2]:h[3]]
		end := len(user)
		if i+1 < len(heads) {
			end = heads[i+1][0]
		}
		n := 0
		for _, s := range sentences(user[h[1]:end]) {
			if n == 2 {
				break
			}
			low := strings.ToLower(s)
			for _, a := range areaWords {
				if !anyOf(low, a.words) {
					continue
				}
				stance := finding.Neutral
				switch {
				case anyOf(low, riskWords):
					stance = finding.Risk
				case anyOf(low, mitigationWords):
					stance = finding.Mitigation
				}
				raws = append(raws, finding.Raw{Ref: ref, Area: string(a.area), Stance: string(stance), Claim: "A source says: " + clip(s, 12), Quote: s})
				n++
				break
			}
		}
	}
	b, _ := json.Marshal(map[string]any{"findings": raws})
	return model.Reply{Text: string(b), Price: project(nanoRates, msgBytes(msgs)/3, len(b)/3), Measured: true}, nil
}

func (keywordReader) Estimate(msgs []model.Message, _ model.Options) cost.Price {
	return project(nanoRates, msgBytes(msgs)/3, model.DefaultMaxTokens)
}
func (keywordReader) Settings() model.Settings {
	return model.Settings{Provider: "standin", Model: "standin-keyword-reader"}
}

func anyOf(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// sentences cuts text after . ! ? and at line breaks, and keeps the pieces
// of 10 to 40 words, a longer one cut to its first 40 words.
func sentences(text string) []string {
	var out []string
	start := 0
	emit := func(end int) {
		s := strings.TrimSpace(text[start:end])
		start = end
		ws := strings.Fields(s)
		if len(ws) < 10 {
			return
		}
		if len(ws) > 40 {
			ws = ws[:40]
		}
		out = append(out, strings.Join(ws, " "))
	}
	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case c == '\n':
			emit(i + 1)
		case (c == '.' || c == '!' || c == '?') && (i+1 == len(text) || text[i+1] == ' '):
			emit(i + 1)
		}
	}
	emit(len(text))
	return out
}

func clip(s string, words int) string {
	ws := strings.Fields(s)
	if len(ws) > words {
		ws = append(ws[:words], "...")
	}
	return strings.Join(ws, " ")
}

// stanceJudge grades from the stances in its prompt alone: a risk flags the
// area, a mitigation alone makes it ok with a setting, neutral findings alone
// make it ok, and no finding leaves it unknown.
type stanceJudge struct{}

var (
	judgeLine = regexp.MustCompile(`(?m)^- id=(\S+) area=(\S+) stance=(\S+) `)
	onlyLine  = regexp.MustCompile(`(?m)^Grade only these areas: (.+)$`)
)

func (stanceJudge) Chat(_ context.Context, msgs []model.Message, _ model.Options) (model.Reply, error) {
	user := msgs[len(msgs)-1].Content
	var areas []checklist.Area
	if m := onlyLine.FindStringSubmatch(user); m != nil {
		for _, a := range strings.Split(m[1], ", ") {
			areas = append(areas, checklist.Area(a))
		}
	} else {
		for _, a := range checklist.Areas() {
			areas = append(areas, a.ID)
		}
	}
	by := map[string]map[string][]string{}
	for _, m := range judgeLine.FindAllStringSubmatch(user, -1) {
		if by[m[2]] == nil {
			by[m[2]] = map[string][]string{}
		}
		by[m[2]][m[3]] = append(by[m[2]][m[3]], m[1])
	}
	gs := []checklist.Grade{}
	for _, a := range areas {
		s := by[string(a)]
		g := checklist.Grade{Area: a, Level: checklist.Unknown, Findings: []string{}, Reason: "No finding bears on this area."}
		switch {
		case len(s["risk"]) > 0:
			g = checklist.Grade{Area: a, Level: checklist.Flag, Findings: s["risk"], Reason: "A risk finding stands."}
		case len(s["mitigation"]) > 0:
			g = checklist.Grade{Area: a, Level: checklist.OKWithSetting, Setting: "The control the cited finding names", Findings: s["mitigation"], Reason: "A control answers it."}
		case len(s["neutral"]) > 0:
			g = checklist.Grade{Area: a, Level: checklist.OK, Findings: s["neutral"], Reason: "Nothing calls for action."}
		}
		gs = append(gs, g)
	}
	b, _ := json.Marshal(map[string]any{"grades": gs})
	return model.Reply{Text: string(b), Price: project(superRates, msgBytes(msgs)/3, len(b)/3), Measured: true}, nil
}

func (stanceJudge) Estimate(msgs []model.Message, _ model.Options) cost.Price {
	return project(superRates, msgBytes(msgs)/3, 16000)
}
func (stanceJudge) Settings() model.Settings {
	return model.Settings{Provider: "standin", Model: "standin-stance-judge"}
}
