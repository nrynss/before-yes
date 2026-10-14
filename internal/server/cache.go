package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nrynss/keel/cache"

	"github.com/nrynss/before-yes/internal/appstore"
	"github.com/nrynss/before-yes/internal/cards"
	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/pipeline"
)

// Default lifetimes for the two cache levels.
const (
	DefaultCardMaxAge     = 168 * time.Hour
	DefaultEvidenceMaxAge = 168 * time.Hour
)

// ErrInvalidConfig reports unusable configuration, such as a negative age.
var ErrInvalidConfig = errors.New("server: invalid cache config")

// uncacheableEvidenceError carries evidence that Collect decided not to cache
// (for example due to failed reader chunks or missing source failures).
// Returning an error from the maker ensures Keel's GetOrMake does not store it,
// while allowing the in-flight callers to unwrap and receive the evidence.
type uncacheableEvidenceError struct {
	ev  pipeline.Evidence
	hit bool
}

func (e uncacheableEvidenceError) Error() string {
	return "server: uncacheable evidence"
}

// flightState tracks the in-process singleflight leader for an evidence key.
type flightState struct {
	leaderToken uint64
	pCollectHit bool
	putCalled   bool
	putPayload  []byte
}

// EvidenceCacheConfig configures NewEvidenceCache.
type EvidenceCacheConfig struct {
	Store  cache.Store
	MaxAge time.Duration
	Now    func() time.Time
	Log    *slog.Logger
}

// EvidenceCache implements pipeline.EvidenceCache over a Keel cache.Store, and
// provides single-flight collection via Collect. It reuses pipeline.EvidenceKey
// as the cache key.
type EvidenceCache struct {
	store  cache.Store
	maxAge time.Duration
	now    func() time.Time
	log    *slog.Logger
	cache  *cache.Cache

	mu            sync.Mutex
	activeFlights map[string]*flightState
	seq           uint64
}

// NewEvidenceCache creates an EvidenceCache from cfg. Store is required.
func NewEvidenceCache(cfg EvidenceCacheConfig) (*EvidenceCache, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrInvalidConfig)
	}
	if cfg.MaxAge < 0 {
		return nil, fmt.Errorf("%w: MaxAge must not be negative", ErrInvalidConfig)
	}
	maxAge := cfg.MaxAge
	if maxAge == 0 {
		maxAge = DefaultEvidenceMaxAge
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	kc, err := cache.New(cache.Config{
		Store:  cfg.Store,
		MaxAge: maxAge,
		Now:    now,
		Log:    log,
	})
	if err != nil {
		return nil, fmt.Errorf("server: new evidence cache: %w", err)
	}

	return &EvidenceCache{
		store:         cfg.Store,
		maxAge:        maxAge,
		now:           now,
		log:           log,
		cache:         kc,
		activeFlights: make(map[string]*flightState),
	}, nil
}

// activeFlightsCount returns the number of active singleflight deduplication entries.
func (c *EvidenceCache) activeFlightsCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.activeFlights)
}

// Get returns the stored evidence under key. It satisfies pipeline.EvidenceCache.
// Only live entries with content-type "application/json" are returned.
// ErrNotFound and expired entries return (Evidence{}, false, nil). A store fault
// or undecodable payload returns an error.
func (c *EvidenceCache) Get(ctx context.Context, key string) (pipeline.Evidence, bool, error) {
	entry, err := c.store.Get(ctx, key)
	if errors.Is(err, cache.ErrNotFound) {
		return pipeline.Evidence{}, false, nil
	}
	if err != nil {
		return pipeline.Evidence{}, false, err
	}
	if entry.Refusal {
		return pipeline.Evidence{}, false, nil
	}
	if c.now().Sub(entry.CreatedAt) >= c.maxAge {
		return pipeline.Evidence{}, false, nil
	}
	if entry.ContentType != "application/json" {
		return pipeline.Evidence{}, false, fmt.Errorf("server: unexpected evidence content type: %q", entry.ContentType)
	}
	var ev pipeline.Evidence
	if err := json.Unmarshal(entry.Payload, &ev); err != nil {
		return pipeline.Evidence{}, false, fmt.Errorf("server: unmarshal evidence: %w", err)
	}
	return ev, true, nil
}

// Put stores ev under key with content-type "application/json" and the current
// timestamp. It satisfies pipeline.EvidenceCache. If key is actively being
// collected in an in-process flight, Put stages the payload for the flight maker
// to hand to Keel's storeResult, avoiding a redundant double-write.
func (c *EvidenceCache) Put(ctx context.Context, key string, ev pipeline.Evidence) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("server: marshal evidence: %w", err)
	}
	c.mu.Lock()
	if st, ok := c.activeFlights[key]; ok {
		st.putCalled = true
		st.putPayload = b
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	entry := cache.Entry{
		Payload:     b,
		ContentType: "application/json",
		CreatedAt:   c.now().UTC(),
	}
	return c.store.Put(ctx, key, entry)
}

