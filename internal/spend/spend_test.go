package spend_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nrynss/keel/cost"
	coststore "github.com/nrynss/keel/cost/sqlitestore"
	_ "modernc.org/sqlite"

	"github.com/nrynss/before-yes/internal/spend"
)

// openMeter opens a meter over path and closes it when the test ends.
func openMeter(t *testing.T, path string, pools map[spend.Pool]spend.PoolConfig) *spend.Meter {
	t.Helper()
	m, err := spend.Open(context.Background(), spend.Config{Path: path, Pools: pools})
	if err != nil {
		t.Fatalf("open meter: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

// spendOnce runs one successful measured call through the meter.
func spendOnce(t *testing.T, m *spend.Meter, pool spend.Pool, kind, ref string, estimate, price cost.Price) {
	t.Helper()
	usage, err := m.Spend(context.Background(), pool, kind, ref, estimate,
		func(context.Context) (cost.Usage, error) {
			return cost.Usage{Price: price, Measured: true}, nil
		})
	if err != nil {
		t.Fatalf("spend %s %s: %v", pool, kind, err)
	}
	if usage.Price != price || !usage.Measured {
		t.Fatalf("spend %s %s settled at %+v, want %d measured", pool, kind, usage, price)
	}
}

// freshDB opens path through database/sql directly, so a test reads the file
// the way any other process would rather than through the store that wrote it.
func freshDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// dbFiles returns the .db files in dir. WAL and shared-memory sidecars are not
// databases, so they do not count.
func dbFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".db" {
			names = append(names, e.Name())
		}
	}
	return names
}

// TestOverBudgetEstimateIsRefusedBeforeTheCall pins the refusal the pool owes
// a call it cannot cover: the error is cost.ErrOverBudget and the work never
// runs.
func TestOverBudgetEstimateIsRefusedBeforeTheCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "before-yes.db")
	m := openMeter(t, path, map[spend.Pool]spend.PoolConfig{
		spend.PoolModel: {Limit: cost.Cent},
	})
	ctx := context.Background()

	ran := false
	_, err := m.Spend(ctx, spend.PoolModel, "tokenfactory:read", "check-1", 2*cost.Cent,
		func(context.Context) (cost.Usage, error) {
			ran = true
			return cost.Usage{Price: cost.Cent, Measured: true}, nil
		})
	if !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("an estimate over the pool must be refused with cost.ErrOverBudget: %v", err)
	}
	if ran {
		t.Fatal("work must not run when the pool cannot cover the estimate")
	}
	got, err := m.Status(ctx, spend.PoolModel)
	if err != nil {
		t.Fatal(err)
	}
	want := spend.Status{Limit: cost.Cent, Remaining: cost.Cent}
	if got != want {
		t.Fatalf("a refused call must leave the pool untouched: got %+v, want %+v", got, want)
	}

	// The refusal reads the headroom the pool has left, not only its
	// ceiling: an estimate the pool's remaining spend cannot cover is
	// refused too, and it leaves the headroom alone.
	spendOnce(t, m, spend.PoolModel, "tokenfactory:read", "check-2", 300*cost.Millicent, 300*cost.Millicent)
	ran = false
	_, err = m.Spend(ctx, spend.PoolModel, "tokenfactory:read", "check-3", 800*cost.Millicent,
		func(context.Context) (cost.Usage, error) {
			ran = true
			return cost.Usage{Price: 800 * cost.Millicent, Measured: true}, nil
		})
	if !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("an estimate over the pool's remaining budget must be refused: %v", err)
	}
	if ran {
		t.Fatal("work must not run when the pool's remaining budget cannot cover the estimate")
	}
	got, err = m.Status(ctx, spend.PoolModel)
	if err != nil {
		t.Fatal(err)
	}
	want = spend.Status{Limit: cost.Cent, Spent: 300 * cost.Millicent, Remaining: 700 * cost.Millicent}
	if got != want {
		t.Fatalf("a refused call must leave the pool untouched: got %+v, want %+v", got, want)
	}
}

