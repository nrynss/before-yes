package main

import (
	"context"
	"encoding/json"
	"github.com/nrynss/before-yes/internal/checklist"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/before-yes/internal/appstore"
	"github.com/nrynss/before-yes/internal/finding"
	"github.com/nrynss/before-yes/internal/pipeline"
	"github.com/nrynss/before-yes/internal/spend"
	"github.com/nrynss/before-yes/internal/tavily"
)

const root = "../.."

// Every quote an expectation calls decisive must be in the text the
// gatherer collects from the fixtures, so a miss is the reader's or the
// judge's, never an expectation that no pipeline could meet.
func TestExpectationsAreReachable(t *testing.T) {
	exps, err := loadExpectations(filepath.Join(root, "testdata", "eval"))
	if err != nil {
		t.Fatal(err)
	}
	if len(exps) != 3 {
		t.Fatalf("%d expectations, want the three demo titles", len(exps))
	}
	for _, e := range exps {
		docs := gather(t, e)
		areas := map[string]bool{}
		for _, a := range e.Areas {
			areas[string(a.Area)] = true
			for _, q := range append(append([]Quote{}, a.Decisive...), a.Supporting...) {
				if !inDocs(docs, q) {
					t.Errorf("%s %s: quote from %s is not in the gathered text: %q", e.Slug, a.Area, q.Source, q.Quote)
				}
			}
		}
		if len(areas) != 6 {
			t.Errorf("%s: %d areas, want six", e.Slug, len(areas))
		}
	}
}

func inDocs(docs []pipeline.Doc, q Quote) bool {
	want := finding.Normalize(q.Quote)
	for _, d := range docs {
		if sameSource(d.Source, q.Source) && strings.Contains(finding.Normalize(d.Text), want) {
			return true
		}
	}
	return false
}

