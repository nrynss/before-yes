// Package spend is the durable meter every paid call goes through. Each pool
// is a Keel cost store in the application's one database file, under its own
// namespace, and each holds a ceiling the caller passes in.
//
// Spend is the seam the pipeline adopts: it refuses a call the pool cannot
// cover before the call runs, holds the estimate while the call runs, settles
// what the call reported, and writes the charge to the pool's ledger with the
// check id as its Ref and a Kind naming the call. BeginCheck reserves one
// fresh check against the daily checks pool before the check's first paid
// call, and Check.Done or Check.Fail ends it. Status reports a pool's standing
// and Spent reports what one reference has cost. The pool names and their
// units live here, because the pipeline package that will spend through them
// does not exist yet; it calls this method set unchanged when it lands.
package spend

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nrynss/keel/cost"
	coststore "github.com/nrynss/keel/cost/sqlitestore"
	"github.com/nrynss/keel/sqlite"
)

// Pool names one budget pool. Every paid call names the pool it draws on.
type Pool string

const (
	// PoolModel is the Token Factory pool. It counts USD nanodollars, the
	// zero denomination.
	PoolModel Pool = "model"

	// PoolTavily is the Tavily pool. It counts thousandths of a credit:
	// CreditUnit minor units make one whole credit.
	PoolTavily Pool = "tavily"

	// PoolChecksDaily counts fresh checks per UTC day. It counts whole
	// checks, one minor unit each, so a limit of 5 admits five checks a day.
	PoolChecksDaily Pool = "checks_daily"
)

// CreditUnit is one whole Tavily credit in the tavily pool's unit. The pool
// counts a credit in thousandths, so one credit is CreditUnit minor units
// and a call that costs 1.5 credits reports 1500.
const CreditUnit cost.Price = 1000

// CreditDenomination names the tavily pool's unit, one Tavily credit counted
// in thousandths. The model pool keeps the zero denomination, USD
// nanodollars.
var CreditDenomination = cost.Denomination{Name: "tavily.credit", Minor: int(CreditUnit)}

// CheckDenomination names the checks pool's unit, one fresh check. A check
// exists only whole, so one minor unit is one check.
var CheckDenomination = cost.Denomination{Name: "check", Minor: 1}

// oneCheck is the price of one fresh check in the checks pool's unit.
const oneCheck cost.Price = 1

// checkKind is the Kind a settled check books under in the checks pool.
const checkKind = "check"

// DefaultReservationTTL is how long a hold stands when Config.ReservationTTL
// is zero. It must outlast the longest call and the longest fresh check, or a
// hold lapses while its work still runs and a concurrent reserve can take the
// headroom. internal/model lets one call run up to 15 minutes
// (model.DefaultTimeouts.Ceiling), and a fresh check runs several calls, so 30
// minutes leaves margin above a single call and holds a whole check.
const DefaultReservationTTL = 30 * time.Minute

var (
	// ErrNoPool reports a call against a pool the meter was not opened
	// with.
	ErrNoPool = errors.New("spend: no such pool")

	// ErrUnknownPool reports a pool name in Config that is not one of the
	// pools this package defines.
	ErrUnknownPool = errors.New("spend: unknown pool")

	// ErrCheckEnded reports a second Done or Fail on one Check. A check
	// ends once.
	ErrCheckEnded = errors.New("spend: check already ended")
)

// PoolConfig is one pool's ceiling and window in the shared database file.
type PoolConfig struct {
	// Limit is the pool's ceiling, in the pool's own unit: USD
	// nanodollars for PoolModel, thousandths of a credit for PoolTavily,
	// and checks for PoolChecksDaily. The caller owns the ceiling, so no
	// dollar, credit or check figure is chosen here.
	Limit cost.Price

	// Period restarts the ceiling on a calendar window. The zero value
	// keeps one lifetime ceiling, which is what PoolModel takes. Open
	// ignores this field for PoolTavily and PoolChecksDaily; see periodOf.
	Period cost.Period

	// Namespace names the store's own tables and migration ledger in the
	// shared file, which is how the pools keep their rows apart. Empty
	// means cost_<pool>, so the pools read cost_model, cost_tavily and
	// cost_checks_daily. A name ending in _owner is refused by the store,
	// because its budget table would collide with the base namespace's
	// owner-budget table.
	Namespace string
}

