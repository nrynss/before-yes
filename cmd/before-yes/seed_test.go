package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nrynss/keel/sqlite"

	"github.com/nrynss/before-yes/internal/cards"
	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/pipeline"
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
		if strings.HasPrefix(line, "backed up\t") || strings.HasPrefix(line, "skipped\t") {
			continue // the copy a reseed makes before it stores anything
		}
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

// A reseed replaces a stored card whose body changed (here, its model fields),
// keeps its public id, adds no row, and backs the store up before it writes.
func TestSeedCardsReplacesChangedCard(t *testing.T) {
	src := filepath.Join("..", "..", "testdata", "demo-cards")
	dir := t.TempDir()
	names := []string{"roblox.json", "minecraft.json", "khan-academy-kids.json"}
	orig := map[string][]byte{}
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(src, n))
		if err != nil {
			t.Fatal(err)
		}
		orig[n] = b
	}
	// The first store holds Roblox with older model names.
	oldRoblox := regexp.MustCompile(`"(reader|judge)_model": "[^"]*"`).ReplaceAll(orig["roblox.json"], []byte(`"${1}_model": "old-model"`))
	if err := os.WriteFile(filepath.Join(dir, "roblox.json"), oldRoblox, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, n := range names[1:] {
		if err := os.WriteFile(filepath.Join(dir, n), orig[n], 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e := writeSettings(t, "")
	args := []string{"seed-cards", "-config", e.path, "-dir", dir}
	var out1, errb bytes.Buffer
	if rc := run(context.Background(), args, &out1, &errb); rc != 0 {
		t.Fatalf("first run: rc %d, stderr %s", rc, errb.String())
	}
	if !strings.Contains(out1.String(), "stored\tRoblox\t") {
		t.Fatalf("first run, want Roblox stored:\n%s", out1.String())
	}

	// Now the recomputed card replaces it.
	if err := os.WriteFile(filepath.Join(dir, "roblox.json"), orig["roblox.json"], 0o644); err != nil {
		t.Fatal(err)
	}
	var out2 bytes.Buffer
	if rc := run(context.Background(), args, &out2, &errb); rc != 0 {
		t.Fatalf("second run: rc %d, stderr %s", rc, errb.String())
	}
	if !strings.Contains(out2.String(), "replaced\tRoblox\t") || strings.Contains("\n"+out2.String(), "\nstored\t") {
		t.Fatalf("second run, want Roblox replaced and nothing new stored:\n%s", out2.String())
	}
	backups, err := filepath.Glob(filepath.Join(e.dir, "data", backupDirName, prefixOf(dbName)+"-*.db"))
	if err != nil || len(backups) == 0 {
		t.Fatalf("no backup of the store before the replace: %v %v", backups, err)
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
	if err != nil || len(recent) != 3 {
		t.Fatalf("Recent has %d cards (err %v), want 3", len(recent), err)
	}
	var want pipeline.Card
	if err := json.Unmarshal(orig["roblox.json"], &want); err != nil {
		t.Fatal(err)
	}
	for _, s := range recent {
		if s.Title != "Roblox" {
			continue
		}
		got, err := store.Get(ctx, s.ID)
		if err != nil || got.Stats.ReaderModel != want.Stats.ReaderModel || got.Stats.JudgeModel != want.Stats.JudgeModel {
			t.Fatalf("stored Roblox models %q %q (err %v), want %q %q", got.Stats.ReaderModel, got.Stats.JudgeModel, err, want.Stats.ReaderModel, want.Stats.JudgeModel)
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
