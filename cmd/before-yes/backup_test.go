package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nrynss/keel/sqlite"
)

// A backup copies the application database into data_dir/backups, skips the
// meter's file when it does not exist yet, and never creates an empty one.
func TestBackupCopiesExistingDatabase(t *testing.T) {
	e := writeSettings(t, "")
	ctx := context.Background()
	data := filepath.Join(e.dir, "data")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(data, dbName)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer().ExecContext(ctx, "CREATE TABLE marker (v TEXT); INSERT INTO marker VALUES ('kept')"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	if rc := run(ctx, []string{"backup", "-config", e.path}, &out, &errb); rc != 0 {
		t.Fatalf("backup: rc %d, stderr %s", rc, errb.String())
	}
	if !strings.Contains(out.String(), "backed up\t"+dbName+"\t") {
		t.Fatalf("output %q does not report the copy", out.String())
	}
	if !strings.Contains(out.String(), "skipped\t"+spendName+"\tnot found") {
		t.Fatalf("output %q does not skip the missing meter file", out.String())
	}
	if _, err := os.Stat(filepath.Join(data, spendName)); !os.IsNotExist(err) {
		t.Fatalf("backup created %s: %v", spendName, err)
	}

	copies, err := filepath.Glob(filepath.Join(data, backupDirName, prefixOf(dbName)+"-*.db"))
	if err != nil {
		t.Fatal(err)
	}
	if len(copies) != 1 {
		t.Fatalf("copies %v, want exactly one", copies)
	}
	bdb, err := sqlite.Open(ctx, sqlite.Config{Path: copies[0]})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bdb.Close() }()
	var v string
	if err := bdb.Reader().QueryRowContext(ctx, "SELECT v FROM marker").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != "kept" {
		t.Fatalf("copy holds %q, want kept", v)
	}
}

// Pruning keeps the newest copies by name, which sorts in time order.
func TestPruneBackupsKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"before-yes-20260101T000000Z.db",
		"before-yes-20260102T000000Z.db",
		"before-yes-20260103T000000Z.db",
		"before-yes-20260104T000000Z.db",
		"spend-20260101T000000Z.db",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := pruneBackups(dir, "before-yes", 2, &out); err != nil {
		t.Fatal(err)
	}
	for _, gone := range names[:2] {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Fatalf("%s survived pruning: %v", gone, err)
		}
	}
	for _, kept := range append(names[2:4:4], names[4]) {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Fatalf("%s was pruned: %v", kept, err)
		}
	}
}

func TestBackupRejectsZeroKeep(t *testing.T) {
	e := writeSettings(t, "")
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"backup", "-config", e.path, "-keep", "0"}, &out, &errb); rc == 0 {
		t.Fatal("keep 0 was accepted")
	}
}
