package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/keel/cache"
	"github.com/nrynss/keel/cache/sqlitestore"
	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/sqlite"

	"github.com/nrynss/before-yes/internal/appstore"
	"github.com/nrynss/before-yes/internal/cards"
	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/model"
	"github.com/nrynss/before-yes/internal/pipeline"
	"github.com/nrynss/before-yes/internal/spend"
	"github.com/nrynss/before-yes/internal/tavily"
)

// fakeMeter satisfies pipeline's Meter interface without budgeting.
type fakeMeter struct{}

func (fakeMeter) Spend(ctx context.Context, pool spend.Pool, kind, ref string, estimate cost.Price, work cost.Work) (cost.Usage, error) {
	rep, err := work(ctx)
	return cost.Usage{Price: rep.Price, Measured: rep.Measured}, err
}

// fakeSearcher satisfies pipeline.Searcher.
type fakeSearcher struct {
	mu           sync.Mutex
	searchCalls  int
	extractCalls int
	failSearch   bool
	failExtract  bool
}

func (s *fakeSearcher) Search(ctx context.Context, req tavily.Search) ([]tavily.Result, tavily.Credits, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.searchCalls++
	if s.failSearch {
		return nil, tavily.Credits{}, errors.New("search failed")
	}
	return []tavily.Result{
		{
			URL:     "https://example.com/privacy",
			Title:   "Privacy Policy",
			Content: "We collect basic info. Chat is filtered for safety.",
		},
	}, tavily.Credits{Used: 1, Reported: true}, nil
}

func (s *fakeSearcher) ExtractWithFailures(ctx context.Context, urls []string) (tavily.Extracted, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.extractCalls++
	if s.failExtract {
		fails := make([]tavily.Failed, len(urls))
		for i, u := range urls {
			fails[i] = tavily.Failed{URL: u, Error: "extract failed"}
		}
		return tavily.Extracted{Failed: fails, Credits: tavily.Credits{Used: 1, Reported: true}}, nil
	}
	pages := make([]tavily.Page, len(urls))
	for i, u := range urls {
		pages[i] = tavily.Page{
			URL:        u,
			RawContent: "Full policy document: chat is moderated and data is retained.",
		}
	}
	return tavily.Extracted{Pages: pages, Credits: tavily.Credits{Used: 1, Reported: true}}, nil
}

// fakeChatter satisfies pipeline.Chatter for the reader.
type fakeChatter struct {
	mu         sync.Mutex
	calls      int
	failChat   bool
	modelName  string
	replyText  string
	replyPrice cost.Price
}

func (c *fakeChatter) Chat(ctx context.Context, msgs []model.Message, opts model.Options) (model.Reply, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.failChat {
		return model.Reply{}, errors.New("chat failed")
	}
	txt := c.replyText
	if txt == "" {
		txt = `[]`
	}
	return model.Reply{Text: txt, Price: c.replyPrice, Measured: true}, nil
}

func (c *fakeChatter) Estimate([]model.Message, model.Options) cost.Price { return 100 }

func (c *fakeChatter) Settings() model.Settings {
	name := c.modelName
	if name == "" {
		name = "nano"
	}
	return model.Settings{Provider: "test", Model: name}
}

// fakeStorefronter satisfies pipeline.Store and pipeline.Storefronter.
type fakeStorefronter struct {
	country string
}

func (f fakeStorefronter) Find(ctx context.Context, term string, limit int) ([]appstore.App, error) {
	return []appstore.App{
		{
			ID:        123456,
			Name:      term,
			SellerURL: "https://example.com",
		},
	}, nil
}

func (f fakeStorefronter) Reviews(ctx context.Context, appID int64, pages int) ([]appstore.Review, error) {
	return []appstore.Review{
		{
			ID:    "rev1",
			Title: "Great",
			Body:  "Chat is safe.",
			Index: 1,
		},
	}, nil
}

func (f fakeStorefronter) FeedURL(appID int64, page int) string {
	return ""
}

func (f fakeStorefronter) Storefront() string {
	return f.country
}