// Config configures Open.
type Config struct {
	// Path is the application's one database file. Every pool in Pools
	// opens its own store over this one file.
	Path string

	// Pools gives the ceiling and window of each pool to open. A pool the
	// map omits stays closed, and Spend refuses a call against it with
	// ErrNoPool.
	Pools map[Pool]PoolConfig

	// Now supplies the clock that stamps charges and judges reservation
	// expiry and window boundaries, on every pool. Nil means time.Now. Tests
	// set it to cross a UTC midnight or month start without sleeping.
	Now func() time.Time

	// ReservationTTL is how long every pool's hold stands before the store
	// purges it. Zero means DefaultReservationTTL. A hold must outlast the
	// call or check it covers, so set this above the longest call the
	// meter will see.
	ReservationTTL time.Duration
}

// Meter is the durable meter every paid call goes through. Build one with
// Open and release it with Close. Spend and BeginCheck are safe for
// concurrent use.
type Meter struct {
	db    *sqlite.DB
	pools map[Pool]*coststore.Store
}

// poolNames lists the pools this package defines, in a fixed order so Open
// opens them deterministically.
var poolNames = []Pool{PoolModel, PoolTavily, PoolChecksDaily}

// known reports whether name is a pool this package defines.
func known(name Pool) bool {
	return name == PoolModel || name == PoolTavily || name == PoolChecksDaily
}

// unitOf returns the unit a pool's prices count.
func unitOf(name Pool) cost.Denomination {
	switch name {
	case PoolTavily:
		return CreditDenomination
	case PoolChecksDaily:
		return CheckDenomination
	default:
		return cost.Denomination{}
	}
}

// periodOf returns the window a pool's ceiling restarts on. The tavily pool
// is pinned to cost.MonthlyUTC whatever the caller passes: Tavily's free
// tier is a monthly allowance, so a lifetime tavily ceiling would be 900
// credits for good rather than 900 a month. The checks pool is pinned to
// cost.DailyUTC for the same reason: the daily cap on fresh checks is a
// per-day limit. Neither pin is left to the caller, because a zero Period on
// either pool would silently change what the ceiling means. The model pool
// keeps the caller's Period, which is cost.None for its lifetime credit.
func periodOf(name Pool, caller cost.Period) cost.Period {
	switch name {
	case PoolTavily:
		return cost.MonthlyUTC
	case PoolChecksDaily:
		return cost.DailyUTC
	default:
		return caller
	}
}

// namespaceOf returns the namespace the pool's store owns. Empty means
// cost_<pool>.
func namespaceOf(name Pool, namespace string) string {
	if namespace == "" {
		return "cost_" + string(name)
	}
	return namespace
}

// Open opens one cost store per configured pool, all over cfg.Path, and
// returns the meter. Each pool's store owns its own namespace, so the pools
// share the one database file without sharing a ledger. It refuses a pool
// name this package does not define, and it returns the first store's error
// unchanged in the chain, so a namespace ending in _owner surfaces as
// sqlitestore.ErrInvalid.
func Open(ctx context.Context, cfg Config) (*Meter, error) {
	for name := range cfg.Pools {
		if !known(name) {
			return nil, fmt.Errorf("%w: %q", ErrUnknownPool, name)
		}
	}
	db, err := sqlite.Open(ctx, sqlite.Config{Path: cfg.Path})
	if err != nil {
		return nil, fmt.Errorf("spend: open database: %w", err)
	}
	ttl := cfg.ReservationTTL
	if ttl == 0 {
		ttl = DefaultReservationTTL
	}
	m := &Meter{db: db, pools: make(map[Pool]*coststore.Store, len(cfg.Pools))}
	for _, name := range poolNames {
		pc, ok := cfg.Pools[name]
		if !ok {
			continue
		}
		st, err := coststore.Open(ctx, coststore.Config{
			DB:             db,
			Namespace:      namespaceOf(name, pc.Namespace),
			Limit:          pc.Limit,
			Period:         periodOf(name, pc.Period),
			Denomination:   unitOf(name),
			Now:            cfg.Now,
			ReservationTTL: ttl,
		})
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("spend: open pool %s: %w", name, err)
		}
		m.pools[name] = st
	}
	return m, nil
}

// Close closes the meter's one database handle. Call it once no Spend is in
// flight. It is safe to call a second time.
func (m *Meter) Close() error {
	if m.db == nil {
		return nil
	}
	return m.db.Close()
}

