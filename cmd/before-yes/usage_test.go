package main

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/keel/sqlite"

	counts "github.com/nrynss/before-yes/internal/usage"
)

// seedUsage writes counts into data/usage.db the way serve does.
func seedUsage(t *testing.T, data string, add func(ctx context.Context, s *counts.Store)) {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(data, usageName)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	s, err := counts.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	add(ctx, s)
}

// The table has a row per day, newest first, one column per event, zeros for
// quiet days, and a totals row. Days outside the span are left out.
func TestUsageCommandPrintsTable(t *testing.T) {
	e := writeSettings(t, "")
	data := filepath.Join(e.dir, "data")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	seedUsage(t, data, func(ctx context.Context, s *counts.Store) {
		for i := 0; i < 3; i++ {
			mustAddAt(t, ctx, s, now, counts.EventHome)
		}
		mustAddAt(t, ctx, s, now.Add(11*time.Hour+59*time.Minute), counts.EventCheckStarted) // 23:59 UTC on the 29th
		mustAddAt(t, ctx, s, now.AddDate(0, 0, -1), counts.EventCardView)
		mustAddAt(t, ctx, s, now.AddDate(0, 0, -1), counts.EventCardView)
		mustAddAt(t, ctx, s, now.AddDate(0, 0, -20), counts.EventHome) // outside a 3-day span
	})

	var out bytes.Buffer
	s, _, err := Load(context.Background(), e.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := usageReport(context.Background(), s, 3, now, &out); err != nil {
		t.Fatal(err)
	}
	t.Logf("sample output:\n%s", out.String())

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1+3+1 {
		t.Fatalf("%d lines, want header, 3 days and a total:\n%s", len(lines), out.String())
	}
	header := strings.Fields(lines[0])
	if strings.Join(header, " ") != "day "+strings.Join(counts.Events, " ") {
		t.Fatalf("header %v", header)
	}
	col := func(name string) int {
		for i, h := range header {
			if h == name {
				return i
			}
		}
		t.Fatalf("no column %s", name)
		return -1
	}
	row := func(line string) map[string]string {
		f := strings.Fields(line)
		if len(f) != len(header) {
			t.Fatalf("row %q has %d cells, want %d", line, len(f), len(header))
		}
		m := map[string]string{"day": f[0]}
		for _, e := range counts.Events {
			m[e] = f[col(e)]
		}
		return m
	}
	want := []map[string]string{
		{"day": "2026-10-29", counts.EventHome: "3", counts.EventCheckStarted: "1", counts.EventCardView: "0"},
		{"day": "2026-10-28", counts.EventCardView: "2", counts.EventHome: "0", counts.EventCheckStarted: "0"},
		{"day": "2026-10-27", counts.EventHome: "0", counts.EventCardView: "0", counts.EventCheckStarted: "0"},
	}
	for i, w := range want {
		got := row(lines[1+i])
		for k, v := range w {
			if got[k] != v {
				t.Errorf("row %d column %s = %s, want %s\n%s", i, k, got[k], v, out.String())
			}
		}
	}
	total := row(lines[4])
	if total["day"] != "total" || total[counts.EventHome] != "3" || total[counts.EventCardView] != "2" || total[counts.EventCheckStarted] != "1" {
		t.Errorf("totals row %v\n%s", total, out.String())
	}
}

func mustAddAt(t *testing.T, ctx context.Context, s *counts.Store, at time.Time, event string) {
	t.Helper()
	if err := s.AddAt(ctx, at, event); err != nil {
		t.Fatal(err)
	}
}

// Before any count is written there is no database; the command says so and
// creates nothing.
func TestUsageCommandWithoutDatabase(t *testing.T) {
	e := writeSettings(t, "")
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"usage", "-config", e.path}, &out, &errb); rc != 0 {
		t.Fatalf("usage: rc %d, stderr %s", rc, errb.String())
	}
	if strings.TrimSpace(out.String()) != "no usage recorded yet" {
		t.Fatalf("output %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(e.dir, "data", usageName)); !os.IsNotExist(err) {
		t.Fatalf("usage created its database: %v", err)
	}
}

func TestUsageCommandRejectsBadDays(t *testing.T) {
	e := writeSettings(t, "")
	var out, errb bytes.Buffer
	for _, days := range []string{"0", "-3", strconv.Itoa(counts.MaxDays + 1)} {
		if rc := run(context.Background(), []string{"usage", "-config", e.path, "-days", days}, &out, &errb); rc == 0 {
			t.Fatalf("-days %s was accepted", days)
		}
	}
	if rc := run(context.Background(), []string{"usage", "-config", e.path, "Roblox"}, &out, &errb); rc == 0 {
		t.Fatal("a title after usage was accepted")
	}
}

// The backup copies the usage counts with the other databases.
func TestBackupCopiesUsageDatabase(t *testing.T) {
	e := writeSettings(t, "")
	data := filepath.Join(e.dir, "data")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	seedUsage(t, data, func(ctx context.Context, s *counts.Store) {
		mustAddAt(t, ctx, s, time.Now(), counts.EventHome)
	})

	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"backup", "-config", e.path}, &out, &errb); rc != 0 {
		t.Fatalf("backup: rc %d, stderr %s", rc, errb.String())
	}
	if !strings.Contains(out.String(), "backed up\t"+usageName+"\t") {
		t.Fatalf("output %q does not report the usage copy", out.String())
	}
	copies, err := filepath.Glob(filepath.Join(data, backupDirName, prefixOf(usageName)+"-*.db"))
	if err != nil {
		t.Fatal(err)
	}
	if len(copies) != 1 {
		t.Fatalf("usage copies %v, want exactly one", copies)
	}
	db, err := sqlite.Open(context.Background(), sqlite.Config{Path: copies[0]})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.Reader().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM usage_counts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("copy holds %d rows, want 1", n)
	}
}

// The serve path counts through the real wiring, and no response sets a
// cookie. Counts land in usage.db once serve has written them.
func TestServeCountsWithoutCookies(t *testing.T) {
	a, ts := serveApp(t, "", noEnv)
	for _, p := range []string{"/", "/compare/c_0123456789abcdef0123456789abcdef/c_fedcba9876543210fedcba9876543210", "/api/cards", "/api/cards/c_0123456789abcdef0123456789abcdef", "/api/status", "/healthz"} {
		resp, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if v := resp.Header.Values("Set-Cookie"); len(v) != 0 {
			t.Errorf("GET %s set a cookie: %v", p, v)
		}
	}
	resp, _ := do(t, "POST", ts.URL+"/api/checks", `{"title":"","age":9}`, nil)
	if v := resp.Header.Values("Set-Cookie"); len(v) != 0 {
		t.Errorf("POST /api/checks set a cookie: %v", v)
	}

	// Draining the recorder makes every count visible to the reads below.
	a.recorder.Close()
	db, err := sqlite.Open(context.Background(), sqlite.Config{Path: filepath.Join(filepath.Dir(a.DBPath), usageName)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	s, err := counts.Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if n, _ := s.Count(context.Background(), now, counts.EventHome); n != 1 {
		t.Errorf("home %d, want 1", n)
	}
	if n, _ := s.Count(context.Background(), now, counts.EventCompareView); n != 1 {
		t.Errorf("compare_view %d, want 1", n)
	}
	if n, _ := s.Count(context.Background(), now, counts.EventCardView); n != 0 {
		t.Errorf("card_view %d, want 0 (an unknown card is not a view)", n)
	}
}