// TestFailedCallReleasesItsHold pins both halves of the hold rule: the
// estimate is committed while the call runs, and the hold goes back when the
// call fails, with nothing booked.
func TestFailedCallReleasesItsHold(t *testing.T) {
	path := filepath.Join(t.TempDir(), "before-yes.db")
	m := openMeter(t, path, map[spend.Pool]spend.PoolConfig{
		spend.PoolModel: {Limit: cost.Cent},
	})
	ctx := context.Background()

	before, err := m.Status(ctx, spend.PoolModel)
	if err != nil {
		t.Fatal(err)
	}
	estimate := 4 * cost.Millicent
	held := false
	boom := errors.New("the model call failed")
	_, err = m.Spend(ctx, spend.PoolModel, "tokenfactory:read", "check-1", estimate,
		func(wctx context.Context) (cost.Usage, error) {
			s, err := m.Status(wctx, spend.PoolModel)
			if err != nil {
				return cost.Usage{}, err
			}
			held = s.Remaining == before.Remaining-estimate
			return cost.Usage{}, boom
		})
	if !errors.Is(err, boom) {
		t.Fatalf("a failed call returns the work's error: %v", err)
	}
	if !held {
		t.Fatal("the estimate must be held while the call runs")
	}
	after, err := m.Status(ctx, spend.PoolModel)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("a failed call must release its hold: before %+v, after %+v", before, after)
	}
	spent, err := m.Spent(ctx, spend.PoolModel, "check-1")
	if err != nil {
		t.Fatal(err)
	}
	if spent != 0 {
		t.Fatalf("a failed call must book nothing: %d", spent)
	}

	// A fresh connection sees no charge and no hold left behind.
	db := freshDB(t, path)
	var charges, holds int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cost_model_charge`).Scan(&charges); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM cost_model_reservation`).Scan(&holds); err != nil {
		t.Fatal(err)
	}
	if charges != 0 || holds != 0 {
		t.Fatalf("a failed call left %d charges and %d holds behind, want none", charges, holds)
	}
}

// TestSuccessfulCallBooksAChargeTheLedgerKeeps reads the charge back through a
// fresh connection, from the table Keel's own schema names.
func TestSuccessfulCallBooksAChargeTheLedgerKeeps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "before-yes.db")
	m := openMeter(t, path, map[spend.Pool]spend.PoolConfig{
		spend.PoolModel: {Limit: cost.Cent},
	})
	ctx := context.Background()

	spendOnce(t, m, spend.PoolModel, "tokenfactory:read", "check-1", 3*cost.Millicent, 3*cost.Millicent)
	spendOnce(t, m, spend.PoolModel, "tokenfactory:judge", "check-1", 2*cost.Millicent, 2*cost.Millicent)
	spendOnce(t, m, spend.PoolModel, "tokenfactory:read", "check-2", cost.Millicent, cost.Millicent)

	got, err := m.Spent(ctx, spend.PoolModel, "check-1")
	if err != nil {
		t.Fatal(err)
	}
	if want := 5 * cost.Millicent; got != want {
		t.Fatalf("Spent(check-1) = %d, want %d", got, want)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("close meter: %v", err)
	}
	if dbs := dbFiles(t, dir); len(dbs) != 1 || dbs[0] != "before-yes.db" {
		t.Fatalf("want one database file, got %v", dbs)
	}

	db := freshDB(t, path)
	rows, err := db.Query(`SELECT kind, units, unit_price, ref FROM cost_model_charge ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type charge struct {
		kind  string
		units int
		price cost.Price
		ref   string
	}
	var gotRows []charge
	for rows.Next() {
		var c charge
		var units, price int64
		if err := rows.Scan(&c.kind, &units, &price, &c.ref); err != nil {
			t.Fatal(err)
		}
		c.units = int(units)
		c.price = cost.Price(price)
		gotRows = append(gotRows, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	wantRows := []charge{
		{kind: "tokenfactory:read", units: 1, price: 3 * cost.Millicent, ref: "check-1"},
		{kind: "tokenfactory:judge", units: 1, price: 2 * cost.Millicent, ref: "check-1"},
		{kind: "tokenfactory:read", units: 1, price: cost.Millicent, ref: "check-2"},
	}
	if len(gotRows) != len(wantRows) {
		t.Fatalf("cost_model_charge holds %d rows, want %d: %+v", len(gotRows), len(wantRows), gotRows)
	}
	for i := range wantRows {
		if gotRows[i] != wantRows[i] {
			t.Fatalf("cost_model_charge[%d] = %+v, want %+v", i, gotRows[i], wantRows[i])
		}
	}
	var settles int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cost_model_settle`).Scan(&settles); err != nil {
		t.Fatal(err)
	}
	if settles != len(wantRows) {
		t.Fatalf("cost_model_settle holds %d rows, want %d", settles, len(wantRows))
	}
	var limit, spent int64
	if err := db.QueryRow(`SELECT limit_nd, spent_nd FROM cost_model_budget WHERE id = 1`).Scan(&limit, &spent); err != nil {
		t.Fatal(err)
	}
	if cost.Price(limit) != cost.Cent || cost.Price(spent) != 6*cost.Millicent {
		t.Fatalf("cost_model_budget holds limit %d spent %d, want %d and %d",
			limit, spent, cost.Cent, 6*cost.Millicent)
	}
}