// Spend runs one paid call through the meter against a pool.
//
// It reserves estimate before work runs, so a pool that cannot cover the
// estimate refuses the call with an error matching cost.ErrOverBudget and
// work never runs. The hold stands while work runs, so a concurrent call sees
// that budget as taken. The hold lasts Config.ReservationTTL, which defaults
// to DefaultReservationTTL, above the longest call; a call that outlived the
// TTL would let a second call reserve the same headroom. A context that is
// already done refuses the call before the reserve, so work never runs either.
//
// When work returns, Spend settles what the call reported. A reply that marks
// itself unmeasured settles at the estimate, and the usage Spend returns
// carries the price it settled at and the measured flag the call reported. A
// measured price below zero is refused with an error matching
// cost.ErrNegativePrice before anything books, because a refund is a separate
// charge and never a negative booking.
//
// A work that returns context.DeadlineExceeded while the caller's context is
// still live hit its own deadline. The host may have billed the tokens it
// wrote before the deadline, so Spend books the call at the estimate,
// unmeasured, and returns the work's error unchanged. Any other work error
// releases the hold and books nothing. So does a deadline the caller's own
// context carried, and a cancellation: a caller that gave up has not spent.
//
// The settled charge lands in the pool's ledger with Units 1, UnitPrice set
// to the settled price, Ref set to ref, Kind set to kind and the pool's own
// denomination. A charge the ledger refuses reports an error matching
// cost.ErrUnrecordedCharge: the call ran and its spend is booked against the
// pool, so that error is a missing record, not an unrun call.
//
// A context that finishes while work runs does not stop the booking: the call
// ran, so the settle and the charge run with the cancellation detached.
func (m *Meter) Spend(ctx context.Context, pool Pool, kind, ref string, estimate cost.Price, work cost.Work) (usage cost.Usage, err error) {
	p, ok := m.pools[pool]
	if !ok {
		return cost.Usage{}, fmt.Errorf("%w: %q", ErrNoPool, pool)
	}
	if err := ctx.Err(); err != nil {
		return cost.Usage{}, err
	}
	hold, err := p.Reserve(ctx, estimate)
	if err != nil {
		return cost.Usage{}, fmt.Errorf("spend: reserve on %s: %w", pool, err)
	}
	// booked says the settle consumed the hold. A work that fails or
	// panics leaves the hold to the deferred release, which runs on a
	// context of its own, because the call's own cancellation may be the
	// reason the hold must go back.
	booked := false
	defer func() {
		if booked {
			return
		}
		if rerr := p.Release(context.WithoutCancel(ctx), hold); rerr != nil {
			err = errors.Join(err, fmt.Errorf("spend: release hold on %s: %w", pool, rerr))
		}
	}()
	reply, workErr := work(ctx)
	timedOut := workErr != nil && bookableTimeout(ctx, workErr)
	if workErr != nil && !timedOut {
		return cost.Usage{}, workErr
	}
	price := reply.Price
	switch {
	case timedOut, !reply.Measured:
		price = estimate
	case price < 0:
		return cost.Usage{}, fmt.Errorf("%w: %d", cost.ErrNegativePrice, price)
	}
	measured := reply.Measured && !timedOut
	// The call ran, so the settle and the charge land whatever the
	// caller's context does afterwards.
	booking := context.WithoutCancel(ctx)
	if err := p.Settle(booking, hold, price); err != nil {
		return cost.Usage{}, errors.Join(workErr, fmt.Errorf("spend: settle on %s: %w", pool, err))
	}
	booked = true
	if err := p.Add(booking, cost.Charge{
		Kind:         kind,
		Units:        1,
		UnitPrice:    price,
		Ref:          ref,
		Denomination: p.Denomination(),
	}); err != nil {
		return cost.Usage{}, errors.Join(workErr, fmt.Errorf("%w: %w", cost.ErrUnrecordedCharge, err))
	}
	return cost.Usage{Price: price, Measured: measured}, workErr
}

// bookableTimeout reports whether a work error is a deadline the call hit
// itself: it matches context.DeadlineExceeded and the caller's context is not
// done. A deadline the caller's own context carried is not one, because that
// caller gave up.
func bookableTimeout(ctx context.Context, err error) bool {
	return errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil
}

// Check is one fresh check reserved against the checks pool. BeginCheck
// reserves it; the caller ends it with exactly one of Done or Fail.
type Check struct {
	pool  *coststore.Store
	ref   string
	hold  coststore.Reservation
	ended bool
}

