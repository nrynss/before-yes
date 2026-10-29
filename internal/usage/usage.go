// Package usage counts how often each product event happens, per UTC day.
//
// The counts are the only thing stored: one row per (day, event) holding a
// number. Nothing identifies a visitor. The package takes no IP addresses,
// user agents, cookies, titles, ages or keys, and it sets no cookie. The
// counts are read on the server with `before-yes usage`; no HTTP route or
// page shows them.
//
// The table lives in its own database file (usage.db under data_dir), and
// its schema is applied through Keel sqlite.Migrate in its own namespace.
package usage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/nrynss/keel/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Namespace is the Keel migration namespace this package owns.
const Namespace = "beforeyes_usage"

// The events, one name each. Each is counted where it happens in the server.
const (
	EventHome            = "home"             // the app's index page served
	EventCardView        = "card_view"        // a stored card fetched by id
	EventCompareView     = "compare_view"     // the compare page served
	EventCacheHit        = "cache_hit"        // a check answered from a stored card
	EventCheckStarted    = "check_started"    // a fresh check accepted
	EventCheckFinished   = "check_finished"   // a fresh check that produced a card
	EventCheckFailed     = "check_failed"     // a fresh check that ended in a failure
	EventCapRefused      = "cap_refused"      // refused by the daily cap or a spend budget
	EventVisitorKeys     = "visitor_keys"     // a fresh check run on a visitor's own keys
	EventPasscodeRefused = "passcode_refused" // POST /api/checks refused by the passcode gate
)

// Events lists every event in the order a report shows them.
var Events = []string{
	EventHome,
	EventCardView,
	EventCompareView,
	EventCacheHit,
	EventCheckStarted,
	EventCheckFinished,
	EventCheckFailed,
	EventCapRefused,
	EventVisitorKeys,
	EventPasscodeRefused,
}

// MaxDays is the longest span a report covers.
const MaxDays = 3650

const dayLayout = "2006-01-02"

// ErrInvalid reports an event or a span the store will not take.
var ErrInvalid = errors.New("usage: invalid")

// Store keeps the daily counts. Create it with Open.
type Store struct {
	w   *sql.DB
	r   *sql.DB
	now func() time.Time
}

// Open applies the schema in the beforeyes_usage namespace and returns the
// store. The caller owns db and closes it.
func Open(ctx context.Context, db *sqlite.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("usage: open: %w: nil db", ErrInvalid)
	}
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("usage: open: %w", err)
	}
	if err := sqlite.Migrate(ctx, db, Namespace, sub); err != nil {
		return nil, fmt.Errorf("usage: open: %w", err)
	}
	return &Store{w: db.Writer(), r: db.Reader(), now: time.Now}, nil
}

// Add records one event on today's UTC day.
func (s *Store) Add(ctx context.Context, event string) error {
	return s.AddAt(ctx, s.now(), event)
}

// AddAt records one event on the UTC day of at. The row for (day, event) is
// created with a count of one, or its count goes up by one.
func (s *Store) AddAt(ctx context.Context, at time.Time, event string) error {
	if !slices.Contains(Events, event) {
		return fmt.Errorf("%w: unknown event %q", ErrInvalid, event)
	}
	_, err := s.w.ExecContext(ctx,
		`INSERT INTO usage_counts (day, event, n) VALUES (?, ?, 1)
		 ON CONFLICT (day, event) DO UPDATE SET n = n + 1`,
		at.UTC().Format(dayLayout), event)
	if err != nil {
		return fmt.Errorf("usage: add %s: %w", event, err)
	}
	return nil
}

// Count returns how many times event was recorded on the UTC day of at. A
// day with no row counts zero.
func (s *Store) Count(ctx context.Context, at time.Time, event string) (int64, error) {
	var n int64
	err := s.r.QueryRowContext(ctx,
		`SELECT n FROM usage_counts WHERE day = ? AND event = ?`,
		at.UTC().Format(dayLayout), event).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("usage: count %s: %w", event, err)
	}
	return n, nil
}

