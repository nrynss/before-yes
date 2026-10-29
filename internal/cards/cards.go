// Package cards stores finished cards in the application's SQLite file, each
// keyed on an unguessable Keel id.
//
// A card's public URL is /api/cards/{id}, and the id is "c_" followed by a
// Keel id: 32 lowercase hex characters, 128 bits from crypto/rand, no
// timestamp and no counter. The id the pipeline gave the card is replaced
// when the card is stored, and the stored body carries the public id in its id
// field, so the body served at /api/cards/{id} names the URL it was served from.
//
// The table is indexed for the lookups the server makes: by title key, age and
// checklist version (Latest, the card cache), by verdict and time, and by
// time alone (Recent). The schema is applied through Keel sqlite.Migrate in
// its own namespace, so it shares the file with the Keel cost, cache and job
// stores without a collision.
package cards

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/sqlite"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/pipeline"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Namespace is the Keel migration namespace this package owns.
const Namespace = "beforeyes_cards"

// MaxRecent is the most summaries Recent returns.
const MaxRecent = 100

var (
	// ErrNotFound reports an id with no card, and an id that is not a public
	// card id (Get refuses that before it queries).
	ErrNotFound = errors.New("cards: not found")
	// ErrInvalid reports a card or an argument the store will not take.
	ErrInvalid = errors.New("cards: invalid")
)

// Store keeps cards in the application's database. Create it with Open.
type Store struct {
	w *sql.DB
	r *sql.DB
}

// Open applies the schema in the beforeyes_cards namespace and returns the
// store. The caller owns db and closes it.
func Open(ctx context.Context, db *sqlite.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("cards: open: %w: nil db", ErrInvalid)
	}
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("cards: open: %w", err)
	}
	if err := sqlite.Migrate(ctx, db, Namespace, sub); err != nil {
		return nil, fmt.Errorf("cards: open: %w", err)
	}
	return &Store{w: db.Writer(), r: db.Reader()}, nil
}

// TitleKey folds a title so "Roblox" and "  roblox " share cards. It folds
// exactly the way pipeline.EvidenceKey folds a title: lower case, whitespace
// collapsed to single spaces.
func TitleKey(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(title), " "))
}

// execer is what a statement runs on: the writer pool or a transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// check refuses a card the store could never look up again.
func check(card pipeline.Card) error {
	if card.CheckedAt.IsZero() {
		return fmt.Errorf("%w: the card has no CheckedAt", ErrInvalid)
	}
	if TitleKey(card.Title) == "" {
		return fmt.Errorf("%w: the card has no title", ErrInvalid)
	}
	return nil
}