// Collect runs p.Collect with single-flight deduplication on pipeline.EvidenceKey.
// The first caller's context bounds the make, and the Keel flight is in-process only.
// A caller that receives cached evidence or joins an existing in-process flight receives
// hit=true; the leader whose maker ran and gathered receives hit=false.
//
// If p.Cache is not this EvidenceCache, an error is returned. A failed make is never
// cached. Corrupted cache entries fall back to direct collection to recover cleanly.
func (c *EvidenceCache) Collect(ctx context.Context, p *pipeline.Pipeline, title, ref string) (pipeline.Evidence, bool, error) {
	if p == nil {
		return pipeline.Evidence{}, false, errors.New("server: pipeline is nil")
	}
	if p.Cache != c {
		return pipeline.Evidence{}, false, fmt.Errorf("%w: pipeline.Cache must be this EvidenceCache", ErrInvalidConfig)
	}

	var storefront string
	if p.Gatherer != nil {
		sf, err := storefrontOf(p.Gatherer.Store)
		if err != nil {
			// Fall back to direct collect so pipeline's own errors surface.
			return p.Collect(ctx, title, ref)
		}
		storefront = sf
	}
	if p.Reader == nil || p.Reader.Model == nil {
		return p.Collect(ctx, title, ref)
	}
	readerModel := p.Reader.Model.Settings().Model

	key := pipeline.EvidenceKey(title, storefront, readerModel)

	myToken := atomic.AddUint64(&c.seq, 1)

	defer func() {
		c.mu.Lock()
		if st := c.activeFlights[key]; st != nil && st.leaderToken == myToken {
			delete(c.activeFlights, key)
		}
		c.mu.Unlock()
	}()

	maker := func(ctx context.Context) (cache.Result, error) {
		st := &flightState{leaderToken: myToken}
		c.mu.Lock()
		c.activeFlights[key] = st
		c.mu.Unlock()

		ev, pHit, err := p.Collect(ctx, title, ref)
		if err != nil {
			return cache.Result{}, err
		}

		c.mu.Lock()
		st.pCollectHit = pHit
		putCalled := st.putCalled
		payload := st.putPayload
		c.mu.Unlock()

		// Check whether p.Collect cached the evidence. If p.Collect deemed the
		// evidence thin (e.g. failed chunks or transient source failures), it did not
		// call Put. Returning uncacheableEvidenceError ensures Keel's GetOrMake does
		// not store it, and any expired row in SQLite remains untouched.
		if !putCalled {
			return cache.Result{}, uncacheableEvidenceError{ev: ev, hit: pHit}
		}

		return cache.Result{
			Payload:     payload,
			ContentType: "application/json",
			ChargeRef:   ref,
		}, nil
	}

	res, err := c.cache.GetOrMake(ctx, key, maker)
	if err != nil {
		var ue uncacheableEvidenceError
		if errors.As(err, &ue) {
			c.mu.Lock()
			st := c.activeFlights[key]
			hit := true
			if st != nil && st.leaderToken == myToken && !ue.hit {
				hit = false
			}
			c.mu.Unlock()
			return ue.ev, hit, nil
		}
		return pipeline.Evidence{}, false, err
	}

	var ev pipeline.Evidence
	if err := json.Unmarshal(res.Payload, &ev); err != nil {
		// Cached payload does not decode; fall back to direct Collect to refresh.
		return p.Collect(ctx, title, ref)
	}

	c.mu.Lock()
	st := c.activeFlights[key]
	hit := true
	if st != nil && st.leaderToken == myToken && !st.pCollectHit {
		hit = false
	}
	c.mu.Unlock()

	return ev, hit, nil
}

// storefrontOf replicates pipeline.storefrontOf to compute the exact EvidenceKey.
// A nil store means "none", *appstore.Client uses Country, and Storefronter is honoured.
func storefrontOf(s pipeline.Store) (string, error) {
	switch s := s.(type) {
	case nil:
		return "none", nil
	case *appstore.Client:
		if s == nil {
			return "none", nil
		}
		return s.Country, nil
	case pipeline.Storefronter:
		return s.Storefront(), nil
	}
	return "", errors.New("server: the store does not say its storefront (implement Storefronter)")
}

// CardCacheConfig configures NewCardCache.
type CardCacheConfig struct {
	Cards  *cards.Store
	MaxAge time.Duration
	Now    func() time.Time
}

// CardCache manages the card-level cache backed by cards.Store.
// Cards are keyed by TitleKey(title), age, and checklist.Version within MaxAge.
// A failed make is never cached; cards are only stored via cards.Put* on success.
type CardCache struct {
	Cards  *cards.Store
	MaxAge time.Duration
	Now    func() time.Time
}

// NewCardCache creates a CardCache. Cards is required. MaxAge defaults to 168h.
func NewCardCache(cfg CardCacheConfig) (*CardCache, error) {
	if cfg.Cards == nil {
		return nil, fmt.Errorf("%w: cards store is required", ErrInvalidConfig)
	}
	if cfg.MaxAge < 0 {
		return nil, fmt.Errorf("%w: MaxAge must not be negative", ErrInvalidConfig)
	}
	maxAge := cfg.MaxAge
	if maxAge == 0 {
		maxAge = DefaultCardMaxAge
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &CardCache{
		Cards:  cfg.Cards,
		MaxAge: maxAge,
		Now:    now,
	}, nil
}

func (c *CardCache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Fresh checks whether a card exists for title, age, and checklist.Version
// within MaxAge of Now. It returns (id, true, nil) on a hit, ("", false, nil)
// on a miss (not found or older than MaxAge), and an error if the store fails.
func (c *CardCache) Fresh(ctx context.Context, title string, age int) (string, bool, error) {
	if c.Cards == nil {
		return "", false, fmt.Errorf("%w: cards store is nil", ErrInvalidConfig)
	}
	maxAge := c.MaxAge
	if maxAge == 0 {
		maxAge = DefaultCardMaxAge
	}
	since := c.now().Add(-maxAge)
	cid, err := c.Cards.Latest(ctx, title, age, checklist.Version, since)
	if errors.Is(err, cards.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return cid, true, nil
}