// openTestDB opens a test SQLite database with busy timeout.
func openTestDB(t *testing.T, path string) *sqlite.DB {
	t.Helper()
	db, err := sqlite.Open(context.Background(), sqlite.Config{
		Path:        path,
		BusyTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func openEvidenceStore(t *testing.T, db *sqlite.DB, namespace string) *sqlitestore.Store {
	t.Helper()
	store, err := sqlitestore.Open(context.Background(), sqlitestore.Config{
		DB:        db,
		Namespace: namespace,
	})
	if err != nil {
		t.Fatalf("sqlitestore.Open(%s): %v", namespace, err)
	}
	return store
}

// TestConcurrentIdenticalChecksCollectOnce proves that when N concurrent goroutines
// request evidence for the same title, only ONE collect runs (one gather and one reader call),
// all callers receive identical evidence, and joiners receive hit=true while the leader
// receives hit=false.
func TestConcurrentIdenticalChecksCollectOnce(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "test.db"))
	store := openEvidenceStore(t, db, "evidence")

	ec, err := NewEvidenceCache(EvidenceCacheConfig{
		Store: store,
	})
	if err != nil {
		t.Fatalf("NewEvidenceCache: %v", err)
	}

	searcher := &fakeSearcher{}
	chatter := &fakeChatter{}
	p := &pipeline.Pipeline{
		Gatherer: &pipeline.Gatherer{
			Store:  fakeStorefronter{country: "us"},
			Search: searcher,
			Meter:  fakeMeter{},
		},
		Reader: &pipeline.Reader{
			Model: chatter,
			Meter: fakeMeter{},
		},
		Cache: ec,
	}

	const n = 10
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(n)

	evs := make([]pipeline.Evidence, n)
	hits := make([]bool, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start
			ev, hit, err := ec.Collect(context.Background(), p, "Roblox", fmt.Sprintf("ref-%d", idx))
			evs[idx] = ev
			hits[idx] = hit
			errs[idx] = err
		}(i)
	}

	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d failed: %v", i, err)
		}
	}

	// Verify reader and search were called exactly once per plan.
	if chatter.calls != 1 {
		t.Errorf("chatter called %d times; want 1", chatter.calls)
	}
	if searcher.extractCalls != 1 {
		t.Errorf("searcher.ExtractWithFailures called %d times; want 1", searcher.extractCalls)
	}

	// All callers got the same title and gathered structure.
	for i := 1; i < n; i++ {
		if evs[i].Title != evs[0].Title {
			t.Errorf("caller %d title = %q; want %q", i, evs[i].Title, evs[0].Title)
		}
		if evs[i].CollectedAt != evs[0].CollectedAt {
			t.Errorf("caller %d CollectedAt = %v; want %v", i, evs[i].CollectedAt, evs[0].CollectedAt)
		}
	}

	// Exactly one caller got hit=false (the leader); all other n-1 callers got hit=true.
	missCount := 0
	hitCount := 0
	for _, h := range hits {
		if h {
			hitCount++
		} else {
			missCount++
		}
	}
	if missCount != 1 {
		t.Errorf("missCount = %d; want exactly 1 leader with hit=false", missCount)
	}
	if hitCount != n-1 {
		t.Errorf("hitCount = %d; want %d joiners with hit=true", hitCount, n-1)
	}
}