// TestPoolsMakeTheirLedgersApartInOneFile pins two stores in one database file:
// each pool books under its own namespace, each speaks its own unit, and
// neither ledger holds the other's charge.
func TestPoolsMakeTheirLedgersApartInOneFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "before-yes.db")
	m := openMeter(t, path, map[spend.Pool]spend.PoolConfig{
		spend.PoolModel:  {Limit: 10 * cost.Cent},
		spend.PoolTavily: {Limit: 100 * spend.CreditUnit, Period: cost.MonthlyUTC},
	})
	ctx := context.Background()

	spendOnce(t, m, spend.PoolModel, "tokenfactory:read", "check-1", 3*cost.Millicent, 3*cost.Millicent)
	spendOnce(t, m, spend.PoolTavily, "tavily:search", "check-1", 2*spend.CreditUnit, 2*spend.CreditUnit)

	model, err := m.Status(ctx, spend.PoolModel)
	if err != nil {
		t.Fatal(err)
	}
	if want := (spend.Status{
		Limit:     10 * cost.Cent,
		Spent:     3 * cost.Millicent,
		Remaining: 10*cost.Cent - 3*cost.Millicent,
	}); model != want {
		t.Fatalf("model pool standing = %+v, want %+v", model, want)
	}
	tavily, err := m.Status(ctx, spend.PoolTavily)
	if err != nil {
		t.Fatal(err)
	}
	if want := (spend.Status{
		Limit:     100 * spend.CreditUnit,
		Spent:     2 * spend.CreditUnit,
		Remaining: 98 * spend.CreditUnit,
	}); tavily != want {
		t.Fatalf("tavily pool standing = %+v, want %+v", tavily, want)
	}
	// One reference, priced by each pool in that pool's own unit.
	modelRef, err := m.Spent(ctx, spend.PoolModel, "check-1")
	if err != nil {
		t.Fatal(err)
	}
	if want := 3 * cost.Millicent; modelRef != want {
		t.Fatalf("the model pool spent %d on check-1, want %d", modelRef, want)
	}
	tavilyRef, err := m.Spent(ctx, spend.PoolTavily, "check-1")
	if err != nil {
		t.Fatal(err)
	}
	if want := 2 * spend.CreditUnit; tavilyRef != want {
		t.Fatalf("the tavily pool spent %d on check-1, want %d", tavilyRef, want)
	}

	// A fresh connection while the meter is open: both namespaces' tables
	// are in the one committed file. The namespace names and column names
	// are Keel's schema, read here rather than guessed.
	db := freshDB(t, path)
	var tables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('cost_model_charge', 'cost_tavily_charge')`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 2 {
		t.Fatalf("the shared file holds %d of the two charge tables, want 2", tables)
	}
	var modelKind, modelDenomination, tavilyKind, tavilyDenomination string
	var modelPrice, tavilyPrice int64
	if err := db.QueryRow(`SELECT kind, unit_price, denomination FROM cost_model_charge`).
		Scan(&modelKind, &modelPrice, &modelDenomination); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT kind, unit_price, denomination FROM cost_tavily_charge`).
		Scan(&tavilyKind, &tavilyPrice, &tavilyDenomination); err != nil {
		t.Fatal(err)
	}
	if modelKind != "tokenfactory:read" || cost.Price(modelPrice) != 3*cost.Millicent || modelDenomination != "" {
		t.Fatalf("cost_model_charge holds %q %d %q, want tokenfactory:read in nanodollars",
			modelKind, modelPrice, modelDenomination)
	}
	if tavilyKind != "tavily:search" || cost.Price(tavilyPrice) != 2*spend.CreditUnit || tavilyDenomination != spend.CreditDenomination.Name {
		t.Fatalf("cost_tavily_charge holds %q %d %q, want tavily:search in %q",
			tavilyKind, tavilyPrice, tavilyDenomination, spend.CreditDenomination.Name)
	}

	if err := m.Close(); err != nil {
		t.Fatalf("close meter: %v", err)
	}
	if dbs := dbFiles(t, dir); len(dbs) != 1 || dbs[0] != "before-yes.db" {
		t.Fatalf("both pools must live in one database file, got %v", dbs)
	}
}

