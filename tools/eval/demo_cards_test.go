package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/pipeline"
)

var updateDemo = flag.Bool("update", false, "rewrite testdata/demo-cards from the replay of gather 0")

// demoWant is the verdict each demo card must reach, by slug. The ages are
// the ones testdata/eval uses for each title.
var demoWant = map[string]checklist.Verdict{
	"roblox":            checklist.No,
	"minecraft":         checklist.YesWithSettings,
	"khan-academy-kids": checklist.Yes,
}

// demoCardPath is where a demo card is tracked, by slug.
func demoCardPath(slug string) string {
	return filepath.Join(root, "testdata", "demo-cards", slug+".json")
}

// storedForm is the card as the store keeps it, with an empty id. Decide
// mints a random id on every run, and the store replaces it when the card
// is seeded (before-yes seed-cards), so the tracked file leaves it empty.
func storedForm(c pipeline.Card) ([]byte, error) {
	c.ID = ""
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// TestDemoCardsMatchReplay recomputes the demo cards from the replay of
// gather 0 (the frozen testdata/titles and the recorded replies) and fails
// when a tracked file in testdata/demo-cards differs. With -update it
// rewrites the files, and it refuses a card whose verdict is not the
// expected one.
func TestDemoCardsMatchReplay(t *testing.T) {
	prev := *gatherN
	t.Cleanup(func() { *gatherN = prev })
	*gatherN = 0
	rs := mustEval(t, "replay")
	if len(rs) != len(demoWant) {
		t.Fatalf("replay gave %d titles, want %d", len(rs), len(demoWant))
	}
	for _, r := range rs {
		want, ok := demoWant[r.slug]
		if !ok {
			t.Errorf("%s: no demo card is pinned for it", r.Title)
			continue
		}
		if r.Err != "" {
			t.Errorf("%s: %s", r.Title, r.Err)
			continue
		}
		if r.card.Verdict != want || r.WantVerdict != want {
			t.Errorf("%s: verdict %s, want %s (expectation %s); the demo card is not written", r.Title, r.card.Verdict, want, r.WantVerdict)
			continue
		}
		b, err := storedForm(r.card)
		if err != nil {
			t.Fatal(err)
		}
		path := demoCardPath(r.slug)
		if *updateDemo {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, b, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, b) {
			t.Errorf("%s is stale; run go test ./tools/eval -run TestDemoCardsMatchReplay -update", path)
		}
	}
}
