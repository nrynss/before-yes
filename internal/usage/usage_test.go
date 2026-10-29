package usage

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/keel/sqlite"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	db, err := sqlite.Open(context.Background(), sqlite.Config{Path: filepath.Join(t.TempDir(), "usage.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s, err := Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var day = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

// Each event is one row per UTC day, and a repeat increments that row.
func TestAddUpsertsOneRowPerDayAndEvent(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	for i := 0; i < 3; i++ {
		if err := s.AddAt(ctx, day, EventHome); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddAt(ctx, day, EventCardView); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Count(ctx, day, EventHome); err != nil || n != 3 {
		t.Fatalf("home %d err %v, want 3", n, err)
	}
	if n, err := s.Count(ctx, day, EventCardView); err != nil || n != 1 {
		t.Fatalf("card_view %d err %v, want 1", n, err)
	}
	if n, err := s.Count(ctx, day, EventCheckStarted); err != nil || n != 0 {
		t.Fatalf("check_started %d err %v, want 0", n, err)
	}
	var rows int
	if err := s.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_counts`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("%d rows, want 2 (one per event, not one per count)", rows)
	}
}

// The day is the UTC day: 23:59:59 and 00:00:00 UTC are different days, and
// a local time is counted on the UTC day it falls in.
func TestDayBoundaryIsUTC(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	lateOn28 := time.Date(2026, 10, 28, 23, 59, 59, 0, time.UTC)
	earlyOn29 := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	// 01:30 on the 29th in UTC+2 is 23:30 on the 28th in UTC.
	localOn29 := time.Date(2026, 10, 29, 1, 30, 0, 0, time.FixedZone("UTC+2", 2*3600))

	if err := s.AddAt(ctx, lateOn28, EventHome); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAt(ctx, earlyOn29, EventHome); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAt(ctx, localOn29, EventHome); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Count(ctx, lateOn28, EventHome); n != 2 {
		t.Fatalf("28 Oct UTC has %d, want 2 (23:59:59 and the 23:30 local-time event)", n)
	}
	if n, _ := s.Count(ctx, earlyOn29, EventHome); n != 1 {
		t.Fatalf("29 Oct UTC has %d, want 1", n)
	}
}

// Report reads the counts back newest day first, includes quiet days, and
// leaves out days outside the span.
func TestReportReadsBackNewestFirst(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	mustAdd := func(at time.Time, e string, times int) {
		t.Helper()
		for i := 0; i < times; i++ {
			if err := s.AddAt(ctx, at, e); err != nil {
				t.Fatal(err)
			}
		}
	}
	mustAdd(now, EventHome, 4)
	mustAdd(now.AddDate(0, 0, -2), EventCardView, 2)
	mustAdd(now.AddDate(0, 0, -5), EventHome, 9) // outside a 3-day span

	days, err := s.Report(ctx, now, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 3 {
		t.Fatalf("%d days, want 3", len(days))
	}
	wantDates := []string{"2026-10-29", "2026-10-28", "2026-10-27"}
	for i, d := range days {
		if d.Date != wantDates[i] {
			t.Fatalf("day %d is %s, want %s", i, d.Date, wantDates[i])
		}
	}
	if days[0].Counts[EventHome] != 4 || len(days[0].Counts) != 1 {
		t.Fatalf("29 Oct counts %v", days[0].Counts)
	}
	if len(days[1].Counts) != 0 {
		t.Fatalf("28 Oct counts %v, want none", days[1].Counts)
	}
	if days[2].Counts[EventCardView] != 2 {
		t.Fatalf("27 Oct counts %v", days[2].Counts)
	}
	totals := Totals(days)
	if totals[EventHome] != 4 || totals[EventCardView] != 2 {
		t.Fatalf("totals %v", totals)
	}
	if _, err := s.Report(ctx, now, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero days: %v, want ErrInvalid", err)
	}
	if _, err := s.Report(ctx, now, MaxDays+1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("too many days: %v, want ErrInvalid", err)
	}
}

// An event outside the list is refused and stores nothing, so no free text
// (an address, an id, a title) can reach the table.
func TestUnknownEventRefused(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	err := s.AddAt(ctx, day, "203.0.113.7")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown event: %v, want ErrInvalid", err)
	}
	var rows int
	if err := s.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_counts`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("%d rows after a refused event", rows)
	}
}

// The recorder writes what it was given, and Close waits for the queue.
func TestRecorderWritesQueuedCountsOnClose(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	s.now = func() time.Time { return day }
	r := NewRecorder(s, nil)
	for i := 0; i < 5; i++ {
		r.Count(EventHome)
	}
	r.Count(EventCheckFailed)
	r.Close()
	r.Count(EventHome) // after Close: dropped, not a panic
	r.Close()          // a second Close is a no-op

	if n, _ := s.Count(ctx, day, EventHome); n != 5 {
		t.Fatalf("home %d, want 5", n)
	}
	if n, _ := s.Count(ctx, day, EventCheckFailed); n != 1 {
		t.Fatalf("check_failed %d, want 1", n)
	}
}

// A store that fails never blocks or fails Count: the failure is logged at
// warn and the recorder keeps going.
func TestRecorderFailedWriteLoggedAtWarn(t *testing.T) {
	db, err := sqlite.Open(context.Background(), sqlite.Config{Path: filepath.Join(t.TempDir(), "usage.db")})
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	logs := &syncBuf{}
	r := NewRecorder(s, slog.New(slog.NewTextHandler(logs, nil)))
	for i := 0; i < 3; i++ {
		r.Count(EventHome)
	}
	r.Close()
	if !bytes.Contains(logs.bytes(), []byte("usage: count not stored")) {
		t.Fatalf("no warn line for the failed writes: %q", logs.bytes())
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.b.Bytes()...)
}