// Day is one UTC day's counts. Counts holds an entry for each event that
// happened that day, and is empty for a quiet day.
type Day struct {
	Date   string
	Counts map[string]int64
}

// Report returns the days UTC days ending with the day of now, newest first.
// Every day in the span is returned, quiet days included. days must be
// between 1 and MaxDays.
func (s *Store) Report(ctx context.Context, now time.Time, days int) ([]Day, error) {
	if days < 1 || days > MaxDays {
		return nil, fmt.Errorf("%w: days must be between 1 and %d, got %d", ErrInvalid, MaxDays, days)
	}
	n := now.UTC()
	last := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
	first := last.AddDate(0, 0, -(days - 1))
	rows, err := s.r.QueryContext(ctx,
		`SELECT day, event, n FROM usage_counts WHERE day >= ? AND day <= ?`,
		first.Format(dayLayout), last.Format(dayLayout))
	if err != nil {
		return nil, fmt.Errorf("usage: report: %w", err)
	}
	defer rows.Close()
	byDay := map[string]map[string]int64{}
	for rows.Next() {
		var day, event string
		var c int64
		if err := rows.Scan(&day, &event, &c); err != nil {
			return nil, fmt.Errorf("usage: report: %w", err)
		}
		if byDay[day] == nil {
			byDay[day] = map[string]int64{}
		}
		byDay[day][event] = c
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage: report: %w", err)
	}
	out := make([]Day, 0, days)
	for i := 0; i < days; i++ {
		date := last.AddDate(0, 0, -i).Format(dayLayout)
		counts := byDay[date]
		if counts == nil {
			counts = map[string]int64{}
		}
		out = append(out, Day{Date: date, Counts: counts})
	}
	return out, nil
}

// Totals sums each event over the days.
func Totals(days []Day) map[string]int64 {
	t := map[string]int64{}
	for _, d := range days {
		for e, c := range d.Counts {
			t[e] += c
		}
	}
	return t
}

// queueSize bounds the counts waiting to be written. A count that finds the
// queue full is dropped, never waited for.
const queueSize = 1024

// writeTimeout bounds one write.
const writeTimeout = 5 * time.Second

// Recorder writes counts in the background, so a request never waits on
// the database. Count never blocks and never returns an error; a failed
// write is logged at warn and the count is lost, which is the price of not
// slowing or failing a request.
type Recorder struct {
	store   *Store
	log     *slog.Logger
	timeout time.Duration

	mu     sync.Mutex
	closed bool
	queue  chan string
	done   chan struct{}
}

// NewRecorder starts the background writer for store. log may be nil.
func NewRecorder(store *Store, log *slog.Logger) *Recorder {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	r := &Recorder{
		store:   store,
		log:     log,
		timeout: writeTimeout,
		queue:   make(chan string, queueSize),
		done:    make(chan struct{}),
	}
	go r.run()
	return r
}

// Count queues one event for the day it is written on. It drops the event,
// and logs it at warn, when the queue is full or the recorder is closed.
func (r *Recorder) Count(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	select {
	case r.queue <- event:
	default:
		r.log.Warn("usage: queue full, count dropped", "event", event)
	}
}

// Close stops accepting counts, writes the queued ones, and returns. It is
// safe to call more than once.
func (r *Recorder) Close() {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.queue)
	}
	r.mu.Unlock()
	<-r.done
}

func (r *Recorder) run() {
	defer close(r.done)
	for event := range r.queue {
		r.write(event)
	}
}

// write stores one count. A panic is logged, never raised.
func (r *Recorder) write(event string) {
	defer func() {
		if v := recover(); v != nil {
			r.log.Warn("usage: count failed", "event", event, "panic", fmt.Sprint(v))
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	if err := r.store.Add(ctx, event); err != nil {
		r.log.Warn("usage: count not stored", "event", event, "err", err)
	}
}