// BeginCheck reserves one fresh check against the checks pool before the
// check's first paid call, and returns the Check that holds it. A pool with
// no checks left refuses with an error matching cost.ErrOverBudget, and the
// caller makes no paid call. The hold stands until Done or Fail, and it
// counts against the day's headroom while the check runs, so checks in
// flight count toward the cap too.
//
// The hold lasts Config.ReservationTTL (DefaultReservationTTL when zero), so a
// check that finishes inside that window holds its slot the whole time. A
// check that runs past the TTL loses its hold and can be overtaken by a
// second check; that is the one remaining risk. Done books the check when it
// ends whether or not the hold lapsed, so the day's count stays right.
//
// The caller calls BeginCheck only for a fresh check, one with no cache hit.
// A check answered from the cache never reaches BeginCheck, so it neither
// reserves nor books against the cap.
func (m *Meter) BeginCheck(ctx context.Context, ref string) (*Check, error) {
	p, ok := m.pools[PoolChecksDaily]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNoPool, PoolChecksDaily)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hold, err := p.Reserve(ctx, oneCheck)
	if err != nil {
		return nil, fmt.Errorf("spend: reserve check on %s: %w", PoolChecksDaily, err)
	}
	return &Check{pool: p, ref: ref, hold: hold}, nil
}

// Done settles the check as one check and books it in the checks pool under
// its ref, so the day's count includes it. A booking the ledger refuses
// reports an error matching cost.ErrUnrecordedCharge, as Spend does. Done
// runs detached from any context, because the check ran.
func (c *Check) Done() error {
	if c.ended {
		return ErrCheckEnded
	}
	c.ended = true
	booking := context.Background()
	if err := c.pool.Settle(booking, c.hold, oneCheck); err != nil {
		return errors.Join(fmt.Errorf("spend: settle check: %w", err), c.pool.Release(booking, c.hold))
	}
	if err := c.pool.Add(booking, cost.Charge{
		Kind:         checkKind,
		Units:        1,
		UnitPrice:    oneCheck,
		Ref:          c.ref,
		Denomination: c.pool.Denomination(),
	}); err != nil {
		return fmt.Errorf("%w: %w", cost.ErrUnrecordedCharge, err)
	}
	return nil
}

// Fail releases the check's hold and books nothing, so the check does not
// count against the day. Call it when the check fails before it completes.
func (c *Check) Fail() error {
	if c.ended {
		return ErrCheckEnded
	}
	c.ended = true
	if err := c.pool.Release(context.Background(), c.hold); err != nil {
		return fmt.Errorf("spend: release check hold: %w", err)
	}
	return nil
}

// Status is one pool's standing, in the pool's own unit.
type Status struct {
	// Limit is the pool's ceiling for the current window.
	Limit cost.Price

	// Spent is the price the pool booked for the current window.
	Spent cost.Price

	// Remaining is the headroom the next reservation may use: Limit less
	// Spent less the holds still standing.
	Remaining cost.Price
}

// Status reports a pool's standing. Over a lifetime pool it reads all spend
// ever booked; over a windowed pool it reads the current window.
func (m *Meter) Status(ctx context.Context, pool Pool) (Status, error) {
	p, ok := m.pools[pool]
	if !ok {
		return Status{}, fmt.Errorf("%w: %q", ErrNoPool, pool)
	}
	var s Status
	var err error
	if s.Limit, err = p.Limit(ctx); err != nil {
		return Status{}, fmt.Errorf("spend: status on %s: %w", pool, err)
	}
	if s.Spent, err = p.Spent(ctx); err != nil {
		return Status{}, fmt.Errorf("spend: status on %s: %w", pool, err)
	}
	if s.Remaining, err = p.Remaining(ctx); err != nil {
		return Status{}, fmt.Errorf("spend: status on %s: %w", pool, err)
	}
	return s, nil
}

// Spent reports what one reference has cost in a pool: the sum of every
// charge whose Ref equals ref. It reads the pool's whole ledger, so a
// windowed pool still answers for a check booked in an earlier window.
func (m *Meter) Spent(ctx context.Context, pool Pool, ref string) (cost.Price, error) {
	p, ok := m.pools[pool]
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrNoPool, pool)
	}
	total, err := p.TotalForRef(ctx, ref)
	if err != nil {
		return 0, fmt.Errorf("spend: spent on %s: %w", pool, err)
	}
	return total, nil
}
