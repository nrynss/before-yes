// Package spend is the durable meter every paid call goes through. Each pool
// is a Keel cost store in the application's one database file, under its own
// namespace, and each holds a ceiling the caller passes in.
//
// Spend is the seam the pipeline adopts: it refuses a call the pool cannot
// cover before the call runs, holds the estimate while the call runs, settles
// what the call reported, and writes the charge to the pool's ledger with the
// check id as its Ref and a Kind naming the call. Status reports a pool's
// standing and Spent reports what one reference has cost. The pool names and
// their units live here, because the pipeline package that will spend through
// them does not exist yet; it calls this method set unchanged when it lands.
package spend

import (
	"context"
	"errors"
	"fmt"

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
)

// CreditUnit is one whole Tavily credit in the tavily pool's unit. The pool
// counts a credit in thousandths, so one credit is CreditUnit minor units
// and a call that costs 1.5 credits reports 1500.
const CreditUnit cost.Price = 1000

// CreditDenomination names the tavily pool's unit, one Tavily credit counted
// in thousandths. The model pool keeps the zero denomination, USD
// nanodollars.
var CreditDenomination = cost.Denomination{Name: "tavily.credit", Minor: int(CreditUnit)}

var (
	// ErrNoPool reports a call against a pool the meter was not opened
	// with.
	ErrNoPool = errors.New("spend: no such pool")

	// ErrUnknownPool reports a pool name in Config that is not one of the
	// pools this package defines.
	ErrUnknownPool = errors.New("spend: unknown pool")
)

// PoolConfig is one pool's ceiling and window in the shared database file.
type PoolConfig struct {
	// Limit is the pool's ceiling, in the pool's own unit: USD
	// nanodollars for PoolModel, thousandths of a credit for PoolTavily.
	// The caller owns the ceiling, so no dollar or credit figure is
	// chosen here.
	Limit cost.Price

	// Period restarts the ceiling on a calendar window. The zero value
	// keeps one lifetime ceiling, the shape PoolModel's ceiling takes.
	// PoolTavily's ceiling bounds one month, so it takes
	// cost.MonthlyUTC.
	Period cost.Period

	// Namespace names the store's own tables and migration ledger in the
	// shared file, which is how the pools keep their rows apart. Empty
	// means cost_<pool>, so PoolModel and PoolTavily read cost_model and
	// cost_tavily. A name ending in _owner is refused by the store,
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
}

// Meter is the durable meter every paid call goes through. Build one with
// Open and release it with Close. Spend is safe for concurrent use.
type Meter struct {
	db    *sqlite.DB
	pools map[Pool]*coststore.Store
}

// poolNames lists the pools this package defines, in a fixed order so Open
// opens them deterministically.
var poolNames = []Pool{PoolModel, PoolTavily}

// known reports whether name is a pool this package defines.
func known(name Pool) bool {
	return name == PoolModel || name == PoolTavily
}

// unitOf returns the unit a pool's prices count. PoolModel counts USD
// nanodollars, the zero denomination, and PoolTavily counts thousandths of a
// credit.
func unitOf(name Pool) cost.Denomination {
	if name == PoolTavily {
		return CreditDenomination
	}
	return cost.Denomination{}
}

// namespaceOf returns the namespace the pool's store owns. Empty means
// cost_<pool>, so the two pools read cost_model and cost_tavily.
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
	m := &Meter{db: db, pools: make(map[Pool]*coststore.Store, len(cfg.Pools))}
	for _, name := range poolNames {
		pc, ok := cfg.Pools[name]
		if !ok {
			continue
		}
		st, err := coststore.Open(ctx, coststore.Config{
			DB:           db,
			Namespace:    namespaceOf(name, pc.Namespace),
			Limit:        pc.Limit,
			Period:       pc.Period,
			Denomination: unitOf(name),
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
// that budget as taken. The hold expires with the store's reservation TTL,
// which the meter does not expose and leaves at the store's default of ten
// minutes; a call that outlives the TTL can let a second call reserve the
// same headroom. A context that is already done refuses the call before the
// reserve, so work never runs either.
//
// When work returns, Spend settles what the call reported. A reply that marks
// itself unmeasured settles at the estimate, and the usage Spend returns
// carries the price it settled at and the measured flag the call reported. A
// measured price below zero is refused with an error matching
// cost.ErrNegativePrice before anything books, because a refund is a separate
// charge and never a negative booking.
//
// The settled charge lands in the pool's ledger with Units 1, UnitPrice set
// to the settled price, Ref set to ref, Kind set to kind and the pool's own
// denomination. A charge the ledger refuses reports an error matching
// cost.ErrUnrecordedCharge: the call ran and its spend is booked against the
// pool, so that error is a missing record, not an unrun call.
//
// A work that fails returns its error and frees its hold, and nothing is
// booked. A release that itself fails is joined onto that error, and the hold
// then lasts until it expires. A context that finishes while work runs does
// not stop the booking: the call ran, so the settle and the charge run with
// the cancellation detached.
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
	usage, err = work(ctx)
	if err != nil {
		return cost.Usage{}, err
	}
	price := usage.Price
	if !usage.Measured {
		price = estimate
	} else if price < 0 {
		return cost.Usage{}, fmt.Errorf("%w: %d", cost.ErrNegativePrice, price)
	}
	// The call ran, so the settle and the charge land whatever the
	// caller's context does afterwards.
	booking := context.WithoutCancel(ctx)
	if err := p.Settle(booking, hold, price); err != nil {
		return cost.Usage{}, fmt.Errorf("spend: settle on %s: %w", pool, err)
	}
	booked = true
	if err := p.Add(booking, cost.Charge{
		Kind:         kind,
		Units:        1,
		UnitPrice:    price,
		Ref:          ref,
		Denomination: p.Denomination(),
	}); err != nil {
		return cost.Usage{}, fmt.Errorf("%w: %w", cost.ErrUnrecordedCharge, err)
	}
	return cost.Usage{Price: price, Measured: usage.Measured}, nil
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