// TestOpenRefusesOwnerNamespace pins Keel's refusal of a namespace ending in
// _owner, which would otherwise share a table with the base namespace.
func TestOpenRefusesOwnerNamespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "before-yes.db")
	m, err := spend.Open(context.Background(), spend.Config{
		Path: path,
		Pools: map[spend.Pool]spend.PoolConfig{
			spend.PoolModel: {Limit: cost.Cent, Namespace: "cost_model_owner"},
		},
	})
	if !errors.Is(err, coststore.ErrInvalid) {
		t.Fatalf("a namespace ending in _owner must be refused with sqlitestore.ErrInvalid: %v", err)
	}
	if m != nil {
		t.Fatal("a refused open must not return a meter")
	}
	// Keel refuses the suffix before any migration runs, so the file it
	// touched holds no store tables.
	db := freshDB(t, path)
	var tables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatalf("a refused open left %d tables behind, want 0", tables)
	}
}

// TestOpenRefusesUnknownPool pins that a pool name the package does not define
// cannot become a silent pool of some other unit.
func TestOpenRefusesUnknownPool(t *testing.T) {
	_, err := spend.Open(context.Background(), spend.Config{
		Path: filepath.Join(t.TempDir(), "before-yes.db"),
		Pools: map[spend.Pool]spend.PoolConfig{
			"tokenfactory": {Limit: cost.Cent},
		},
	})
	if !errors.Is(err, spend.ErrUnknownPool) {
		t.Fatalf("a pool name outside the two this package defines must be refused: %v", err)
	}
}