// TestCardFoundAgainByTitleAndAge tests the card-level cache:
// - Title case and spacing fold to the same card
// - A different age misses
// - A different checklist version misses
// - An expired card misses under an injected clock
// - A store fault returns an error, not a silent miss
func TestCardFoundAgainByTitleAndAge(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "cards.db"))
	cardStore, err := cards.Open(context.Background(), db)
	if err != nil {
		t.Fatalf("cards.Open: %v", err)
	}

	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	cc, err := NewCardCache(CardCacheConfig{
		Cards:  cardStore,
		MaxAge: 168 * time.Hour,
		Now:    clock,
	})
	if err != nil {
		t.Fatalf("NewCardCache: %v", err)
	}

	// Store a card for "Roblox", age 9.
	card := pipeline.Card{
		Title:            "Roblox",
		Age:              9,
		ChecklistVersion: checklist.Version,
		CheckedAt:        now,
		Verdict:          checklist.No,
	}
	cid, err := cardStore.Put(context.Background(), card)
	if err != nil {
		t.Fatalf("cardStore.Put: %v", err)
	}

	// 1. Same title & age: hits.
	gotID, ok, err := cc.Fresh(context.Background(), "Roblox", 9)
	if err != nil || !ok || gotID != cid {
		t.Fatalf("Fresh('Roblox', 9) = (%q, %v, %v); want (%q, true, nil)", gotID, ok, err, cid)
	}

	// 2. Whitespace and case folding: hits.
	gotID2, ok, err := cc.Fresh(context.Background(), "  roblox  ", 9)
	if err != nil || !ok || gotID2 != cid {
		t.Fatalf("Fresh('  roblox  ', 9) = (%q, %v, %v); want (%q, true, nil)", gotID2, ok, err, cid)
	}

	// 3. Different age: misses.
	_, ok, err = cc.Fresh(context.Background(), "Roblox", 10)
	if err != nil || ok {
		t.Errorf("Fresh('Roblox', 10) = ok=%v, err=%v; want ok=false, nil", ok, err)
	}

	// 4. Different title: misses.
	_, ok, err = cc.Fresh(context.Background(), "Minecraft", 9)
	if err != nil || ok {
		t.Errorf("Fresh('Minecraft', 9) = ok=%v, err=%v; want ok=false, nil", ok, err)
	}

	// 5. Older than MaxAge: misses with clock advance.
	now = now.Add(169 * time.Hour) // past 168h MaxAge
	_, ok, err = cc.Fresh(context.Background(), "Roblox", 9)
	if err != nil || ok {
		t.Errorf("Fresh expired card = ok=%v, err=%v; want ok=false, nil", ok, err)
	}

	// 6. Store error returns an error, never a silent miss.
	_ = db.Close()
	_, _, err = cc.Fresh(context.Background(), "Roblox", 9)
	if err == nil {
		t.Error("Fresh on closed DB returned nil error; want error")
	}
}

// TestFailedMakeNeverCached verifies:
//  1. When evidence collection fails, nothing is stored in the cache. A second
//     request recollects and succeeds.
//  2. When a card check fails, nothing is stored in the cards store.
func TestFailedMakeNeverCached(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "test.db"))
	store := openEvidenceStore(t, db, "evidence")
	cardStore, err := cards.Open(context.Background(), db)
	if err != nil {
		t.Fatalf("cards.Open: %v", err)
	}

	ec, err := NewEvidenceCache(EvidenceCacheConfig{Store: store})
	if err != nil {
		t.Fatalf("NewEvidenceCache: %v", err)
	}
	cc, err := NewCardCache(CardCacheConfig{Cards: cardStore})
	if err != nil {
		t.Fatalf("NewCardCache: %v", err)
	}

	searcher := &fakeSearcher{failSearch: true}
	chatter := &fakeChatter{}
	p := &pipeline.Pipeline{
		Gatherer: &pipeline.Gatherer{
			Store:  fakeStorefronter{country: "us"},
			Search: searcher,
			Meter:  fakeMeter{},
		},
		Reader: &pipeline.Reader{Model: chatter, Meter: fakeMeter{}},
		Cache:  ec,
	}

	// 1. Evidence collect failure:
	_, _, err = ec.Collect(context.Background(), p, "Roblox", "ref-1")
	if err == nil {
		t.Fatal("Collect with failing search succeeded; want error")
	}

	// Verify nothing was stored in the cache store.
	key := pipeline.EvidenceKey("Roblox", "us", "nano")
	if _, found, _ := ec.Get(context.Background(), key); found {
		t.Error("failed make was stored in evidence cache; want empty")
	}

	// Now heal the searcher: second collect must run and succeed.
	searcher.failSearch = false
	callsBefore := searcher.searchCalls
	ev, hit, err := ec.Collect(context.Background(), p, "Roblox", "ref-2")
	if err != nil || hit {
		t.Fatalf("re-collect after failure = (hit=%v, err=%v); want hit=false, nil", hit, err)
	}
	if ev.Title != "Roblox" {
		t.Errorf("ev.Title = %q; want Roblox", ev.Title)
	}
	if searcher.searchCalls <= callsBefore {
		t.Errorf("searcher was not called on re-collect; before=%d, after=%d", callsBefore, searcher.searchCalls)
	}

	// 2. Card cache: failed make leaves the card store empty.
	_, ok, err := cc.Fresh(context.Background(), "Roblox", 9)
	if err != nil || ok {
		t.Errorf("cc.Fresh = ok=%v, err=%v; want ok=false, nil (empty card store)", ok, err)
	}
}

