package spend_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/nrynss/keel/cost"

	"github.com/nrynss/before-yes/internal/spend"
)

// dayClock is the meter's clock in these tests. It stands still until a test
// moves it, so a UTC midnight is crossed by assignment, not by sleeping.
type dayClock struct{ at time.Time }

func (c *dayClock) now() time.Time { return c.at }

// openClockedMeter opens a meter over path on clock and closes it when the
// test ends.
func openClockedMeter(t *testing.T, path string, clock *dayClock, pools map[spend.Pool]spend.PoolConfig) *spend.Meter {
	t.Helper()
	m, err := spend.Open(context.Background(), spend.Config{Path: path, Pools: pools, Now: clock.now})
	if err != nil {
		t.Fatalf("open meter: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

// runCheck runs one fresh check the way the pipeline will: it begins the check,
// makes the paid call, and ends the check with Done when the call succeeds and
// Fail when it does not. It reports the BeginCheck error, and whether the
// paid call ran.
func runCheck(m *spend.Meter, ref string, succeed bool) (ran bool, err error) {
	c, err := m.BeginCheck(context.Background(), ref)
	if err != nil {
		return false, err
	}
	ran = true
	if succeed {
		return ran, c.Done()
	}
	return ran, c.Fail()
}

// TestSixthCheckInADayIsRefusedBeforeTheCall pins the daily cap: with a limit
// of 5, the sixth fresh check of a UTC day is refused with cost.ErrOverBudget
// and its paid call never runs.
func TestSixthCheckInADayIsRefusedBeforeTheCall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "before-yes.db")
	clock := &dayClock{at: time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)}
	m := openClockedMeter(t, path, clock, map[spend.Pool]spend.PoolConfig{
		spend.PoolChecksDaily: {Limit: 5},
	})

	calls := 0
	for i := 1; i <= 5; i++ {
		ran, err := runCheck(m, fmt.Sprintf("check-%d", i), true)
		if err != nil || !ran {
			t.Fatalf("check %d: ran %v, err %v; the first five checks of the day must run", i, ran, err)
		}
		calls++
	}

	c, err := m.BeginCheck(context.Background(), "check-6")
	if !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("the sixth check of the day must be refused with cost.ErrOverBudget: %v", err)
	}
	if c != nil {
		t.Fatal("a refused check must not return a Check")
	}
	// The caller makes its paid call only after BeginCheck succeeds, so a
	// refused check leaves calls at five.
	if calls != 5 {
		t.Fatalf("the paid call ran %d times, want 5", calls)
	}
	s, err := m.Status(context.Background(), spend.PoolChecksDaily)
	if err != nil {
		t.Fatal(err)
	}
	if want := (spend.Status{Limit: 5, Spent: 5, Remaining: 0}); s != want {
		t.Fatalf("checks pool standing = %+v, want %+v", s, want)
	}
}

// TestDailyCapResetsOnTheNextUTCDay moves the meter's clock across a UTC
// midnight. The day's five checks stop counting the moment the window turns,
// so the next day admits five more, and the earlier checks stay in the ledger.
func TestDailyCapResetsOnTheNextUTCDay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "before-yes.db")
	clock := &dayClock{at: time.Date(2026, 10, 14, 23, 59, 0, 0, time.UTC)}
	m := openClockedMeter(t, path, clock, map[spend.Pool]spend.PoolConfig{
		spend.PoolChecksDaily: {Limit: 5},
	})
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		if _, err := runCheck(m, fmt.Sprintf("oct14-%d", i), true); err != nil {
			t.Fatalf("oct14 check %d: %v", i, err)
		}
	}
	if _, err := m.BeginCheck(ctx, "oct14-6"); !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("the sixth check before midnight must be refused: %v", err)
	}

	// One second past UTC midnight, a new window opens.
	clock.at = time.Date(2026, 10, 15, 0, 0, 1, 0, time.UTC)
	s, err := m.Status(ctx, spend.PoolChecksDaily)
	if err != nil {
		t.Fatal(err)
	}
	if want := (spend.Status{Limit: 5, Spent: 0, Remaining: 5}); s != want {
		t.Fatalf("after midnight the checks pool must read a fresh window: got %+v, want %+v", s, want)
	}
	for i := 1; i <= 5; i++ {
		if _, err := runCheck(m, fmt.Sprintf("oct15-%d", i), true); err != nil {
			t.Fatalf("oct15 check %d must run in the new day: %v", i, err)
		}
	}
	if _, err := m.BeginCheck(ctx, "oct15-6"); !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("the sixth check of the new day must be refused: %v", err)
	}

	// The earlier day's checks remain in the ledger, read through a fresh
	// connection. Each is one whole check, booked under its own ref.
	if err := m.Close(); err != nil {
		t.Fatalf("close meter: %v", err)
	}
	db := freshDB(t, path)
	var rows, ones int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cost_checks_daily_charge`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 10 {
		t.Fatalf("cost_checks_daily_charge holds %d rows, want 10 (five per day)", rows)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM cost_checks_daily_charge WHERE kind = 'check' AND unit_price = 1 AND units = 1 AND denomination = 'check'`).Scan(&ones); err != nil {
		t.Fatal(err)
	}
	if ones != 10 {
		t.Fatalf("%d of the rows are one whole check, want 10", ones)
	}
}

