package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/nrynss/keel/sqlite"

	"github.com/nrynss/before-yes/internal/cards"
	"github.com/nrynss/before-yes/internal/pipeline"
)

// seedBackupKeep is how many copies of each database a reseed keeps, the
// backup command's default.
const seedBackupKeep = 14

// seedCards stores every card file in dir (*.json, each a card in the stored
// format, as testdata/demo-cards holds them) into the card store under
// data_dir. A card the store already holds is not stored again, so running
// it twice changes nothing the second time. A stored card with the same
// title, age, checklist version and checked time but another body (a demo
// card recomputed, say, after a model name changed) is replaced in place, so
// its URL stays and no duplicate row appears. Before any store that already
// has a database, the databases are backed up. Cards are never deleted.
//
// Recent lists the newest card per title and age, so a demo card that is
// older than a stored card for the same title and age stays hidden behind
// it. The command reports that case; it does not move the stored card.
func seedCards(ctx context.Context, s Settings, dir string, stdout io.Writer) error {
	names, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return fmt.Errorf("seed-cards: no card files in %s", dir)
	}
	cs := make([]pipeline.Card, 0, len(names))
	for _, n := range names {
		b, err := os.ReadFile(n)
		if err != nil {
			return err
		}
		var c pipeline.Card
		d := json.NewDecoder(bytes.NewReader(b))
		d.DisallowUnknownFields()
		if err := d.Decode(&c); err != nil {
			return fmt.Errorf("seed-cards: %s: %w", n, err)
		}
		cs = append(cs, c)
	}
	if err := os.MkdirAll(s.DataDir, 0o700); err != nil {
		return err
	}
	// A reseed may replace stored cards, so the existing store is copied first.
	if _, err := os.Stat(filepath.Join(s.DataDir, dbName)); err == nil {
		if err := backupData(ctx, s, seedBackupKeep, stdout); err != nil {
			return err
		}
	}
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(s.DataDir, dbName), BusyTimeout: 5 * time.Second})
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	store, err := cards.Open(ctx, db)
	if err != nil {
		return err
	}
	for _, c := range cs {
		pub, out, err := store.Seed(ctx, c)
		if err != nil {
			return fmt.Errorf("seed-cards: %s: %w", c.Title, err)
		}
		how := map[cards.SeedOutcome]string{
			cards.SeedInserted: "stored",
			cards.SeedKept:     "already stored",
			cards.SeedReplaced: "replaced",
		}[out]
		fmt.Fprintf(stdout, "%s\t%s\tage %d\t%s\t%s\n", how, c.Title, c.Age, c.Verdict, pub)
	}
	return nil
}
