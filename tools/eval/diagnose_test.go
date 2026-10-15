package main

import (
	"github.com/nrynss/before-yes/internal/checklist"
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