// TestFailedCheckReleasesItsCount pins that a failed check gives its count
// back: five checks that fail leave the day's cap whole, so five checks that
// succeed still run, and only the sixth is refused.
func TestFailedCheckReleasesItsCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "before-yes.db")
	clock := &dayClock{at: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)}
	m := openClockedMeter(t, path, clock, map[spend.Pool]spend.PoolConfig{
		spend.PoolChecksDaily: {Limit: 5},
	})
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		if _, err := runCheck(m, fmt.Sprintf("failed-%d", i), false); err != nil {
			t.Fatalf("failed check %d: %v", i, err)
		}
	}
	s, err := m.Status(ctx, spend.PoolChecksDaily)
	if err != nil {
		t.Fatal(err)
	}
	if want := (spend.Status{Limit: 5, Spent: 0, Remaining: 5}); s != want {
		t.Fatalf("five failed checks must release their count: got %+v, want %+v", s, want)
	}
	spent, err := m.Spent(ctx, spend.PoolChecksDaily, "failed-1")
	if err != nil {
		t.Fatal(err)
	}
	if spent != 0 {
		t.Fatalf("a failed check must book nothing, booked %d", spent)
	}

	for i := 1; i <= 5; i++ {
		if _, err := runCheck(m, fmt.Sprintf("done-%d", i), true); err != nil {
			t.Fatalf("check %d after the failures must run: %v", i, err)
		}
	}
	if _, err := m.BeginCheck(ctx, "done-6"); !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("the sixth successful check must be refused: %v", err)
	}
}

// TestDeadlineExceededIsBookedAtTheEstimate pins that a call which hit its own
// deadline is booked at the estimate, unmeasured, and still returns the error.
// The charge is read back through a fresh connection.
func TestDeadlineExceededIsBookedAtTheEstimate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "before-yes.db")
	m := openMeter(t, path, map[spend.Pool]spend.PoolConfig{
		spend.PoolModel: {Limit: cost.Cent},
	})
	ctx := context.Background()

	estimate := 3 * cost.Millicent
	usage, err := m.Spend(ctx, spend.PoolModel, "tokenfactory:read", "check-1", estimate,
		func(context.Context) (cost.Usage, error) {
			return cost.Usage{Price: cost.Millicent, Measured: false},
				fmt.Errorf("model: no reply before the call's deadline: %w", context.DeadlineExceeded)
		})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a timed-out call must still return context.DeadlineExceeded: %v", err)
	}
	if usage.Price != estimate || usage.Measured {
		t.Fatalf("a timed-out call settles at the estimate, unmeasured: %+v, want price %d", usage, estimate)
	}
	s, err := m.Status(ctx, spend.PoolModel)
	if err != nil {
		t.Fatal(err)
	}
	if want := (spend.Status{Limit: cost.Cent, Spent: estimate, Remaining: cost.Cent - estimate}); s != want {
		t.Fatalf("the estimate must be booked: got %+v, want %+v", s, want)
	}

	if err := m.Close(); err != nil {
		t.Fatalf("close meter: %v", err)
	}
	db := freshDB(t, path)
	var price int64
	var kind string
	if err := db.QueryRow(`SELECT unit_price, kind FROM cost_model_charge WHERE ref = 'check-1'`).Scan(&price, &kind); err != nil {
		t.Fatalf("the timed-out call must leave a charge in the ledger: %v", err)
	}
	if cost.Price(price) != estimate || kind != "tokenfactory:read" {
		t.Fatalf("cost_model_charge holds %q at %d, want tokenfactory:read at the estimate %d", kind, price, estimate)
	}
}

