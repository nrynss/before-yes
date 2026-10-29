package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nrynss/keel/sqlite"

	counts "github.com/nrynss/before-yes/internal/usage"
)

// usageName is the usage counts' database file under data_dir.
const usageName = "usage.db"

// usageReport prints the counts for the last days UTC days ending with the
// day of now: one row per day, newest first, one column per event, and a
// totals row. Quiet days are printed with zeros. A data_dir with no usage
// database yet prints one line saying so. The database is opened for
// reading and migrated if needed, but never created here.
func usageReport(ctx context.Context, s Settings, days int, now time.Time, stdout io.Writer) error {
	if days < 1 || days > counts.MaxDays {
		return fmt.Errorf("usage: -days must be between 1 and %d, got %d", counts.MaxDays, days)
	}
	path := filepath.Join(s.DataDir, usageName)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		_, err := fmt.Fprintln(stdout, "no usage recorded yet")
		return err
	} else if err != nil {
		return err
	}
	db, err := sqlite.Open(ctx, sqlite.Config{Path: path, BusyTimeout: 5 * time.Second})
	if err != nil {
		return fmt.Errorf("usage: open: %w", err)
	}
	defer func() { _ = db.Close() }()
	st, err := counts.Open(ctx, db)
	if err != nil {
		return err
	}
	rows, err := st.Report(ctx, now, days)
	if err != nil {
		return err
	}
	return writeUsageTable(stdout, rows)
}

// writeUsageTable prints days as a right-aligned table with a totals row.
func writeUsageTable(w io.Writer, days []counts.Day) error {
	header := append([]string{"day"}, counts.Events...)
	rows := [][]string{header}
	for _, d := range days {
		cells := []string{d.Date}
		for _, e := range counts.Events {
			cells = append(cells, strconv.FormatInt(d.Counts[e], 10))
		}
		rows = append(rows, cells)
	}
	totals := counts.Totals(days)
	cells := []string{"total"}
	for _, e := range counts.Events {
		cells = append(cells, strconv.FormatInt(totals[e], 10))
	}
	rows = append(rows, cells)

	widths := make([]int, len(header))
	for _, r := range rows {
		for i, c := range r {
			widths[i] = max(widths[i], len(c))
		}
	}
	for _, r := range rows {
		var b strings.Builder
		for i, c := range r {
			if i > 0 {
				b.WriteString("  ")
			}
			fmt.Fprintf(&b, "%*s", widths[i], c)
		}
		b.WriteByte('\n')
		if _, err := io.WriteString(w, b.String()); err != nil {
			return err
		}
	}
	return nil
}
