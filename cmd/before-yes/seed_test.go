package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nrynss/keel/sqlite"

	"github.com/nrynss/before-yes/internal/cards"
	"github.com/nrynss/before-yes/internal/checklist"
)

// The demo cards are stored once however often seed-cards runs, and the
// store lists all three with their verdicts. The store is a temporary one
// under a temporary data_dir.
func TestSeedCardsIsIdempotent(t *testing.T) {
	e := writeSettings(t, "")
	args := []string{"seed-cards", "-config", e.path, "-dir", filepath.Join("..", "..", "testdata", "demo-cards")}

	var out1, errb bytes.Buffer
	if rc := run(context.Background(), args, &out1, &errb); rc != 0 {
		t.Fatalf("first run: rc %d, stderr %s", rc, errb.String())
	}
	var out2 bytes.Buffer
	if rc := run(context.Background(), args, &out2, &errb); rc != 0 {
		t.Fatalf("second run: rc %d, stderr %s", rc, errb.String())
	}
	stored := 0
	for _, line := range strings.Split(strings.TrimSpace(out1.String()), "\n") {
		if strings.HasPrefix(line, "stored\t") {
			stored++
		}
	}
	if stored != 3 {
		t.Fatalf("first run stored %d cards, want 3:\n%s", stored, out1.String())
	}
	for _, line := range strings.Split(strings.TrimSpace(out2.String()), "\n") {
		if !strings.HasPrefix(line, "already stored\t") {
			t.Fatalf("second run line %q, want already stored", line)
		}
	}

	ctx := context.Background()
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(e.dir, "data", dbName)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	store, err := cards.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	recent, err := store.Recent(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]checklist.Verdict{"Roblox": checklist.No, "Minecraft": checklist.YesWithSettings, "Khan Academy Kids": checklist.Yes}
	if len(recent) != 3 {
		t.Fatalf("Recent has %d cards, want 3", len(recent))
	}
	for _, s := range recent {
		if want[s.Title] != s.Verdict || !strings.HasPrefix(s.ID, "c_") {
			t.Errorf("Recent: %s %s %s", s.Title, s.Verdict, s.ID)
		}
	}
}

// An empty directory is an error, and nothing is opened or stored.
func TestSeedCardsRefusesEmptyDir(t *testing.T) {
	e := writeSettings(t, "")
	var out, errb bytes.Buffer
	args := []string{"seed-cards", "-config", e.path, "-dir", t.TempDir()}
	if rc := run(context.Background(), args, &out, &errb); rc != 1 || !strings.Contains(errb.String(), "no card files") {
		t.Fatalf("rc %d, stderr %s", rc, errb.String())
	}
}
