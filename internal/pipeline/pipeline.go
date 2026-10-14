package pipeline

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/finding"
)

// Request is one parent's question: a title and a child's age.
type Request struct {
	Title string `json:"title"`
	Age   int    `json:"age"`
}

// ErrRequest reports a request the pipeline will not run.
var ErrRequest = errors.New("pipeline: invalid request")

// ErrNoSources reports a title for which the gather step found nothing to
// read.
var ErrNoSources = errors.New("pipeline: no sources")

// Age bounds a card is made for.
const (
	MinAge = 3
	MaxAge = 17
)

// Normalize folds the title's whitespace and checks the title and the age.
func (r Request) Normalize() (Request, error) {
	r.Title = strings.Join(strings.Fields(r.Title), " ")
	if r.Title == "" || len([]rune(r.Title)) > 120 {
		return r, fmt.Errorf("%w: a title of 1 to 120 characters is required", ErrRequest)
	}
	if r.Age < MinAge || r.Age > MaxAge {
		return r, fmt.Errorf("%w: age must be between %d and %d", ErrRequest, MinAge, MaxAge)
	}
	return r, nil
}

// EvidenceCache keeps collected evidence by EvidenceKey, so a second age for
// the same title costs one judge call. T4.2 backs it durably.
type EvidenceCache interface {
	Get(ctx context.Context, key string) (Evidence, bool, error)
	Put(ctx context.Context, key string, ev Evidence) error
}

// MemCache is an in-memory EvidenceCache.
type MemCache struct {
	mu sync.Mutex
	m  map[string]Evidence
}

func (c *MemCache) Get(_ context.Context, key string) (Evidence, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ev, ok := c.m[key]
	return ev, ok, nil
}

func (c *MemCache) Put(_ context.Context, key string, ev Evidence) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]Evidence{}
	}
	c.m[key] = ev
	return nil
}

// EvidenceKey is the evidence cache key: the title folded to lower case
// with its whitespace collapsed, the plan version, the storefront and the
// reader model. Any of them changing changes what Collect would gather or
// read, so each is part of the key. No key or secret is ever part of it.
func EvidenceKey(title, storefront, readerModel string) string {
	t := strings.ToLower(strings.Join(strings.Fields(title), " "))
	return strings.Join([]string{"evidence", PlanVersion, strings.ToLower(storefront), readerModel, t}, "|")
}

// Pipeline makes a card: Collect gathers and reads a title's sources once,
// and Decide judges that evidence for one age.
type Pipeline struct {
	Gatherer *Gatherer
	Reader   *Reader
	Judge    *Judge
	// Cache keeps evidence between checks. Nil means no cache.
	Cache EvidenceCache
	// Storefront is the App Store country the gatherer's store reads, for
	// the cache key. Empty means "us".
	Storefront string
	Now        func() time.Time
}

func (p *Pipeline) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Pipeline) storefront() string {
	if p.Storefront == "" {
		return "us"
	}
	return p.Storefront
}

// Run normalizes the request, collects the evidence (from the cache when
// it holds it) and decides the card.
func (p *Pipeline) Run(ctx context.Context, req Request, ref string) (Card, error) {
	req, err := req.Normalize()
	if err != nil {
		return Card{}, err
	}
	ev, _, err := p.Collect(ctx, req.Title, ref)
	if err != nil {
		return Card{}, err
	}
	return p.Decide(ctx, ev, req.Age, ref)
}