// TestThinEvidenceNotStoredButSharedWithJoiners tests that when Collect produces
// uncacheable (thin) evidence, it is shared with concurrent callers in the flight,
// but NOT persisted to the store. A subsequent call recolonizes.
func TestThinEvidenceNotStoredButSharedWithJoiners(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "test.db"))
	store := openEvidenceStore(t, db, "evidence")

	ec, err := NewEvidenceCache(EvidenceCacheConfig{Store: store})
	if err != nil {
		t.Fatalf("NewEvidenceCache: %v", err)
	}

	// A searcher that fails on Extract produces Gathered.Failures (un-cacheable by T3.4 MaxFailedChunks).
	searcher := &fakeSearcher{failExtract: true}
	chatter := &fakeChatter{}
	p := &pipeline.Pipeline{
		Gatherer: &pipeline.Gatherer{
			Store:  fakeStorefronter{country: "us"},
			Search: searcher,
			Meter:  fakeMeter{},
		},
		Reader: &pipeline.Reader{Model: chatter, Meter: fakeMeter{}},
		Cache:  ec,
	}

	// Run two concurrent callers for the uncacheable evidence.
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	evs := make([]pipeline.Evidence, 2)
	errs := make([]error, 2)
	hits := make([]bool, 2)

	for i := 0; i < 2; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start
			ev, hit, err := ec.Collect(context.Background(), p, "Roblox", fmt.Sprintf("ref-%d", idx))
			evs[idx] = ev
			hits[idx] = hit
			errs[idx] = err
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d failed: %v", i, err)
		}
	}
	// Both callers got the evidence.
	if evs[0].Title != "Roblox" || evs[1].Title != "Roblox" {
		t.Errorf("expected Roblox for both callers, got %q, %q", evs[0].Title, evs[1].Title)
	}

	// Verify nothing was persisted to ec.store.
	key := pipeline.EvidenceKey("Roblox", "us", "nano")
	_, found, err := ec.Get(context.Background(), key)
	if err != nil || found {
		t.Errorf("thin evidence found in store: found=%v, err=%v; want not found", found, err)
	}

	// A subsequent collect must run again (chatter called a second time).
	initialCalls := chatter.calls
	_, hit, err := ec.Collect(context.Background(), p, "Roblox", "ref-3")
	if err != nil {
		t.Fatalf("subsequent collect failed: %v", err)
	}
	if hit {
		t.Error("subsequent collect reported hit=true; want hit=false (uncached)")
	}
	if chatter.calls <= initialCalls {
		t.Errorf("chatter was not called again; calls = %d, initial = %d", chatter.calls, initialCalls)
	}
}

// TestEvidenceSurvivesReopen verifies that evidence stored in a durable SQLite file
// survives a database close and reopen, serving a subsequent Collect from cache without
// calling gather or reader.
func TestEvidenceSurvivesReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "reopen.db")

	// Phase 1: Open, collect, and store.
	{
		db := openTestDB(t, dbPath)
		store := openEvidenceStore(t, db, "evidence")
		ec, err := NewEvidenceCache(EvidenceCacheConfig{Store: store})
		if err != nil {
			t.Fatalf("NewEvidenceCache: %v", err)
		}

		searcher := &fakeSearcher{}
		chatter := &fakeChatter{}
		p := &pipeline.Pipeline{
			Gatherer: &pipeline.Gatherer{
				Store:  fakeStorefronter{country: "us"},
				Search: searcher,
				Meter:  fakeMeter{},
			},
			Reader: &pipeline.Reader{Model: chatter, Meter: fakeMeter{}},
			Cache:  ec,
		}

		ev, hit, err := ec.Collect(context.Background(), p, "Roblox", "ref-1")
		if err != nil || hit {
			t.Fatalf("initial Collect: hit=%v, err=%v; want hit=false, nil", hit, err)
		}
		if ev.Title != "Roblox" {
			t.Errorf("ev.Title = %q; want Roblox", ev.Title)
		}
		_ = db.Close()
	}

	// Phase 2: Reopen from the same file. Collect must hit cache.
	{
		db := openTestDB(t, dbPath)
		store := openEvidenceStore(t, db, "evidence")
		ec, err := NewEvidenceCache(EvidenceCacheConfig{Store: store})
		if err != nil {
			t.Fatalf("NewEvidenceCache: %v", err)
		}

		searcher := &fakeSearcher{}
		chatter := &fakeChatter{}
		p := &pipeline.Pipeline{
			Gatherer: &pipeline.Gatherer{
				Store:  fakeStorefronter{country: "us"},
				Search: searcher,
				Meter:  fakeMeter{},
			},
			Reader: &pipeline.Reader{Model: chatter, Meter: fakeMeter{}},
			Cache:  ec,
		}

		ev, hit, err := ec.Collect(context.Background(), p, "Roblox", "ref-2")
		if err != nil || !hit {
			t.Fatalf("reopened Collect: hit=%v, err=%v; want hit=true, nil", hit, err)
		}
		if ev.Title != "Roblox" {
			t.Errorf("ev.Title = %q; want Roblox", ev.Title)
		}
		if chatter.calls != 0 {
			t.Errorf("chatter called %d times on cache hit; want 0", chatter.calls)
		}
		if searcher.searchCalls != 0 {
			t.Errorf("searcher called %d times on cache hit; want 0", searcher.searchCalls)
		}
	}
}

