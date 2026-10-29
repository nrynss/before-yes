package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nrynss/keel/sqlite"
)

// backupDirName is the folder under data_dir that holds the copies.
const backupDirName = "backups"

// backupData writes a consistent copy of each database under data_dir into
// the backups folder, named by the UTC time, then removes the oldest copies
// of each database beyond keep. The databases are the application's, the
// meter's and the usage counts'. A database that does not exist yet is
// skipped, so a backup never creates an empty database. Copies are made with
// keel's sqlite.Backup (VACUUM INTO, then rename), so a copy is never half
// written, even while serve runs.
func backupData(ctx context.Context, s Settings, keep int, stdout io.Writer) error {
	if keep < 1 {
		return fmt.Errorf("backup: -keep must be at least 1, got %d", keep)
	}
	dir := filepath.Join(s.DataDir, backupDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	for _, name := range []string{dbName, spendName, usageName} {
		src := filepath.Join(s.DataDir, name)
		if _, err := os.Stat(src); err != nil {
			fmt.Fprintf(stdout, "skipped\t%s\tnot found\n", name)
			continue
		}
		db, err := sqlite.Open(ctx, sqlite.Config{Path: src, BusyTimeout: 5 * time.Second})
		if err != nil {
			return fmt.Errorf("backup: open %s: %w", name, err)
		}
		base := prefixOf(name) + "-" + stamp + ".db"
		dest := filepath.Join(dir, base)
		err = sqlite.Backup(ctx, db, dest)
		_ = db.Close()
		if err != nil {
			return fmt.Errorf("backup: %s: %w", name, err)
		}
		fmt.Fprintf(stdout, "backed up\t%s\t%s\n", name, dest)
		if err := pruneBackups(dir, prefixOf(name), keep, stdout); err != nil {
			return err
		}
	}
	return nil
}

// prefixOf names a database's copies: before-yes.db gives before-yes, and
// spend.db gives spend.
func prefixOf(name string) string {
	return name[:len(name)-len(filepath.Ext(name))]
}

// pruneBackups removes all but the newest keep copies whose names start with
// prefix followed by a dash. The names carry a UTC stamp that sorts in time
// order, so a lexical sort is a time sort.
func pruneBackups(dir, prefix string, keep int, stdout io.Writer) error {
	names, err := filepath.Glob(filepath.Join(dir, prefix+"-*.db"))
	if err != nil {
		return err
	}
	sort.Strings(names)
	for i := 0; i < len(names)-keep; i++ {
		if err := os.Remove(names[i]); err != nil {
			return fmt.Errorf("backup: prune %s: %w", names[i], err)
		}
		fmt.Fprintf(stdout, "pruned\t%s\n", names[i])
	}
	return nil
}