// insert stores card under the 32-hex Keel id rawID with pubID ("c_" + rawID)
// as the card's own id.
func insert(ctx context.Context, ex execer, rawID, pubID string, card pipeline.Card) error {
	card.ID = pubID
	body, err := json.Marshal(card)
	if err != nil {
		return fmt.Errorf("cards: put: %w", err)
	}
	_, err = ex.ExecContext(ctx,
		`INSERT INTO cards (id, title, title_key, age, checklist, verdict, created_at, body) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rawID, card.Title, TitleKey(card.Title), card.Age, card.ChecklistVersion, string(card.Verdict), card.CheckedAt.UnixMilli(), body)
	if err != nil {
		return fmt.Errorf("cards: put: %w", err)
	}
	return nil
}

// Put stores card under a fresh Keel id and returns its public id ("c_" + 32-hex).
// The stored card's id field is that public id, so Get(id) returns a card whose ID
// is id. The caller's card is not changed. A card with a zero CheckedAt, or with
// no title, is refused with ErrInvalid.
func (s *Store) Put(ctx context.Context, card pipeline.Card) (string, error) {
	if err := check(card); err != nil {
		return "", err
	}
	raw, err := id.New()
	if err != nil {
		return "", fmt.Errorf("cards: put: %w", err)
	}
	pub := "c_" + raw
	if err := insert(ctx, s.w, raw, pub, card); err != nil {
		return "", err
	}
	return pub, nil
}

// SeedOutcome says what Seed did with a card.
type SeedOutcome string

const (
	// SeedInserted: the store had no such card, so a new row was added.
	SeedInserted SeedOutcome = "inserted"
	// SeedKept: the store already held the same card; nothing changed.
	SeedKept SeedOutcome = "kept"
	// SeedReplaced: the store held the card's identity (title, age, checklist
	// version and checked time) with another body. That row's title, verdict
	// and body were replaced in place, so its public id and URL stay the same.
	SeedReplaced SeedOutcome = "replaced"
)

// Seed stores card so that the store holds it once, and returns its public id
// and what it did. The same card is one with the same title, age, checklist
// version and checked time whose body is equal once both ids are cleared, so
// running Seed twice with one card stores it once and returns the same id both
// times. A card that differs only in its id is the same card. A stored card
// with the same title, age, checklist version and checked time but another
// body (for example, a demo card recomputed after a model change) is replaced
// in place, not duplicated. A card with a different checked time is a new
// row. A card with a zero CheckedAt, or with no title, is refused with
// ErrInvalid.
func (s *Store) Seed(ctx context.Context, card pipeline.Card) (pubID string, out SeedOutcome, err error) {
	if err := check(card); err != nil {
		return "", "", err
	}
	want, err := sameBody(card)
	if err != nil {
		return "", "", fmt.Errorf("cards: seed: %w", err)
	}
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return "", "", fmt.Errorf("cards: seed: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // a no-op once committed
	rows, err := tx.QueryContext(ctx,
		`SELECT id, body FROM cards WHERE title_key = ? AND age = ? AND checklist = ? AND created_at = ? ORDER BY seq`,
		TitleKey(card.Title), card.Age, card.ChecklistVersion, card.CheckedAt.UnixMilli())
	if err != nil {
		return "", "", fmt.Errorf("cards: seed: %w", err)
	}
	replace := "" // the oldest row with this identity and another body
	for rows.Next() {
		var raw string
		var body []byte
		if err := rows.Scan(&raw, &body); err != nil {
			_ = rows.Close()
			return "", "", fmt.Errorf("cards: seed: %w", err)
		}
		var stored pipeline.Card
		if err := json.Unmarshal(body, &stored); err != nil {
			_ = rows.Close()
			return "", "", fmt.Errorf("cards: seed: the stored body does not decode: %w", err)
		}
		got, err := sameBody(stored)
		if err != nil {
			_ = rows.Close()
			return "", "", fmt.Errorf("cards: seed: %w", err)
		}
		if bytes.Equal(got, want) {
			_ = rows.Close()
			return "c_" + raw, SeedKept, nil
		}
		if replace == "" {
			replace = raw
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return "", "", fmt.Errorf("cards: seed: %w", err)
	}
	_ = rows.Close()
	if replace != "" {
		pub := "c_" + replace
		if err := update(ctx, tx, replace, pub, card); err != nil {
			return "", "", err
		}
		if err := tx.Commit(); err != nil {
			return "", "", fmt.Errorf("cards: seed: %w", err)
		}
		return pub, SeedReplaced, nil
	}
	raw, err := id.New()
	if err != nil {
		return "", "", fmt.Errorf("cards: seed: %w", err)
	}
	pub := "c_" + raw
	if err := insert(ctx, tx, raw, pub, card); err != nil {
		return "", "", err
	}
	if err := tx.Commit(); err != nil {
		return "", "", fmt.Errorf("cards: seed: %w", err)
	}
	return pub, SeedInserted, nil
}

// update replaces the title, verdict and body of the row rawID, keeping its id.
// The body's own id is pubID, as insert sets it.
func update(ctx context.Context, ex execer, rawID, pubID string, card pipeline.Card) error {
	card.ID = pubID
	body, err := json.Marshal(card)
	if err != nil {
		return fmt.Errorf("cards: seed: %w", err)
	}
	_, err = ex.ExecContext(ctx,
		`UPDATE cards SET title = ?, verdict = ?, body = ? WHERE id = ?`,
		card.Title, string(card.Verdict), body, rawID)
	if err != nil {
		return fmt.Errorf("cards: seed: %w", err)
	}
	return nil
}

// sameBody is the card's stored encoding with its id cleared, so two cards
// that differ only in id compare equal.
func sameBody(card pipeline.Card) ([]byte, error) {
	card.ID = ""
	return json.Marshal(card)
}

// PutWithAlternatives stores a card and the cards of its checked
// alternatives in one transaction, so the main card never links to a card
// that was not stored. alts are pipeline.AltResult.Cards. A checked
// alternative on a card names its card by the pipeline's own id (the "c_"
// id on the alt card); each alt is stored under a fresh Keel id and the link
// is rewritten to its public id ("c_" + 32-hex). A checked alternative whose
// card is not among alts is dropped from the stored card, never left dangling,
// and so is an alternative that was not checked (a pending or refused one is
// never shown anyway). The same holds for the links on an alt card itself.
//
// altIDs are the public ids ("c_" + 32-hex) of alts, in order. The caller's
// cards are not changed. If any insert fails nothing is stored.
func (s *Store) PutWithAlternatives(ctx context.Context, main pipeline.Card, alts []pipeline.Card) (mainID string, altIDs []string, err error) {
	if err := check(main); err != nil {
		return "", nil, err
	}
	// Every id is minted first, so a link between two alts resolves too.
	byOld := map[string]string{}
	rawAlts := make([]string, len(alts))
	altIDs = make([]string, len(alts))
	for i, a := range alts {
		if err := check(a); err != nil {
			return "", nil, fmt.Errorf("alternative %d: %w", i, err)
		}
		if rawAlts[i], err = id.New(); err != nil {
			return "", nil, fmt.Errorf("cards: put: %w", err)
		}
		altIDs[i] = "c_" + rawAlts[i]
		if a.ID == "" {
			continue
		}
		if _, dup := byOld[a.ID]; dup {
			return "", nil, fmt.Errorf("%w: two alternative cards share the id %q", ErrInvalid, a.ID)
		}
		byOld[a.ID] = altIDs[i]
	}
	rawMain, err := id.New()
	if err != nil {
		return "", nil, fmt.Errorf("cards: put: %w", err)
	}
	mainID = "c_" + rawMain

	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, fmt.Errorf("cards: put: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // a no-op once committed
	for i, a := range alts {
		if err := insert(ctx, tx, rawAlts[i], altIDs[i], relink(a, byOld)); err != nil {
			return "", nil, err
		}
	}
	if err := insert(ctx, tx, rawMain, mainID, relink(main, byOld)); err != nil {
		return "", nil, err
	}
	if err := tx.Commit(); err != nil {
		return "", nil, fmt.Errorf("cards: put: %w", err)
	}
	return mainID, altIDs, nil
}

// relink returns card with its checked alternatives pointing at the new ids
// in byOld. An alternative with no entry there is dropped. The card is a
// copy and its original slice is not written to.
func relink(card pipeline.Card, byOld map[string]string) pipeline.Card {
	kept := make(pipeline.Alternatives, 0, len(card.Alternatives))
	for _, a := range card.Alternatives {
		if a.Status != pipeline.AltChecked {
			continue
		}
		nid, ok := byOld[a.CardID]
		if !ok {
			continue
		}
		a.CardID = nid
		kept = append(kept, a)
	}
	card.Alternatives = kept
	return card
}

// Get returns the card stored under pubID ("c_" + 32-hex). An id that does not
// start with "c_" or whose remainder is not a 32-hex Keel id is refused with
// ErrNotFound before any query runs. An unknown id is ErrNotFound. A stored
// body that does not decode is an error, not a miss.
func (s *Store) Get(ctx context.Context, pubID string) (pipeline.Card, error) {
	raw, ok := strings.CutPrefix(pubID, "c_")
	if !ok || !id.Valid(raw) {
		return pipeline.Card{}, ErrNotFound
	}
	var body []byte
	err := s.r.QueryRowContext(ctx, `SELECT body FROM cards WHERE id = ?`, raw).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return pipeline.Card{}, ErrNotFound
	}
	if err != nil {
		return pipeline.Card{}, fmt.Errorf("cards: get %s: %w", pubID, err)
	}
	var c pipeline.Card
	if err := json.Unmarshal(body, &c); err != nil {
		return pipeline.Card{}, fmt.Errorf("cards: get %s: the stored body does not decode: %w", pubID, err)
	}
	return c, nil
}

// Latest returns the public id ("c_" + 32-hex) of the newest card for the title
// (folded by TitleKey), the age and the checklist version whose checked time is
// at or after since. Two cards at the same time resolve to the one stored last.
// ErrNotFound means there is none.
func (s *Store) Latest(ctx context.Context, title string, age int, checklistVersion string, since time.Time) (string, error) {
	var raw string
	err := s.r.QueryRowContext(ctx,
		`SELECT id FROM cards WHERE title_key = ? AND age = ? AND checklist = ? AND created_at >= ?
		 ORDER BY created_at DESC, seq DESC LIMIT 1`,
		TitleKey(title), age, checklistVersion, since.UnixMilli()).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("cards: latest: %w", err)
	}
	return "c_" + raw, nil
}

// Summary is one row of GET /api/cards. Its JSON is the contract's, field for
// field (testdata/contracts/api/get-cards.json). T4.1 wraps a list of them as
// {"cards": [...]}.
type Summary struct {
	ID               string            `json:"id"`
	Title            string            `json:"title"`
	Age              int               `json:"age"`
	Verdict          checklist.Verdict `json:"verdict"`
	ChecklistVersion string            `json:"checklist_version"`
	CheckedAt        time.Time         `json:"checked_at"`
}

// Recent lists the newest cards, one per title and age (the newest card of
// each), newest first. Two cards at the same time order by the one stored
// last. limit must be 1 or more and is capped at MaxRecent. The list is never
// nil, so it encodes as [] when empty.
func (s *Store) Recent(ctx context.Context, limit int) ([]Summary, error) {
	if limit < 1 {
		return nil, fmt.Errorf("%w: limit must be at least 1", ErrInvalid)
	}
	limit = min(limit, MaxRecent)
	rows, err := s.r.QueryContext(ctx, `
		SELECT id, title, age, verdict, checklist, created_at FROM (
			SELECT id, title, age, verdict, checklist, created_at, seq,
				ROW_NUMBER() OVER (PARTITION BY title_key, age ORDER BY created_at DESC, seq DESC) AS rn
			FROM cards)
		WHERE rn = 1 ORDER BY created_at DESC, seq DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("cards: recent: %w", err)
	}
	defer rows.Close()
	out := []Summary{}
	for rows.Next() {
		var sm Summary
		var raw string
		var verdict string
		var ms int64
		if err := rows.Scan(&raw, &sm.Title, &sm.Age, &verdict, &sm.ChecklistVersion, &ms); err != nil {
			return nil, fmt.Errorf("cards: recent: %w", err)
		}
		sm.ID = "c_" + raw
		sm.Verdict = checklist.Verdict(verdict)
		sm.CheckedAt = time.UnixMilli(ms).UTC()
		out = append(out, sm)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cards: recent: %w", err)
	}
	return out, nil
}