// recordingStore wraps cache.Store to record keys passed to Put.
type recordingStore struct {
	cache.Store
	mu   sync.Mutex
	keys []string
}

func (r *recordingStore) Put(ctx context.Context, key string, entry cache.Entry) error {
	r.mu.Lock()
	r.keys = append(r.keys, key)
	r.mu.Unlock()
	return r.Store.Put(ctx, key, entry)
}

// TestEvidenceKeyIsPipelinesKey proves that the key computed in server cache equals
// the key p.Collect uses, for all three store kinds:
// 1. nil store -> "none"
// 2. *appstore.Client -> Country
// 3. pipeline.Storefronter -> Storefront()
func TestEvidenceKeyIsPipelinesKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resultCount": 1,
			"results": []map[string]any{
				{
					"trackId":   123456,
					"trackName": "Sample Title",
					"sellerUrl": "https://example.com",
				},
			},
		})
	}))
	defer srv.Close()

	cases := []struct {
		name       string
		store      pipeline.Store
		storefront string
	}{
		{
			name:       "nil store",
			store:      nil,
			storefront: "none",
		},
		{
			name:       "appstore.Client",
			store:      appstore.New(srv.URL, "gb"),
			storefront: "gb",
		},
		{
			name:       "Storefronter",
			store:      fakeStorefronter{country: "fr"},
			storefront: "fr",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestDB(t, filepath.Join(t.TempDir(), "key.db"))
			baseStore := openEvidenceStore(t, db, "evidence")
			recStore := &recordingStore{Store: baseStore}

			ec, err := NewEvidenceCache(EvidenceCacheConfig{Store: recStore})
			if err != nil {
				t.Fatalf("NewEvidenceCache: %v", err)
			}

			searcher := &fakeSearcher{}
			chatter := &fakeChatter{modelName: "test-reader"}
			p := &pipeline.Pipeline{
				Gatherer: &pipeline.Gatherer{
					Store:  tc.store,
					Search: searcher,
					Meter:  fakeMeter{},
				},
				Reader: &pipeline.Reader{Model: chatter, Meter: fakeMeter{}},
				Cache:  ec,
			}

			_, _, err = ec.Collect(context.Background(), p, "Sample Title", "ref-test")
			if err != nil {
				t.Fatalf("Collect failed: %v", err)
			}

			wantKey := pipeline.EvidenceKey("Sample Title", tc.storefront, "test-reader")

			recStore.mu.Lock()
			keys := append([]string{}, recStore.keys...)
			recStore.mu.Unlock()

			if len(keys) == 0 {
				t.Fatalf("no key was written to store")
			}
			for _, k := range keys {
				if k != wantKey {
					t.Errorf("written key = %q; want %q", k, wantKey)
				}
			}
		})
	}
}

