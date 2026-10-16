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

// reasonFallbacks are the grades whose parent reason is the runtime fallback
// in the re-recorded sets: the writer's reply for each was rejected by the
// support check, so the card carries the fallback text. Keyed "<gather>
// <title> <area>". Any other standing grade must carry a written reason, and
// these must still be fallbacks, so a change in either direction is seen.
// (Before the re-record no standing grade fell back.)
var reasonFallbacks = map[string]bool{
	"0 Roblox money":            true,
	"0 Roblox age_gating":       true,
	"0 Roblox content":          true,
	"0 Roblox developer_record": true,
	"1 Minecraft content":       true,
	"2 Roblox contact":          true,
}

// Final examples must explain their source evidence, not merely preserve a
// correct verdict while quietly using the runtime fallback, except for the
// pinned reasonFallbacks.
func TestPublishedParentReasonsRegression(t *testing.T) {
	oldGather, oldCases, oldOnly := *gatherN, *reasonCases, *liveOnly
	defer func() { *gatherN = oldGather; *reasonCases = oldCases; *liveOnly = oldOnly }()
	*reasonCases = ""
	*liveOnly = ""
	root := filepath.Join("..", "..")
	for n := 0; n < 3; n++ {
		*gatherN = n
		got, err := evaluate(root, "replay")
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range got {
			if r.Err != "" {
				t.Fatal(r.Err)
			}
			for _, g := range r.Grades {
				if g.Level == "unknown" {
					continue
				}
				fell := strings.HasPrefix(g.Reason, "The cited sources raise") || strings.Contains(g.Reason, "couldn’t verify a plain-language")
				key := fmt.Sprintf("%d %s %s", n, r.Title, g.Area)
				if fell != reasonFallbacks[key] {
					t.Fatalf("%s: fallback = %v, pinned %v (%q)", key, fell, reasonFallbacks[key], g.Reason)
				}
				if fell {
					continue
				}
				// Khan gather 1 data: the judge cites a news hit about another
				// app (the FTC and Weight Watchers' Kurbo); the reason says
				// what that quote says. Before: it named COPPA and verification.
				if n == 1 && r.Title == "Khan Academy Kids" && g.Area == "data" {
					if !strings.Contains(strings.ToLower(g.Reason), "regulators") || !strings.Contains(strings.ToLower(g.Reason), "consent") {
						t.Fatal(g.Reason)
					}
				}
				if n == 2 && r.Title == "Minecraft" && g.Area == "money" && !strings.Contains(g.Reason, "10,000 Minecoins") {
					t.Fatal(g.Reason)
				}
				if n == 2 && r.Title == "Roblox" {
					switch g.Area {
					case "money":
						if (!strings.Contains(g.Reason, "$7,000") && !strings.Contains(g.Reason, "$46")) || strings.Contains(g.Reason, "despite purchase limits") {
							t.Fatal(g.Reason)
						}
					case "data":
						if !strings.Contains(strings.ToLower(g.Reason), "third-party") || !strings.Contains(g.Reason, "retention") {
							t.Fatal(g.Reason)
						}
					case "age_gating":
						// Before: a 41-year-old and "younger than 13". Now the
						// four-year-old who was assigned twenty five.
						if !strings.Contains(g.Reason, "four year old") || !strings.Contains(g.Reason, "twenty five") || strings.Contains(g.Reason, "28th") || strings.Contains(g.Reason, "due to") {
							t.Fatal(g.Reason)
						}
					case "content":
						if !strings.Contains(g.Reason, "Street Code") || strings.Contains(g.Reason, "possible") || strings.Contains(g.Reason, "likely") {
							t.Fatal(g.Reason)
						}
					}
				}
			}
		}
	}
	*gatherN = 0
	*reasonCases = "testdata/eval/parent-reasons/outside-cases.json"
	got, err := evaluateReasonCases(root, "replay")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range got {
		if r.Err != "" {
			t.Fatal(r.Err)
		}
		if r.Title != "Scratch" {
			continue
		}
		for _, g := range r.Grades {
			if g.Area == "money" {
				if !strings.Contains(g.Reason, "free") || strings.Contains(g.Reason, "no mention") || len(strings.Fields(g.Reason)) > 30 {
					t.Fatal(g.Reason)
				}
			}
		}
	}
}
