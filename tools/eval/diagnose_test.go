package main

import (
	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/finding"
	"github.com/nrynss/before-yes/internal/pipeline"
	"strings"
	"testing"
)

func TestDiagnoseUsesCheckedSnapshotRatherThanParentProse(t *testing.T) {
	card := pipeline.Card{Title: "Any title", Age: 9, Grades: []checklist.Grade{{Area: checklist.Money, Level: checklist.Unknown, Reason: "We do not have enough evidence."}}}
	tr := &callTrace{}
	tr.captureCheckedReasons(map[checklist.Area]string{checklist.Money: pipeline.NotEarnedReason})
	var out strings.Builder
	diagnose(&out, card, pipeline.Evidence{}, tr)
	if !strings.Contains(out.String(), "check entail: flag_not_earned") {
		t.Fatal(out.String())
	}
	if !strings.Contains(out.String(), "reason We do not have enough evidence.") {
		t.Fatal(out.String())
	}
}

// An area the conflict pass answered, which the checks made unknown and the
// first pass's grade then replaced, is labelled a first-pass fallback, not
// "conflict pass replaced".
func TestDiagnoseLabelsFirstPassFallback(t *testing.T) {
	f := finding.Finding{ID: "f_1", Area: checklist.Money, Stance: finding.Risk, Quote: "a quote about paying real money", Source: finding.Source{Kind: pipeline.KindReview, Index: 3}}
	card := pipeline.Card{
		Title: "Any title", Age: 9,
		Grades: []checklist.Grade{{Area: checklist.Money, Level: checklist.Flag, Findings: []string{"f_1"}}},
		Stats:  pipeline.Stats{ConflictedAreas: []checklist.Area{checklist.Money}},
	}
	tr := &callTrace{judge: []traced{
		{reply: `{"grades":[{"area":"money","level":"flag","findings":["f_1"]}]}`},
		{reply: `{"grades":[{"area":"money","level":"ok","findings":["f_nope"]}]}`},
	}}
	var out strings.Builder
	diagnose(&out, card, pipeline.Evidence{Findings: []finding.Finding{f}}, tr)
	got := out.String()
	for _, want := range []string{"first-pass fallback", "conflict pass ok", "judge (first pass) flag"} {
		if !strings.Contains(got, want) {
			t.Errorf("diagnose lacks %q:\n%s", want, got)
		}
	}
}
