package pipeline

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/nrynss/before-yes/internal/appstore"
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

// readerChunking names the reader's chunking and ref scheme. Bump it when
// chunkDocs or assignRefs change what the reader is shown in a way the
// sample in ReaderVersion would not catch.
const readerChunking = "chunk-1"

// ReaderVersion names what the reader is shown and told: a short hash of
// the system prompt, the user prompt as rendered for a sample set of
// documents (so the template, the chunking, the overlap and the ref scheme
// are all in it), the chunk sizes and readerChunking. Editing any of them
// changes the key on its own, so no one has to remember to bump.
func ReaderVersion() string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%d\n%d\n%s\n", readerChunking, chunkChars, overlapChars, readerSystem())
	docs := versionSample()
	for _, c := range chunkDocs(docs) {
		h.Write([]byte(readerUser("Sample Title", c, docs)))
	}
	return "reader-" + hex.EncodeToString(h.Sum(nil))[:12]
}

// versionSample is a fixed set of documents that exercises every path of
// the chunker: two reviews with the same index, a short page, and a page
// long enough to split with overlap.
func versionSample() []Doc {
	long := strings.Repeat("A sample sentence about chat and purchases. ", chunkChars/40)
	return []Doc{
		{Source: finding.Source{Kind: KindReview, Index: 1, Title: "Review one"}, Text: "Review text one."},
		{Source: finding.Source{Kind: KindReview, Index: 1, Title: "Review again"}, Text: "Review text again."},
		{Source: finding.Source{Kind: KindPolicy, URL: "https://example.com/privacy", Title: "Privacy"}, Text: "We collect your email."},
		{Source: finding.Source{Kind: KindNews, URL: "https://example.com/news", Title: "News"}, Text: long},
	}
}

// GatherVersion names what the gatherer asks and returns: PlanVersion
// (hand-bumped for rule changes inside Gather), the plan for every kind
// (PlanFor) as drawn for a sample title with and without a host, and the field names and types of
// Gathered and Doc. A new query, a changed search option or a new output
// field changes the key on its own.
func GatherVersion() string {
	h := sha256.New()
	fmt.Fprintln(h, PlanVersion)
	for _, kind := range allKinds {
		for _, host := range []string{"", "example.com"} {
			b, _ := json.Marshal(PlanFor(kind, "Sample Title", host))
			h.Write(b)
		}
	}
	for _, v := range []any{Gathered{}, Doc{}, finding.Source{}} {
		t := reflect.TypeOf(v)
		for i := 0; i < t.NumField(); i++ {
			fmt.Fprintf(h, "%s.%s %s;", t.Name(), t.Field(i).Name, t.Field(i).Type)
		}
	}
	return PlanVersion + "-" + hex.EncodeToString(h.Sum(nil))[:12]
}

// EvidenceKey is the evidence cache key: the title folded to lower case
// with its whitespace collapsed, the gather version, the reader version, the
// storefront and the reader model. Any of them changing changes what
// Collect would gather or read, so each is part of the key. No key or
// secret is ever part of it.
func EvidenceKey(title, storefront, readerModel string) string {
	t := strings.ToLower(strings.Join(strings.Fields(title), " "))
	return strings.Join([]string{"evidence", GatherVersion(), ReaderVersion(), strings.ToLower(storefront), readerModel, t}, "|")
}

// Storefronter is a store that can say which App Store country it reads.
// A wrapper around *appstore.Client implements it to stay usable.
type Storefronter interface{ Storefront() string }

// storefrontOf reads the storefront from the store itself, the one source
// of truth, so the cache key cannot disagree with what was gathered. No
// store means no App Store evidence: "none". A store that cannot say is an
// error rather than a guess.
func storefrontOf(s Store) (string, error) {
	switch s := s.(type) {
	case nil:
		return "none", nil
	case *appstore.Client:
		if s == nil {
			return "none", nil
		}
		return s.Country, nil
	case Storefronter:
		return s.Storefront(), nil
	}
	return "", errors.New("pipeline: the store does not say its storefront (implement Storefronter)")
}