// TestUnmeasuredUsageSettlesAtTheEstimate pins that a reply which cannot price
// itself books the estimate, both in the pool and in the ledger, and ignores
// any price the unmeasured reply carried.
func TestUnmeasuredUsageSettlesAtTheEstimate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "before-yes.db")
	m := openMeter(t, path, map[spend.Pool]spend.PoolConfig{
		spend.PoolModel: {Limit: cost.Cent},
	})
	ctx := context.Background()

	estimate := 2 * cost.Millicent
	usage, err := m.Spend(ctx, spend.PoolModel, "tokenfactory:read", "check-1", estimate,
		func(context.Context) (cost.Usage, error) {
			// The call could not measure its own price.
			return cost.Usage{Price: cost.Cent, Measured: false}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if usage.Measured || usage.Price != estimate {
		t.Fatalf("an unmeasured reply settles at the estimate: %+v, want price %d unmeasured", usage, estimate)
	}
	s, err := m.Status(ctx, spend.PoolModel)
	if err != nil {
		t.Fatal(err)
	}
	if s.Spent != estimate {
		t.Fatalf("the estimate must be booked: %+v", s)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("close meter: %v", err)
	}
	db := freshDB(t, path)
	var price int64
	if err := db.QueryRow(`SELECT unit_price FROM cost_model_charge WHERE ref = 'check-1'`).Scan(&price); err != nil {
		t.Fatal(err)
	}
	if cost.Price(price) != estimate {
		t.Fatalf("the charge carries %d, want the estimate %d", price, estimate)
	}
}

// TestCancelledContextStillLandsTheBooking pins that a call which ran books
// even when the caller's context dies while it runs.
func TestCancelledContextStillLandsTheBooking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "before-yes.db")
	m := openMeter(t, path, map[spend.Pool]spend.PoolConfig{
		spend.PoolModel: {Limit: cost.Cent},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	price := 3 * cost.Millicent
	usage, err := m.Spend(ctx, spend.PoolModel, "tokenfactory:read", "check-1", 2*cost.Millicent,
		func(wctx context.Context) (cost.Usage, error) {
			cancel()
			if wctx.Err() == nil {
				t.Error("the work must observe the caller's cancellation")
			}
			return cost.Usage{Price: price, Measured: true}, nil
		})
	if err != nil {
		t.Fatalf("a call that ran must book even when its context is cancelled: %v", err)
	}
	if usage.Price != price || !usage.Measured {
		t.Fatalf("the call settled at %+v, want %d measured", usage, price)
	}
	s, err := m.Status(context.Background(), spend.PoolModel)
	if err != nil {
		t.Fatal(err)
	}
	if want := (spend.Status{
		Limit:     cost.Cent,
		Spent:     price,
		Remaining: cost.Cent - price,
	}); s != want {
		t.Fatalf("a cancelled context must not stop the booking: got %+v, want %+v", s, want)
	}
}

// TestCancelledContextRefusesBeforeTheCall pins the other side of the same
// rule: a call whose context is already done never runs.
func TestCancelledContextRefusesBeforeTheCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "before-yes.db")
	m := openMeter(t, path, map[spend.Pool]spend.PoolConfig{
		spend.PoolModel: {Limit: cost.Cent},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ran := false
	_, err := m.Spend(ctx, spend.PoolModel, "tokenfactory:read", "check-1", cost.Millicent,
		func(context.Context) (cost.Usage, error) {
			ran = true
			return cost.Usage{}, nil
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a call with a done context must be refused with context.Canceled: %v", err)
	}
	if ran {
		t.Fatal("work must not run when the context is already done")
	}
	s, err := m.Status(context.Background(), spend.PoolModel)
	if err != nil {
		t.Fatal(err)
	}
	if want := (spend.Status{Limit: cost.Cent, Remaining: cost.Cent}); s != want {
		t.Fatalf("a refused call must leave the pool untouched: got %+v, want %+v", s, want)
	}
}

// TestUnopenedPoolIsRefused pins ErrNoPool on every method that names a pool.
func TestUnopenedPoolIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "before-yes.db")
	m := openMeter(t, path, map[spend.Pool]spend.PoolConfig{
		spend.PoolModel: {Limit: cost.Cent},
	})
	ctx := context.Background()
	work := func(context.Context) (cost.Usage, error) { return cost.Usage{}, nil }

	if _, err := m.Spend(ctx, spend.PoolTavily, "tavily:search", "check-1", 0, work); !errors.Is(err, spend.ErrNoPool) {
		t.Fatalf("Spend must refuse an unopened pool: %v", err)
	}
	if _, err := m.Status(ctx, spend.PoolTavily); !errors.Is(err, spend.ErrNoPool) {
		t.Fatalf("Status must refuse an unopened pool: %v", err)
	}
	if _, err := m.Spent(ctx, spend.PoolTavily, "check-1"); !errors.Is(err, spend.ErrNoPool) {
		t.Fatalf("Spent must refuse an unopened pool: %v", err)
	}
}
