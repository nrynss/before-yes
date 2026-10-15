package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestReasonCasesRejectUnverifiedSource(t *testing.T) {
	old := *reasonCases
	defer func() { *reasonCases = old }()
	*reasonCases = "cases.json"
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "source.json"), []byte(`{"results":[{"url":"https://example.org","raw_content":"The app is free."}]}`), 0600)
	os.WriteFile(filepath.Join(root, "cases.json"), []byte(`[{"title":"Outside title","slug":"outside","age":9,"source_file":"source.json","findings":[{"id":"f","area":"money","stance":"neutral","quote":"invented claim","source":{"url":"https://example.org","kind":"policy"}}]}]`), 0600)
	if _, err := evaluateReasonCases(root, "replay"); err == nil || !strings.Contains(err.Error(), "unverified quote") {
		t.Fatalf("unverified source reached judge: %v", err)
	}
}

// Replay actual recorded writer/checker calls and compare all non-prose grade
// fields and the verified evidence digest with the pre-task calibrated judge.
func TestParentReasonsPreserveRecordedGradesAndEvidence(t *testing.T) {
	oldGather, oldCases, oldOnly := *gatherN, *reasonCases, *liveOnly
	defer func() { *gatherN = oldGather; *reasonCases = oldCases; *liveOnly = oldOnly }()
	*reasonCases = ""
	*liveOnly = ""
	root := filepath.Join("..", "..")
	for n := 0; n < 3; n++ {
		*gatherN = n
		beforeBytes, err := os.ReadFile(filepath.Join(root, "testdata", "eval", "parent-reasons", fmt.Sprintf("before-%d.json", n)))
		if err != nil {
			t.Fatal(err)
		}
		var before []Result
		if err = json.Unmarshal(beforeBytes, &before); err != nil {
			t.Fatal(err)
		}
		after, err := evaluate(root, "replay")
		if err != nil {
			t.Fatal(err)
		}
		assertReasonOnlyResults(t, before, after)
	}
	*gatherN = 0
	*reasonCases = "testdata/eval/parent-reasons/outside-cases.json"
	b, err := os.ReadFile(filepath.Join(root, "testdata", "eval", "parent-reasons", "before-outside.json"))
	if err != nil {
		t.Fatal(err)
	}
	var before []Result
	if err = json.Unmarshal(b, &before); err != nil {
		t.Fatal(err)
	}
	after, err := evaluateReasonCases(root, "replay")
	if err != nil {
		t.Fatal(err)
	}
	assertReasonOnlyResults(t, before, after)
}
func assertReasonOnlyResults(t *testing.T, before, after []Result) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("result count %d != %d", len(before), len(after))
	}
	for i, b := range before {
		a := after[i]
		if a.Err != "" {
			t.Fatalf("%s: %s", a.Title, a.Err)
		}
		if a.Title != b.Title || a.Age != b.Age || a.EvidenceHash != b.EvidenceHash || a.GotVerdict != b.GotVerdict {
			t.Fatalf("evidence/verdict drift %s: %+v %+v", b.Title, b, a)
		}
		bg, ag := slices.Clone(b.Grades), slices.Clone(a.Grades)
		for j := range bg {
			bg[j].Reason = ""
		}
		for j := range ag {
			ag[j].Reason = ""
		}
		if !reflect.DeepEqual(bg, ag) {
			t.Fatalf("non-prose grade drift %s: %+v %+v", b.Title, bg, ag)
		}
	}
}
