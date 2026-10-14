// Package cards stores finished cards under unguessable ids.
package cards

import (
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

	"github.com/nrynss/before-yes/internal/pipeline"
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrNotFound reports an id with no card.
var ErrNotFound = errors.New("cards: not found")

// Store keeps cards in the application's database.
type Store struct {
	db *sql.DB
	r  *sql.DB
}

// Open applies the schema and returns the store.
func Open(ctx context.Context, db *sqlite.DB) (*Store, error) {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, err
	}
	if err := sqlite.Migrate(ctx, db, "beforeyes_cards", sub); err != nil {
		return nil, err
	}
	return &Store{db: db.Writer(), r: db.Reader()}, nil
}

// TitleKey folds a title so "Roblox" and " roblox " share cards and evidence.
func TitleKey(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(title), " "))
}

// Put stores card under a fresh id and returns the id.
func (s *Store) Put(ctx context.Context, card pipeline.Card) (string, error) {
	cid, err := id.New()
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(card)
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO cards (id, title_key, age, checklist, verdict, created_at, body) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		cid, TitleKey(card.Title), card.Age, card.Checklist, string(card.Verdict), card.CheckedAt.UnixMilli(), body)
	if err != nil {
		return "", fmt.Errorf("cards: put: %w", err)
	}
	return cid, nil
}

// Get returns the card stored under cid.
func (s *Store) Get(ctx context.Context, cid string) (pipeline.Card, error) {
	if !id.Valid(cid) {
		return pipeline.Card{}, ErrNotFound
	}
	var body []byte
	err := s.r.QueryRowContext(ctx, `SELECT body FROM cards WHERE id = ?`, cid).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return pipeline.Card{}, ErrNotFound
	}
	if err != nil {
		return pipeline.Card{}, err
	}
	var c pipeline.Card
	return c, json.Unmarshal(body, &c)
}

// Latest returns the id of the newest card for title, age and checklist
// version made after since.
func (s *Store) Latest(ctx context.Context, title string, age int, checklist string, since time.Time) (string, error) {
	var cid string
	err := s.r.QueryRowContext(ctx,
		`SELECT id FROM cards WHERE title_key = ? AND age = ? AND checklist = ? AND created_at >= ? ORDER BY created_at DESC LIMIT 1`,
		TitleKey(title), age, checklist, since.UnixMilli()).Scan(&cid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return cid, err
}

// Summary is one row of the recent list.
type Summary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Age       int       `json:"age"`
	Verdict   string    `json:"verdict"`
	CheckedAt time.Time `json:"checked_at"`
}

// Recent lists the newest cards, one per title and age.
func (s *Store) Recent(ctx context.Context, limit int) ([]Summary, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT id, json_extract(body, '$.title'), age, verdict, created_at FROM cards c
		WHERE created_at = (SELECT MAX(created_at) FROM cards d WHERE d.title_key = c.title_key AND d.age = c.age)
		ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Summary
	for rows.Next() {
		var sm Summary
		var ms int64
		if err := rows.Scan(&sm.ID, &sm.Title, &sm.Age, &sm.Verdict, &ms); err != nil {
			return nil, err
		}
		sm.CheckedAt = time.UnixMilli(ms).UTC()
		out = append(out, sm)
	}
	return out, rows.Err()
}