// Collect returns the evidence for title. A cached copy is returned as it
// is, with hit true, and costs nothing. Otherwise it gathers the sources and
// reads them, books every call under ref, and caches the result. A failed
// collect is never cached.
func (p *Pipeline) Collect(ctx context.Context, title, ref string) (ev Evidence, hit bool, err error) {
	if p.Gatherer == nil || p.Reader == nil || p.Reader.Model == nil {
		return Evidence{}, false, errors.New("pipeline: a gatherer and a reader are required")
	}
	title = strings.Join(strings.Fields(title), " ")
	readerModel := p.Reader.Model.Settings().Model
	key := EvidenceKey(title, p.storefront(), readerModel)
	if p.Cache != nil {
		if ev, ok, err := p.Cache.Get(ctx, key); err == nil && ok {
			return ev, true, nil
		}
	}

	g, err := p.Gatherer.Gather(ctx, title, ref)
	if err != nil {
		return Evidence{}, false, err
	}
	if len(g.Docs) == 0 {
		return Evidence{}, false, fmt.Errorf("%w: found nothing to read for %q", ErrNoSources, title)
	}
	rd, err := p.Reader.Read(ctx, title, ref, g.Docs)
	if err != nil {
		return Evidence{}, false, err
	}

	ev = Evidence{Title: title, CollectedAt: p.now().UTC(), Findings: rd.Findings}
	if g.App != nil {
		ev.Kind = KindIOS
		ev.App = &AppInfo{
			TrackID:       g.App.ID,
			Name:          g.App.Name,
			Seller:        g.App.Seller,
			SellerURL:     g.App.SellerURL,
			StoreURL:      g.App.StoreURL,
			ContentRating: g.App.AgeRating,
			Description:   g.App.Description,
		}
	}
	reviews := 0
	for _, d := range g.Docs {
		if d.Source.Kind == KindReview {
			reviews++
		}
	}
	ev.Stats = Stats{
		Sources:       len(g.Docs) - reviews,
		Reviews:       reviews,
		Chunks:        rd.Chunks,
		FailedChunks:  rd.Failed,
		Drops:         rd.Drops,
		MissingKinds:  append([]string{}, g.Missing...),
		TavilyCredits: int(math.Round(g.Credits)),
		ReadCostNano:  int64(rd.Cost),
		ReaderModel:   readerModel,
	}
	if p.Cache != nil {
		// A cache that will not store is not a failed check: the card is
		// still good, the next ask just pays again.
		_ = p.Cache.Put(ctx, key, ev)
	}
	return ev, false, nil
}

// Decide judges ev for a child of age and makes the card. Only the judge is
// called, and its calls are booked under ref. The card carries the
// evidence's read cost as collected, and this decision's judge cost.
func (p *Pipeline) Decide(ctx context.Context, ev Evidence, age int, ref string) (Card, error) {
	if _, err := (Request{Title: ev.Title, Age: age}).Normalize(); err != nil {
		return Card{}, err
	}
	if p.Judge == nil || p.Judge.Model == nil {
		return Card{}, errors.New("pipeline: a judge is required")
	}
	j, err := p.Judge.Judge(ctx, ev.Title, age, ref, ev.Findings, ev.Stats.MissingKinds)
	if err != nil {
		return Card{}, err
	}
	id, err := newCardID()
	if err != nil {
		return Card{}, err
	}
	card := Card{
		ID:               id,
		Title:            ev.Title,
		Kind:             ev.Kind,
		Age:              age,
		ChecklistVersion: checklist.Version,
		CheckedAt:        p.now().UTC(),
		CollectedAt:      ev.CollectedAt,
		Verdict:          j.Verdict,
		Grades:           j.Grades,
		Settings:         checklist.Settings(j.Grades),
		Findings:         cited(ev.Findings, j.Grades),
		Alternatives:     []Alternative{},
		App:              ev.App,
		Stats:            ev.Stats,
	}
	card.Stats.MissingKinds = append([]string{}, ev.Stats.MissingKinds...)
	card.Stats.ConflictedAreas = append([]checklist.Area{}, j.Conflicted...)
	card.Stats.JudgeCostNano = int64(j.Cost)
	card.Stats.JudgeModel = p.Judge.Model.Settings().Model
	return card, nil
}

// cited is what the card shows: the findings its grades rest on, plus every
// risk finding, so a parent sees everything the judge weighed against the
// title. The evidence's order is kept.
func cited(fs []finding.Finding, grades []checklist.Grade) []finding.Finding {
	use := map[string]bool{}
	for _, g := range grades {
		for _, id := range g.Findings {
			use[id] = true
		}
	}
	out := []finding.Finding{}
	for _, f := range fs {
		if use[f.ID] || f.Stance == finding.Risk {
			out = append(out, f)
		}
	}
	return out
}

const idAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// newCardID is an unguessable card id: "c_" and 22 base-62 characters,
// about 131 bits from crypto/rand.
func newCardID() (string, error) {
	b := make([]byte, 22)
	out := make([]byte, 0, 24)
	out = append(out, 'c', '_')
	for len(out) < 24 {
		if _, err := rand.Read(b); err != nil {
			return "", fmt.Errorf("pipeline: card id: %w", err)
		}
		for _, c := range b {
			// 248 is the largest multiple of 62 under 256, so the
			// characters stay uniform.
			if c < 248 && len(out) < 24 {
				out = append(out, idAlphabet[c%62])
			}
		}
	}
	return string(out), nil
}