// Step is one progress report from Collect: the stage, a parent-facing
// message, and counts when the stage counts something (Total 0 means it
// does not).
type Step struct {
	Stage   string
	Message string
	Current int
	Total   int
}

type progressKey struct{}

// WithProgress returns ctx carrying fn, the progress hook Collect, Gather
// and Read report to as they work. fn must be safe for concurrent use: the
// reader's chunks finish on their own goroutines.
func WithProgress(ctx context.Context, fn func(Step)) context.Context {
	return context.WithValue(ctx, progressKey{}, fn)
}

// report sends s to ctx's progress hook, if it has one.
// A panicking hook is recovered: progress is for display, and a reader
// chunk's goroutine must not take the process down with it.
func report(ctx context.Context, s Step) {
	defer func() { _ = recover() }()
	if fn, ok := ctx.Value(progressKey{}).(func(Step)); ok && fn != nil {
		fn(s)
	}
}

// MaxFailedChunks is the most reader chunks that may fail for evidence to
// be cached. Any failed chunk means the findings may be thin for a reason
// that will not last, so the evidence is used for this card but not kept.
const MaxFailedChunks = 0

// Pipeline makes a card: Collect gathers and reads a title's sources once,
// and Decide judges that evidence for one age.
type Pipeline struct {
	Gatherer *Gatherer
	Reader   *Reader
	Judge    *Judge
	// Cache keeps evidence between checks. Nil means no cache.
	Cache EvidenceCache
	// Checks gates fresh alternative checks on the checks_daily cap
	// (T3.5, user decision). Nil means no cap. *spend.Meter has it.
	Checks Checks
	Now    func() time.Time
}

func (p *Pipeline) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
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
// collect is never cached, nor is one where more than MaxFailedChunks
// reader chunks failed, nor one where a source call errored
// (Gathered.Failures: an App Store search or review feed error, a page
// Tavily could not read). A source that is absent (no listing, an empty
// feed, a 404 page) is a fact and is cached. A cache hit costs nothing now: its Stats keep the
// read cost paid when it was collected (see the T3.4 notes for T4).
func (p *Pipeline) Collect(ctx context.Context, title, ref string) (ev Evidence, hit bool, err error) {
	if p.Gatherer == nil || p.Reader == nil || p.Reader.Model == nil {
		return Evidence{}, false, errors.New("pipeline: a gatherer and a reader are required")
	}
	title = strings.Join(strings.Fields(title), " ")
	readerModel := p.Reader.Model.Settings().Model
	storefront, err := storefrontOf(p.Gatherer.Store)
	if err != nil {
		return Evidence{}, false, err
	}
	key := EvidenceKey(title, storefront, readerModel)
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
	report(ctx, Step{Stage: "read", Message: fmt.Sprintf("reading %d sources", len(g.Docs))})
	rd, err := p.Reader.Read(ctx, title, ref, g.Docs)
	if err != nil {
		return Evidence{}, false, err
	}

	ev = Evidence{Title: title, Kind: g.Kind, CollectedAt: p.now().UTC(), Findings: rd.Findings}
	if g.App != nil {
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
		ReviewSource:  g.ReviewSource,
		Chunks:        rd.Chunks,
		FailedChunks:  rd.Failed,
		Drops:         rd.Drops,
		OffProduct:    g.OffProduct,
		MissingKinds:  append([]string{}, g.Missing...),
		TavilyCredits: int(math.Round(g.Credits)),
		ReadCostNano:  int64(rd.Cost),
		ReaderModel:   readerModel,
	}
	if p.Cache != nil && rd.Failed <= MaxFailedChunks && len(g.Failures) == 0 {
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
		KindLabel:        KindLabel(ev.Kind),
		Age:              age,
		ChecklistVersion: checklist.Version,
		CheckedAt:        p.now().UTC(),
		CollectedAt:      ev.CollectedAt,
		Verdict:          j.Verdict,
		Grades:           j.Grades,
		ThinAreas:        j.Thin,
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