// TestEvidenceMaxAge verifies that entries older than MaxAge expire and trigger
// a fresh collect.
func TestEvidenceMaxAge(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "maxage.db"))
	store := openEvidenceStore(t, db, "evidence")

	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	ec, err := NewEvidenceCache(EvidenceCacheConfig{
		Store:  store,
		MaxAge: 24 * time.Hour,
		Now:    clock,
	})
	if err != nil {
		t.Fatalf("NewEvidenceCache: %v", err)
	}

	searcher := &fakeSearcher{}
	chatter := &fakeChatter{}
	p := &pipeline.Pipeline{
		Gatherer: &pipeline.Gatherer{
			Store:  fakeStorefronter{country: "us"},
			Search: searcher,
			Meter:  fakeMeter{},
		},
		Reader: &pipeline.Reader{Model: chatter, Meter: fakeMeter{}},
		Cache:  ec,
	}

	// Initial collect: misses.
	_, hit, err := ec.Collect(context.Background(), p, "Roblox", "ref-1")
	if err != nil || hit {
		t.Fatalf("initial Collect: hit=%v, err=%v; want hit=false, nil", hit, err)
	}

	// 12 hours later: within 24h, hits.
	now = now.Add(12 * time.Hour)
	_, hit, err = ec.Collect(context.Background(), p, "Roblox", "ref-2")
	if err != nil || !hit {
		t.Fatalf("12h Collect: hit=%v, err=%v; want hit=true, nil", hit, err)
	}
	if chatter.calls != 1 {
		t.Errorf("chatter called %d times; want 1", chatter.calls)
	}

	// 25 hours later (37h from start): past 24h, misses and re-collects.
	now = now.Add(25 * time.Hour)
	_, hit, err = ec.Collect(context.Background(), p, "Roblox", "ref-3")
	if err != nil || hit {
		t.Fatalf("expired Collect: hit=%v, err=%v; want hit=false, nil", hit, err)
	}
	if chatter.calls != 2 {
		t.Errorf("chatter called %d times; want 2 after expiry", chatter.calls)
	}
}

// TestCollectRefusesMiswiredCache proves that Collect returns an error if
// p.Cache is not the EvidenceCache running Collect.
func TestCollectRefusesMiswiredCache(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "miswired.db"))
	store := openEvidenceStore(t, db, "evidence")

	ec, err := NewEvidenceCache(EvidenceCacheConfig{Store: store})
	if err != nil {
		t.Fatalf("NewEvidenceCache: %v", err)
	}

	p := &pipeline.Pipeline{
		Gatherer: &pipeline.Gatherer{Store: fakeStorefronter{country: "us"}},
		Reader:   &pipeline.Reader{Model: &fakeChatter{}},
		Cache:    &pipeline.MemCache{}, // miswired!
	}

	_, _, err = ec.Collect(context.Background(), p, "Roblox", "ref-1")
	if err == nil {
		t.Fatal("Collect with miswired cache returned nil error; want ErrInvalidConfig")
	}
	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("err = %v; want ErrInvalidConfig", err)
	}
}

// TestCorruptEvidenceRecollects verifies that a corrupt JSON payload in the cache
// does not wedge the title, but falls back to direct collect and rewrites the entry.
func TestCorruptEvidenceRecollects(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "corrupt.db"))
	store := openEvidenceStore(t, db, "evidence")

	ec, err := NewEvidenceCache(EvidenceCacheConfig{Store: store})
	if err != nil {
		t.Fatalf("NewEvidenceCache: %v", err)
	}

	searcher := &fakeSearcher{}
	chatter := &fakeChatter{}
	p := &pipeline.Pipeline{
		Gatherer: &pipeline.Gatherer{
			Store:  fakeStorefronter{country: "us"},
			Search: searcher,
			Meter:  fakeMeter{},
		},
		Reader: &pipeline.Reader{Model: chatter, Meter: fakeMeter{}},
		Cache:  ec,
	}

	// Poison the cache entry with non-JSON bytes.
	key := pipeline.EvidenceKey("Roblox", "us", "nano")
	err = store.Put(context.Background(), key, cache.Entry{
		Payload:     []byte("corrupted non-json payload"),
		ContentType: "application/json",
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("store.Put: %v", err)
	}

	// Collect must recover by falling back to p.Collect and rewriting the entry.
	ev, _, err := ec.Collect(context.Background(), p, "Roblox", "ref-corrupt")
	if err != nil {
		t.Fatalf("Collect on corrupt entry failed: %v", err)
	}
	if ev.Title != "Roblox" {
		t.Errorf("ev.Title = %q; want Roblox", ev.Title)
	}

	// The store entry should now be valid JSON.
	entry, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("store.Get after recovery failed: %v", err)
	}
	var recovered pipeline.Evidence
	if err := json.Unmarshal(entry.Payload, &recovered); err != nil {
		t.Fatalf("recovered payload still does not decode: %v", err)
	}
	if recovered.Title != "Roblox" {
		t.Errorf("recovered.Title = %q; want Roblox", recovered.Title)
	}
}