func gather(t *testing.T, e Expectation) []pipeline.Doc {
	t.Helper()
	_, srv := serveFixture(filepath.Join(root, "testdata", "titles", e.Slug), e.Title)
	t.Cleanup(srv.Close)
	m, err := spend.Open(context.Background(), spend.Config{Path: filepath.Join(t.TempDir(), "s.db"), Pools: map[spend.Pool]spend.PoolConfig{spend.PoolTavily: {Limit: 1_000_000}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	tc := tavily.New(srv.URL, func() (string, error) { return "offline", nil })
	tc.Retry.PerMinute, tc.Retry.Name = 0, ""
	tc.Retry.Sleep = func(context.Context, time.Duration) error { return nil }
	g := &pipeline.Gatherer{Store: publicFeed{appstore.New(srv.URL, "us"), appstore.New("", "us")}, Search: tc, Meter: m}
	got, err := g.Gather(context.Background(), e.Title, "t")
	if err != nil {
		t.Fatal(err)
	}
	return got.Docs
}

// The eval runs offline end to end with the stand-ins and reports every
// title. The numbers are pinned loosely: a change to the plan, the prompts
// or the stand-ins shows up here and in the baseline.
func TestEvalStandinOffline(t *testing.T) {
	rs, err := evaluate(root, "standin")
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 3 {
		t.Fatalf("%d results", len(rs))
	}
	for _, r := range rs {
		if r.Err != "" {
			t.Errorf("%s: %s", r.Title, r.Err)
		}
		if len(r.Areas) != 6 || r.Chunks == 0 || r.Credits == 0 || r.ReadNano == 0 || r.JudgeNano == 0 {
			t.Errorf("%s: %+v", r.Title, r)
		}
	}
	var sb strings.Builder
	report(&sb, rs)
	for _, want := range []string{"Roblox, age 9", "Minecraft, age 9", "Khan Academy Kids, age 5", "TOTAL grades agree"} {
		if !strings.Contains(sb.String(), want) {
			t.Errorf("report misses %q", want)
		}
	}
}

// Replay mode with no recordings fails every call, and says so, rather
// than quietly falling back to the stand-ins.
func TestReplayWithoutRecordingsFails(t *testing.T) {
	for _, slug := range []string{"roblox", "minecraft", "khan-academy-kids"} {
		if _, err := os.Stat(filepath.Join(root, "testdata", "eval", "replies", slug)); err == nil {
			t.Skip("recordings exist")
		}
	}
	rs, err := evaluate(root, "replay")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if r.Err == "" {
			t.Errorf("%s: replay with no recordings did not fail", r.Title)
		}
	}
}

// A decisive quote is found only by a quote that holds it, or a stretch of
// it with at least quoteCover of its words. Twelve characters of a long
// quote is not finding it.
func TestFoundQuoteNeedsCover(t *testing.T) {
	want := Quote{Source: "R355", Quote: "if your kid plays Roblox high chance a child predator might ask where you are"}
	f := func(q string) []finding.Finding {
		return []finding.Finding{{Area: checklist.Contact, Quote: q, Source: finding.Source{Kind: pipeline.KindReview, Index: 355}}}
	}
	for q, ok := range map[string]bool{
		"Mom here: if your kid plays Roblox high chance a child predator might ask where you are.": true,
		"plays Roblox high chance a child predator might ask where you are":                        true, // 12 of 15 words
		"if your kid plays Roblox": false, // 5 of 15
		"a child predator":         false,
	} {
		if got := foundQuote(f(q), checklist.Contact, want); got != ok {
			t.Errorf("foundQuote(%q) = %v, want %v", q, got, ok)
		}
	}
	if foundQuote(f(want.Quote), checklist.Money, want) {
		t.Error("a quote in another area counted")
	}
}

// The replay misses report names the role and the count.
func TestReplayMissesReported(t *testing.T) {
	r := &replayer{role: "reader"}
	r.misses.Add(2)
	r.hits.Add(3)
	if msg := replayMisses(r, keywordReader{}); !strings.Contains(msg, "reader 2 of 5 calls") {
		t.Errorf("replayMisses = %q", msg)
	}
	if msg := replayMisses(&replayer{role: "judge"}); msg != "" {
		t.Errorf("no misses reported %q", msg)
	}
}

// The stand-in baseline, pinned. A change to the gatherer, reader,
// judge, scoring or fixtures that moves it must update this table and the
// T3.6 notes together. T3.10 moved only Khan's chunks (22 to 19): the
// off-product filter drops its ten sibling pages, and the entailment check,
// answered by the stand-in reader with {"findings": []}, keeps every grade
// (an unparseable entailment reply never demotes). The shorter-sibling
// rule (a page naming "Khan Academy" and never "Khan Academy Kids") moved
// only Khan's chunks again (19 to 13): five more pages leave the reading.
func TestStandinBaselinePinned(t *testing.T) {
	rs, err := evaluate(root, "standin")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][4]int{ // agreed/6, decisive found/total, chunks
		"Roblox":            {4, 3, 6, 36},
		"Minecraft":         {0, 2, 5, 45},
		"Khan Academy Kids": {3, 1, 5, 13},
	}
	agreed, found, total := 0, 0, 0
	for _, r := range rs {
		w, ok := want[r.Title]
		if !ok {
			t.Errorf("unpinned title %q", r.Title)
			continue
		}
		if got := [4]int{r.Agreed, r.DecisiveHit, r.DecisiveAll, r.Chunks}; got != w {
			t.Errorf("%s: agreed, found, total, chunks = %v, pinned %v", r.Title, got, w)
		}
		agreed, found, total = agreed+r.Agreed, found+r.DecisiveHit, total+r.DecisiveAll
	}
	if agreed != 7 || found != 6 || total != 16 {
		t.Errorf("total grades %d/18, quotes %d/%d; pinned 7/18, 6/16", agreed, found, total)
	}
}

// Replay mode names the missing recordings directory, rather than scoring
// every chunk as failed.
func TestReplayMissingDirIsClear(t *testing.T) {
	if _, err := os.Stat(filepath.Join(root, "testdata", "eval", "replies", "roblox")); err == nil {
		t.Skip("recordings exist")
	}
	for _, r := range mustEval(t, "replay") {
		if !strings.Contains(r.Err, "no recordings for "+r.Title) {
			t.Errorf("%s: err %q", r.Title, r.Err)
		}
	}
}

func mustEval(t *testing.T, models string) []Result {
	t.Helper()
	rs, err := evaluate(root, models)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

// Gather 2 must not keep an unearned setting, and a standing reason must
// not be an ads sentence. Minecraft age gating, where a child account does
// not stop a child who gets past the age check, does not stand. Roblox
// data stands on the image-retention quote; its reason is the area
// definition and does not say ads. Content and money stay flags and do
// not keep the model's extra harms. Minecraft's money flag is earned and
// the verdict stays no.
func TestGather2DoesNotKeepUnearnedSettings(t *testing.T) {
	prevG, prevOnly := *gatherN, *liveOnly
	t.Cleanup(func() {
		*gatherN = prevG
		*liveOnly = prevOnly
	})
	*gatherN = 2

	*liveOnly = "minecraft"
	mc := mustEval(t, "replay")
	if len(mc) != 1 || mc[0].Err != "" {
		t.Fatalf("minecraft replay: %+v", mc)
	}
	if g := areaGot(t, mc[0], checklist.AgeGating); g.Got == checklist.OKWithSetting {
		t.Errorf("age gating stayed ok_with_setting; the cited control does not close the hole")
	}
	if g := areaGot(t, mc[0], checklist.Money); g.Got != checklist.Flag {
		t.Errorf("money = %s, want the earned flag", g.Got)
	}
	if mc[0].GotVerdict != checklist.No {
		t.Errorf("minecraft verdict %s, want no", mc[0].GotVerdict)
	}

	*liveOnly = "roblox"
	rb := mustEval(t, "replay")
	if len(rb) != 1 || rb[0].Err != "" {
		t.Fatalf("roblox replay: %+v", rb)
	}
	data := areaGot(t, rb[0], checklist.Data)
	if data.Got != checklist.Flag {
		t.Errorf("data = %s, want the earned flag", data.Got)
	}
	if strings.Contains(strings.ToLower(data.GotNote), "ads") {
		t.Errorf("data reason says ads: %q", data.GotNote)
	}
	content := areaGot(t, rb[0], checklist.Content)
	if content.Got != checklist.Flag {
		t.Errorf("content = %s, want the earned flag", content.Got)
	}
	for _, w := range []string{"horror", "self-harm"} {
		if strings.Contains(strings.ToLower(content.GotNote), w) {
			t.Errorf("content reason says %q: %q", w, content.GotNote)
		}
	}
	money := areaGot(t, rb[0], checklist.Money)
	if money.Got != checklist.Flag {
		t.Errorf("money = %s, want the earned flag", money.Got)
	}
	for _, w := range []string{"loot boxes", "pay-to-win", "pay‑to‑win"} {
		if strings.Contains(strings.ToLower(money.GotNote), w) {
			t.Errorf("money reason says %q: %q", w, money.GotNote)
		}
	}
	if rb[0].GotVerdict != checklist.No {
		t.Errorf("roblox verdict %s, want no", rb[0].GotVerdict)
	}
}

func areaGot(t *testing.T, r Result, area checklist.Area) AreaResult {
	t.Helper()
	for _, a := range r.Areas {
		if a.Area == area {
			return a
		}
	}
	t.Fatalf("no %s grade", area)
	return AreaResult{}
}

// These replays independently exercise the published decision on captured
// evidence. Generic tracking and curriculum placement cannot cause refusal,
// while real birthdate bypass and incorrect account ages retain their flags.
func TestGatherFlagsRequireTheirOwnDefinition(t *testing.T) {
	prevG, prevOnly := *gatherN, *liveOnly
	t.Cleanup(func() { *gatherN = prevG; *liveOnly = prevOnly })
	for _, tc := range []struct {
		gather  int
		slug    string
		area    checklist.Area
		want    checklist.Level
		verdict checklist.Verdict
	}{
		// Re-recorded: the verdict was unclear (content unknown); now money
		// is an earned flag in gather 1, so the verdict is no.
		{1, "minecraft", checklist.Data, checklist.Unknown, checklist.No},
		{2, "khan-academy-kids", checklist.AgeGating, checklist.Unknown, checklist.Unclear},
		{1, "roblox", checklist.AgeGating, checklist.Flag, checklist.No},
		{2, "roblox", checklist.AgeGating, checklist.Flag, checklist.No},
	} {
		*gatherN = tc.gather
		*liveOnly = tc.slug
		rs := mustEval(t, "replay")
		if len(rs) != 1 || rs[0].Err != "" {
			t.Fatalf("replay %s/%d: %+v", tc.slug, tc.gather, rs)
		}
		if a := areaGot(t, rs[0], tc.area); a.Got != tc.want {
			t.Errorf("%s/%d %s=%s want %s", tc.slug, tc.gather, tc.area, a.Got, tc.want)
		}
		if rs[0].GotVerdict != tc.verdict {
			t.Errorf("%s/%d verdict=%s want %s", tc.slug, tc.gather, rs[0].GotVerdict, tc.verdict)
		}
	}
}

// Remove the independent bypass citation, keeping the actual recorded model
// answer on the conditional correction source. This catches a false flag that
// an unchanged verdict in the complete fixture would otherwise conceal.
func TestConditionalCorrectionAloneDoesNotEarnAgeFlag(t *testing.T) {
	prevG, prevOnly, prevDiagnosis := *gatherN, *liveOnly, *diagnoseOn
	t.Cleanup(func() { *gatherN = prevG; *liveOnly = prevOnly; *diagnoseOn = prevDiagnosis })
	*gatherN, *liveOnly = 0, "roblox"
	*diagnoseOn = true
	ablation := t.TempDir()
	for _, dir := range []string{"testdata/eval", "testdata/titles/roblox"} {
		if err := os.CopyFS(filepath.Join(ablation, dir), os.DirFS(filepath.Join(root, dir))); err != nil {
			t.Fatal(err)
		}
	}
	// Both judge passes cite the independent bypass report: the first pass
	// (cdd22b54...) among four ids, the conflict pass (3f9336f3...) alone.
	// Each is rewritten to cite only the conditional correction.
	for _, name := range []string{"cdd22b54e09097d8fa7b5856", "3f9336f3345293c4d983e91a"} {
		path := filepath.Join(ablation, "testdata/eval/replies/roblox/judge", name+".json")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var rec Recording
		if err := json.Unmarshal(b, &rec); err != nil {
			t.Fatal(err)
		}
		var reply struct {
			Grades []rawGrade `json:"grades"`
		}
		if err := json.Unmarshal([]byte(rec.Text), &reply); err != nil {
			t.Fatal(err)
		}
		changed := false
		for i := range reply.Grades {
			if reply.Grades[i].Area == "age_gating" {
				reply.Grades[i].Findings = []string{"f_3832ce49d8ba54a2"}
				changed = true
			}
		}
		if !changed {
			t.Fatalf("recorded age grade not found in %s", name)
		}
		b, err = json.Marshal(reply)
		if err != nil {
			t.Fatal(err)
		}
		rec.Text = string(b)
		b, err = json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	log, err := os.CreateTemp(t.TempDir(), "diagnosis")
	if err != nil {
		t.Fatal(err)
	}
	previousStderr := os.Stderr
	os.Stderr = log
	t.Cleanup(func() { os.Stderr = previousStderr; _ = log.Close() })
	rs, err := evaluate(ablation, "replay")
	os.Stderr = previousStderr
	if err != nil || len(rs) != 1 || rs[0].Err != "" {
		t.Fatalf("replay: %+v %v", rs, err)
	}
	trace, err := os.ReadFile(log.Name())
	if err != nil {
		t.Fatal(err)
	}
	if a := areaGot(t, rs[0], checklist.AgeGating); a.Got != checklist.Unknown {
		t.Fatalf("conditional correction alone earned %s:\n%s", a.Got, trace)
	}
	if !strings.Contains(string(trace), `age-gate event=none witness=""`) {
		t.Fatalf("actual saved none reply was not replayed: %s", trace)
	}
}

// A replay with an unrecorded entailment call is reported as a miss. The
// misses were once counted on the replayer but lost: runOne set Err in a
// deferred function on an unnamed result, so the caller never saw it and the
// run looked clean while the grade was kept unchecked. Here every entailment
// recording of the Roblox set is removed from a copy, and the replay must
// say so.
func TestReplayReportsUnrecordedEntailment(t *testing.T) {
	prevG, prevOnly := *gatherN, *liveOnly
	t.Cleanup(func() { *gatherN = prevG; *liveOnly = prevOnly })
	*gatherN, *liveOnly = 0, "roblox"
	tmp := t.TempDir()
	for _, dir := range []string{"testdata/eval", "testdata/titles/roblox"} {
		if err := os.CopyFS(filepath.Join(tmp, dir), os.DirFS(filepath.Join(root, dir))); err != nil {
			t.Fatal(err)
		}
	}
	removed := 0
	files, _ := filepath.Glob(filepath.Join(tmp, "testdata/eval/replies/roblox/judge/*.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "meets_definition") {
			if err := os.Remove(f); err != nil {
				t.Fatal(err)
			}
			removed++
		}
	}
	if removed == 0 {
		t.Fatal("no entailment recordings found to remove")
	}
	rs, err := evaluate(tmp, "replay")
	if err != nil || len(rs) != 1 {
		t.Fatalf("replay: %+v %v", rs, err)
	}
	if !strings.Contains(rs[0].Err, "no recording for judge") {
		t.Errorf("Err = %q, want the replay miss with %d entailment recordings removed", rs[0].Err, removed)
	}
}