// TestCancelledWorkIsReleasedAndBooksNothing pins that a call the caller
// cancelled is released with nothing booked. The caller's context is the cause
// both times: once cancelled, and once past its own deadline.
func TestCancelledWorkIsReleasedAndBooksNothing(t *testing.T) {
	cases := []struct {
		name string
		// run gives the caller's context and the work for one call: the work
		// returns the context's own error after the context finishes.
		run  func(t *testing.T) (ctx context.Context, work cost.Work)
		want error
	}{
		{
			name: "cancelled",
			run: func(t *testing.T) (context.Context, cost.Work) {
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				return ctx, func(wctx context.Context) (cost.Usage, error) {
					cancel()
					<-wctx.Done()
					return cost.Usage{}, fmt.Errorf("model: call cancelled: %w", wctx.Err())
				}
			},
			want: context.Canceled,
		},
		{
			name: "caller deadline expired",
			run: func(t *testing.T) (context.Context, cost.Work) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
				t.Cleanup(cancel)
				return ctx, func(wctx context.Context) (cost.Usage, error) {
					<-wctx.Done()
					return cost.Usage{}, fmt.Errorf("model: call stopped: %w", wctx.Err())
				}
			},
			want: context.DeadlineExceeded,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "before-yes.db")
			m := openMeter(t, path, map[spend.Pool]spend.PoolConfig{
				spend.PoolModel: {Limit: cost.Cent},
			})
			ctx, work := tc.run(t)

			_, err := m.Spend(ctx, spend.PoolModel, "tokenfactory:read", "check-1", 2*cost.Millicent, work)
			if !errors.Is(err, tc.want) {
				t.Fatalf("a call the caller gave up on returns its context error %v: %v", tc.want, err)
			}
			s, err := m.Status(context.Background(), spend.PoolModel)
			if err != nil {
				t.Fatal(err)
			}
			if want := (spend.Status{Limit: cost.Cent, Remaining: cost.Cent}); s != want {
				t.Fatalf("a released call must leave the pool whole: got %+v, want %+v", s, want)
			}
			if err := m.Close(); err != nil {
				t.Fatalf("close meter: %v", err)
			}
			db := freshDB(t, path)
			var charges, holds int
			if err := db.QueryRow(`SELECT COUNT(*) FROM cost_model_charge`).Scan(&charges); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT COUNT(*) FROM cost_model_reservation`).Scan(&holds); err != nil {
				t.Fatal(err)
			}
			if charges != 0 || holds != 0 {
				t.Fatalf("a released call left %d charges and %d holds, want none", charges, holds)
			}
		})
	}
}

// TestTavilyOpenedWithZeroPeriodIsMonthly pins that the tavily pool is monthly
// whatever the caller passes. The caller here leaves Period at its zero value,
// which is a lifetime ceiling everywhere else. The clock crosses a month start:
// the pool's ceiling is spent in March and frees again on 1 April.
func TestTavilyOpenedWithZeroPeriodIsMonthly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "before-yes.db")
	clock := &dayClock{at: time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)}
	m := openClockedMeter(t, path, clock, map[spend.Pool]spend.PoolConfig{
		spend.PoolTavily: {Limit: 2 * spend.CreditUnit},
	})
	ctx := context.Background()

	spendOnce(t, m, spend.PoolTavily, "tavily:search", "check-1", 2*spend.CreditUnit, 2*spend.CreditUnit)
	s, err := m.Status(ctx, spend.PoolTavily)
	if err != nil {
		t.Fatal(err)
	}
	if want := (spend.Status{Limit: 2 * spend.CreditUnit, Spent: 2 * spend.CreditUnit}); s.Spent != want.Spent || s.Remaining != 0 {
		t.Fatalf("the March ceiling must be spent: got %+v", s)
	}
	_, err = m.Spend(ctx, spend.PoolTavily, "tavily:search", "check-2", spend.CreditUnit,
		func(context.Context) (cost.Usage, error) {
			t.Fatal("work must not run once the March ceiling is spent")
			return cost.Usage{}, nil
		})
	if !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("a spent March ceiling must refuse the call: %v", err)
	}

	clock.at = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	s, err = m.Status(ctx, spend.PoolTavily)
	if err != nil {
		t.Fatal(err)
	}
	if want := (spend.Status{Limit: 2 * spend.CreditUnit, Spent: 0, Remaining: 2 * spend.CreditUnit}); s != want {
		t.Fatalf("a zero-period tavily pool must reset on the month start: got %+v, want %+v", s, want)
	}
	spendOnce(t, m, spend.PoolTavily, "tavily:search", "check-3", 2*spend.CreditUnit, 2*spend.CreditUnit)
}
