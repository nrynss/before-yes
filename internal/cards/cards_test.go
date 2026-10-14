package cards

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	cachesqlitestore "github.com/nrynss/keel/cache/sqlitestore"
	jobsqlitestore "github.com/nrynss/keel/job/sqlitestore"
	"github.com/nrynss/keel/sqlite"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/pipeline"
	"github.com/nrynss/before-yes/internal/spend"
)

const contracts = "../../testdata/contracts"

var t0 = time.Date(2026, 10, 13, 18, 58, 40, 0, time.UTC)

func openDB(t *testing.T) *sqlite.DB {
	t.Helper()
	db, err := sqlite.Open(context.Background(), sqlite.Config{Path: filepath.Join(t.TempDir(), "app.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), openDB(t))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// mk is a small card the way pipeline.Decide makes one: a pipeline id, and
// no alternatives.
func mk(title string, age int, v checklist.Verdict, at time.Time) pipeline.Card {
	return pipeline.Card{
		ID: "c_" + strings.ReplaceAll(strings.ToLower(title), " ", "") + "12345678", Title: title,
		Kind: pipeline.KindIOS, KindLabel: "iOS app", Age: age, ChecklistVersion: checklist.Version,
		CheckedAt: at, CollectedAt: at, Verdict: v,
		Grades: []checklist.Grade{}, Settings: []string{}, Alternatives: pipeline.Alternatives{},
	}
}

func contractCards(t *testing.T) map[string][]byte {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(contracts, "card-*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no contract cards: %v", err)
	}
	out := map[string][]byte{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.Base(f)] = b
	}
	return out
}

func asMap(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

var (
	keelID   = regexp.MustCompile(`^[0-9a-f]{32}$`)
	publicID = regexp.MustCompile(`^c_[0-9a-f]{32}$`)
)

// An id that does not start with "c_" or whose remainder is not a 32-hex Keel
// id is refused before any query. The proof is a database closed under the
// store: a query would fail with a database error, and the invalid id still
// comes back ErrNotFound.
func TestInvalidIDRefusedBeforeAnyQuery(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	s, err := Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	validHex := strings.Repeat("ab", 16)
	validUnknown := "c_" + validHex
	invalid := []string{
		// Bare hex (no c_ prefix)
		validHex,
		// Wrong prefix
		"j_" + validHex,
		"x_" + validHex,
		// Fixture-style non-hex IDs
		"c_7Hq2mXv9pK3wLr8sTn4bYe",
		"c_Tc6vRb1nQ8zKp2dWm5sLhA",
		// Other invalid formats
		"", "short", "c_", "c_short",
		"c_" + strings.ToUpper(validHex),
		"c_" + validHex + "0",
		"c_" + validHex[:31],
		"c_zz" + validHex[2:],
		"' OR 1=1 --",
		"c_' OR 1=1 --",
		validHex[:30] + "\n\n",
		"../" + validHex,
	}

	// On a live store: invalid is NotFound, and so is a valid unknown id.
	for _, bad := range invalid {
		if _, err := s.Get(ctx, bad); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q) = %v, want ErrNotFound", bad, err)
		}
	}
	if _, err := s.Get(ctx, validUnknown); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(valid unknown) = %v, want ErrNotFound", err)
	}

	// On a closed database any query is an error, so an invalid id that
	// still answers ErrNotFound never reached it.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = s.Get(ctx, validUnknown)
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(valid unknown) on a closed db = %v, want a database error that is not ErrNotFound", err)
	}
	for _, bad := range invalid {
		if _, err := s.Get(ctx, bad); !errors.Is(err, ErrNotFound) {
			t.Errorf("closed db: Get(%q) = %v, want ErrNotFound (no query)", bad, err)
		}
	}
}

func TestGetUndecodableBodyIsAnErrorNotNotFound(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	cid, err := s.Put(ctx, mk("Roblox", 9, checklist.No, t0))
	if err != nil {
		t.Fatal(err)
	}
	rawID := strings.TrimPrefix(cid, "c_")
	if _, err := s.w.ExecContext(ctx, `UPDATE cards SET body = ? WHERE id = ?`, []byte(`{"age": "nine"`), rawID); err != nil {
		t.Fatal(err)
	}
	_, err = s.Get(ctx, cid)
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(corrupt) = %v, want a decode error that is not ErrNotFound", err)
	}
}

func TestPutGetRoundTripKeelID(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	for name, raw := range contractCards(t) {
		var in pipeline.Card
		if err := json.Unmarshal(raw, &in); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		orig := in.ID
		cid, err := s.Put(ctx, in)
		if err != nil {
			t.Fatalf("%s: Put: %v", name, err)
		}
		if !publicID.MatchString(cid) {
			t.Errorf("%s: id %q does not match ^c_[0-9a-f]{32}$", name, cid)
		}
		if in.ID != orig {
			t.Errorf("%s: Put changed the caller's card id %q to %q", name, orig, in.ID)
		}
		got, err := s.Get(ctx, cid)
		if err != nil {
			t.Fatalf("%s: Get: %v", name, err)
		}
		if got.ID != cid {
			t.Errorf("%s: body id %q, row id %q", name, got.ID, cid)
		}
		// The stored row's own id column is the 32-hex Keel ID without prefix.
		rawID := strings.TrimPrefix(cid, "c_")
		if !keelID.MatchString(rawID) {
			t.Errorf("%s: raw id %q is not 32 lowercase hex", name, rawID)
		}
		var colID string
		if err := s.r.QueryRowContext(ctx, `SELECT id FROM cards WHERE id = ?`, rawID).Scan(&colID); err != nil || colID != rawID {
			t.Errorf("%s: row id column = %q (%v), want %q", name, colID, err, rawID)
		}
		var rowJSONID string
		if err := s.r.QueryRowContext(ctx, `SELECT json_extract(body, '$.id') FROM cards WHERE id = ?`, rawID).Scan(&rowJSONID); err != nil || rowJSONID != cid {
			t.Errorf("%s: json id in the stored body = %q (%v), want %q", name, rowJSONID, err, cid)
		}
		// Everything but the id is the contract card, byte for byte as JSON.
		want := asMap(t, raw)
		want["id"] = cid
		out, _ := json.Marshal(got)
		if !reflect.DeepEqual(want, asMap(t, out)) {
			t.Errorf("%s: round trip lost data\nwant %s\ngot  %s", name, raw, out)
		}
	}
}

func TestPutRefusesZeroCheckedAt(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	c := mk("Roblox", 9, checklist.No, time.Time{})
	if _, err := s.Put(ctx, c); !errors.Is(err, ErrInvalid) {
		t.Errorf("Put(zero CheckedAt) = %v, want ErrInvalid", err)
	}
	if _, _, err := s.PutWithAlternatives(ctx, c, nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("PutWithAlternatives(zero CheckedAt) = %v, want ErrInvalid", err)
	}
	c = mk("   ", 9, checklist.No, t0)
	if _, err := s.Put(ctx, c); !errors.Is(err, ErrInvalid) {
		t.Errorf("Put(no title) = %v, want ErrInvalid", err)
	}
	if n := count(t, s); n != 0 {
		t.Errorf("%d rows after refused puts", n)
	}
}

func count(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM cards`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestLatestByTitleAgeChecklistAndSince(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	older, _ := s.Put(ctx, mk("Roblox", 9, checklist.No, t0))
	newer, _ := s.Put(ctx, mk("roblox", 9, checklist.YesWithSettings, t0.Add(time.Hour)))
	age10, _ := s.Put(ctx, mk("Roblox", 10, checklist.Yes, t0.Add(2*time.Hour)))
	v6 := mk("Roblox", 9, checklist.Yes, t0.Add(3*time.Hour))
	v6.ChecklistVersion = "v6"
	oldVersion, _ := s.Put(ctx, v6)
	other, _ := s.Put(ctx, mk("Minecraft", 9, checklist.Yes, t0.Add(4*time.Hour)))

	cases := []struct {
		name    string
		title   string
		age     int
		version string
		since   time.Time
		want    string
	}{
		{"newest of two", "Roblox", 9, checklist.Version, time.Time{}, newer},
		{"title folds case and spaces", "  ROBLOX ", 9, checklist.Version, time.Time{}, newer},
		{"another age", "Roblox", 10, checklist.Version, time.Time{}, age10},
		{"another checklist version", "Roblox", 9, "v6", time.Time{}, oldVersion},
		{"another title", "Minecraft", 9, checklist.Version, time.Time{}, other},
		{"since is inclusive", "Roblox", 9, checklist.Version, t0.Add(time.Hour), newer},
		{"since past the newest", "Roblox", 9, checklist.Version, t0.Add(time.Hour + time.Millisecond), ""},
		{"no such age", "Roblox", 11, checklist.Version, time.Time{}, ""},
		{"no such version", "Roblox", 9, "v0", time.Time{}, ""},
		{"no such title", "Fortnite", 9, checklist.Version, time.Time{}, ""},
	}
	for _, c := range cases {
		got, err := s.Latest(ctx, c.title, c.age, c.version, c.since)
		if c.want == "" {
			if !errors.Is(err, ErrNotFound) || got != "" {
				t.Errorf("%s: Latest = %q, %v; want ErrNotFound", c.name, got, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%s: Latest = %q, %v; want %q", c.name, got, err, c.want)
		}
		if !publicID.MatchString(got) {
			t.Errorf("%s: Latest returned %q; want ^c_[0-9a-f]{32}$", c.name, got)
		}
	}
	if older == newer {
		t.Fatal("ids collided")
	}

	// A tie on the checked time goes to the card stored last, every time.
	first, _ := s.Put(ctx, mk("Tie", 5, checklist.No, t0))
	second, _ := s.Put(ctx, mk("Tie", 5, checklist.Yes, t0))
	for i := 0; i < 5; i++ {
		if got, err := s.Latest(ctx, "Tie", 5, checklist.Version, time.Time{}); err != nil || got != second {
			t.Fatalf("tie: Latest = %q, %v; want the later insert %q (not %q)", got, err, second, first)
		}
	}
}

// Verdict and time are columns, and an index serves them, so a lookup by
// verdict and timestamp is one query. The store has no method for it yet: no
// route asks for one. The query is the one below.
func TestVerdictAndTimeLookupFromColumns(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	no, _ := s.Put(ctx, mk("Roblox", 9, checklist.No, t0))
	_, _ = s.Put(ctx, mk("Minecraft", 9, checklist.Yes, t0.Add(time.Hour)))
	noLater, _ := s.Put(ctx, mk("Toca", 6, checklist.No, t0.Add(2*time.Hour)))

	const q = `SELECT 'c_' || id FROM cards WHERE verdict = ? AND created_at >= ? ORDER BY created_at`
	rows, err := s.r.QueryContext(ctx, q, string(checklist.No), t0.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	if !reflect.DeepEqual(ids, []string{no, noLater}) {
		t.Errorf("verdict no since t0 = %v, want %v", ids, []string{no, noLater})
	}

	plan, err := s.r.QueryContext(ctx, `EXPLAIN QUERY PLAN `+q, string(checklist.No), t0.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	var details []string
	for plan.Next() {
		var a, b, c int
		var d string
		if err := plan.Scan(&a, &b, &c, &d); err != nil {
			t.Fatal(err)
		}
		details = append(details, d)
	}
	if !strings.Contains(strings.Join(details, "; "), "cards_verdict") {
		t.Errorf("the verdict and time lookup does not use cards_verdict: %v", details)
	}
}

func TestRecentOnePerTitleAndAge(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)

	if got, err := s.Recent(ctx, 10); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("Recent on an empty store = %#v, %v; want an empty non-nil list", got, err)
	}
	b, _ := json.Marshal(map[string]any{"cards": mustRecent(t, s, 10)})
	if string(b) != `{"cards":[]}` {
		t.Errorf("empty list encodes as %s, want {\"cards\":[]}", b)
	}
	if _, err := s.Recent(ctx, 0); !errors.Is(err, ErrInvalid) {
		t.Errorf("Recent(0) = %v, want ErrInvalid", err)
	}

	_, _ = s.Put(ctx, mk("Roblox", 9, checklist.No, t0))
	robloxNew, _ := s.Put(ctx, mk("ROBLOX", 9, checklist.YesWithSettings, t0.Add(time.Hour)))
	robloxTen, _ := s.Put(ctx, mk("Roblox", 10, checklist.No, t0.Add(2*time.Hour)))
	// A tie on title, age and time: only the one stored last appears.
	_, _ = s.Put(ctx, mk("Tie", 5, checklist.No, t0.Add(3*time.Hour)))
	tieLast, _ := s.Put(ctx, mk("tie", 5, checklist.Yes, t0.Add(3*time.Hour)))
	khan, _ := s.Put(ctx, mk("Khan Academy Kids", 5, checklist.Yes, t0.Add(30*time.Minute)))

	got := mustRecent(t, s, 10)
	var ids []string
	for _, sm := range got {
		if !publicID.MatchString(sm.ID) {
			t.Errorf("Recent ID %q does not match ^c_[0-9a-f]{32}$", sm.ID)
		}
		ids = append(ids, sm.ID)
	}
	want := []string{tieLast, robloxTen, robloxNew, khan}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("Recent ids = %v, want %v (newest first, one per title and age)", ids, want)
	}
	if got[2].Title != "ROBLOX" || got[2].Verdict != checklist.YesWithSettings || got[2].Age != 9 ||
		got[2].ChecklistVersion != checklist.Version || !got[2].CheckedAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("summary = %+v", got[2])
	}
	if two := mustRecent(t, s, 2); len(two) != 2 || two[0].ID != tieLast || two[1].ID != robloxTen {
		t.Errorf("limit 2 = %+v", two)
	}

	// The summary is the contract's: the same fields, in the same names.
	raw, err := os.ReadFile(filepath.Join(contracts, "api", "get-cards.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Response struct {
			Body struct {
				Cards []json.RawMessage `json:"cards"`
			} `json:"body"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &contract); err != nil || len(contract.Response.Body.Cards) == 0 {
		t.Fatalf("contract: %v", err)
	}
	for i, entry := range contract.Response.Body.Cards {
		var sm Summary
		d := json.NewDecoder(bytes.NewReader(entry))
		d.DisallowUnknownFields()
		if err := d.Decode(&sm); err != nil {
			t.Fatalf("contract entry %d does not decode into Summary: %v", i, err)
		}
		out, _ := json.Marshal(sm)
		if !reflect.DeepEqual(asMap(t, entry), asMap(t, out)) {
			t.Errorf("contract entry %d: %s re-encodes as %s", i, entry, out)
		}
	}
	out, _ := json.Marshal(got[0])
	keys := []string{}
	for k := range asMap(t, out) {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if strings.Join(keys, ",") != "age,checked_at,checklist_version,id,title,verdict" {
		t.Errorf("summary fields = %v", keys)
	}
}

func mustRecent(t *testing.T, s *Store, n int) []Summary {
	t.Helper()
	got, err := s.Recent(context.Background(), n)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestPutWithAlternativesRemapsIDs(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)

	altA := mk("Khan Academy Kids", 9, checklist.Yes, t0)
	altB := mk("Toca Life World", 9, checklist.YesWithSettings, t0)
	main := mk("Roblox", 9, checklist.No, t0)
	main.AlternativesNote = pipeline.MoreAlternativesNote
	main.Alternatives = pipeline.Alternatives{
		{Title: altA.Title, Reason: "r", CardID: altA.ID, Verdict: checklist.Yes, Status: pipeline.AltChecked},
		{Title: "Gone", Reason: "r", CardID: "c_neverstored00000000000", Verdict: checklist.Yes, Status: pipeline.AltChecked},
		{Title: "Unlinked", Reason: "r", Verdict: checklist.Yes, Status: pipeline.AltChecked},
		{Title: "Pending", Reason: "r", Status: pipeline.AltPending},
		{Title: altB.Title, Reason: "r", CardID: altB.ID, Verdict: checklist.YesWithSettings, Status: pipeline.AltChecked},
		{Title: "Refused", Reason: "r", Status: pipeline.AltRefused},
	}
	origLink := main.Alternatives[0].CardID

	mainID, altIDs, err := s.PutWithAlternatives(ctx, main, []pipeline.Card{altA, altB})
	if err != nil {
		t.Fatal(err)
	}
	if len(altIDs) != 2 || !publicID.MatchString(mainID) || !publicID.MatchString(altIDs[0]) || !publicID.MatchString(altIDs[1]) {
		t.Fatalf("ids = %q %q", mainID, altIDs)
	}
	if main.ID == mainID || main.Alternatives[0].CardID != origLink || len(main.Alternatives) != 6 || altA.ID == altIDs[0] {
		t.Error("the caller's cards were changed")
	}

	got, err := s.Get(ctx, mainID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != mainID || got.AlternativesNote != pipeline.MoreAlternativesNote {
		t.Errorf("main = %q note %q", got.ID, got.AlternativesNote)
	}
	if len(got.Alternatives) != 2 || got.Alternatives[0].Title != altA.Title || got.Alternatives[1].Title != altB.Title {
		t.Fatalf("stored alternatives = %+v, want the two with stored cards (dangling, unlinked, pending and refused dropped)", got.Alternatives)
	}
	for i, a := range got.Alternatives {
		if a.CardID != altIDs[i] || !publicID.MatchString(a.CardID) {
			t.Errorf("alternative %d links %q, want the public id %q", i, a.CardID, altIDs[i])
		}
		linked, err := s.Get(ctx, a.CardID)
		if err != nil {
			t.Fatalf("alternative %d does not resolve: %v", i, err)
		}
		if linked.Title != a.Title || linked.ID != a.CardID || linked.Verdict != a.Verdict {
			t.Errorf("alternative %d resolves to %q %q %s", i, linked.ID, linked.Title, linked.Verdict)
		}
	}

	// An alt card's own links obey the same rule: a link to a card not stored
	// is dropped, a link to another alt in the batch is rewritten.
	altA.Alternatives = pipeline.Alternatives{
		{Title: altB.Title, CardID: altB.ID, Verdict: checklist.YesWithSettings, Status: pipeline.AltChecked},
		{Title: "Gone", CardID: "c_neverstored00000000000", Status: pipeline.AltChecked},
	}
	_, ids2, err := s.PutWithAlternatives(ctx, mk("Roblox", 12, checklist.No, t0), []pipeline.Card{altA, altB})
	if err != nil {
		t.Fatal(err)
	}
	a2, _ := s.Get(ctx, ids2[0])
	if len(a2.Alternatives) != 1 || a2.Alternatives[0].CardID != ids2[1] {
		t.Errorf("alt card links = %+v, want one link to %s", a2.Alternatives, ids2[1])
	}
	if !publicID.MatchString(a2.Alternatives[0].CardID) {
		t.Errorf("a2 alternative link %q does not match ^c_[0-9a-f]{32}$", a2.Alternatives[0].CardID)
	}
	if linkedAlt, err := s.Get(ctx, a2.Alternatives[0].CardID); err != nil || linkedAlt.ID != ids2[1] {
		t.Fatalf("alt card alternative link does not resolve with Get: %v", err)
	}

	// No alternatives at all is a plain Put.
	plain, none, err := s.PutWithAlternatives(ctx, mk("Plain", 9, checklist.Yes, t0), nil)
	if err != nil || len(none) != 0 {
		t.Fatalf("no alts: %v %v", none, err)
	}
	if c, err := s.Get(ctx, plain); err != nil || c.Alternatives == nil {
		t.Errorf("plain card: %+v, %v (alternatives must encode as a list)", c, err)
	}

	// Two alt cards with one pipeline id cannot be told apart.
	if _, _, err := s.PutWithAlternatives(ctx, main, []pipeline.Card{altA, altA}); !errors.Is(err, ErrInvalid) {
		t.Errorf("duplicate alt ids = %v, want ErrInvalid", err)
	}
}

// Nothing is stored if any insert fails: a trigger makes the main card's
// insert fail after the alternatives' inserts went through.
func TestPutWithAlternativesIsAtomic(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	if _, err := s.w.ExecContext(ctx, `CREATE TRIGGER refuse_boom BEFORE INSERT ON cards WHEN NEW.title_key = 'boom'
		BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatal(err)
	}
	alt := mk("Khan Academy Kids", 9, checklist.Yes, t0)
	main := mk("Boom", 9, checklist.No, t0)
	main.Alternatives = pipeline.Alternatives{{Title: alt.Title, CardID: alt.ID, Verdict: checklist.Yes, Status: pipeline.AltChecked}}

	_, _, err := s.PutWithAlternatives(ctx, main, []pipeline.Card{alt})
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("err = %v, want the trigger's refusal", err)
	}
	if n := count(t, s); n != 0 {
		t.Fatalf("%d rows stored after a failed put, want 0 (the alternative must roll back)", n)
	}
	if got := mustRecent(t, s, 10); len(got) != 0 {
		t.Errorf("Recent after a failed put = %+v", got)
	}

	// A refused alternative (zero CheckedAt) stores nothing either.
	bad := alt
	bad.CheckedAt = time.Time{}
	if _, _, err := s.PutWithAlternatives(ctx, mk("Fine", 9, checklist.No, t0), []pipeline.Card{bad}); !errors.Is(err, ErrInvalid) {
		t.Errorf("zero CheckedAt alternative = %v, want ErrInvalid", err)
	}
	if n := count(t, s); n != 0 {
		t.Errorf("%d rows after a refused alternative", n)
	}
}

// The cards schema, Keel's cache index, Keel's job store and the cost
// stores all live in one file, each in its own namespace, and opening them
// again changes nothing.
func TestMigrationsOwnNamespaceCoexist(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "app.db")

	type opened struct {
		db    *sqlite.DB
		cards *Store
		meter *spend.Meter
	}
	open := func() opened {
		db, err := sqlite.Open(ctx, sqlite.Config{Path: path})
		if err != nil {
			t.Fatal(err)
		}
		cs, err := Open(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cachesqlitestore.Open(ctx, cachesqlitestore.Config{DB: db}); err != nil {
			t.Fatal(err)
		}
		if _, err := cachesqlitestore.Open(ctx, cachesqlitestore.Config{DB: db, Namespace: "evidence"}); err != nil {
			t.Fatal(err)
		}
		if _, err := jobsqlitestore.Open(ctx, jobsqlitestore.Config{DB: db}); err != nil {
			t.Fatal(err)
		}
		// The meter opens its own handle on the same file.
		m, err := spend.Open(ctx, spend.Config{Path: path, Pools: map[spend.Pool]spend.PoolConfig{
			spend.PoolModel: {Limit: 1_000_000}, spend.PoolTavily: {Limit: 1_000}, spend.PoolChecksDaily: {Limit: 5},
		}})
		if err != nil {
			t.Fatal(err)
		}
		return opened{db, cs, m}
	}
	ledgers := func(db *sqlite.DB) map[string]int {
		rows, err := db.Reader().QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name LIKE '%\_schema\_migrations' ESCAPE '\'`)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatal(err)
			}
			names = append(names, n)
		}
		_ = rows.Close()
		out := map[string]int{}
		for _, n := range names {
			var c int
			if err := db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM "`+n+`"`).Scan(&c); err != nil {
				t.Fatal(err)
			}
			out[n] = c
		}
		return out
	}

	a := open()
	cid, err := a.cards.Put(ctx, mk("Roblox", 9, checklist.No, t0))
	if err != nil {
		t.Fatal(err)
	}
	first := ledgers(a.db)
	for _, ns := range []string{Namespace, "cache", "evidence", "job", "cost_model", "cost_tavily", "cost_checks_daily"} {
		if first[ns+"_schema_migrations"] == 0 {
			t.Errorf("no ledger rows for namespace %q: %v", ns, first)
		}
	}
	files, _ := migrations.ReadDir("migrations")
	if got := first[Namespace+"_schema_migrations"]; got != len(files) {
		t.Errorf("cards ledger has %d rows, want %d (one per migration file)", got, len(files))
	}
	// The cards table is the only table named cards, and Keel's tables carry
	// their namespace, so none of them is shadowed.
	var n int
	if err := a.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'cards'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("tables named cards = %d (%v)", n, err)
	}
	if err := a.meter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Close(); err != nil {
		t.Fatal(err)
	}

	// Open it all again, and the cards store a second time on the same handle.
	b := open()
	defer b.db.Close()
	defer b.meter.Close()
	if _, err := Open(ctx, b.db); err != nil {
		t.Fatalf("a second Open on one handle: %v", err)
	}
	if second := ledgers(b.db); !reflect.DeepEqual(first, second) {
		t.Errorf("reopen changed the ledgers:\n%v\n%v", first, second)
	}
	if got, err := b.cards.Get(ctx, cid); err != nil || got.ID != cid {
		t.Errorf("card after reopen = %q, %v", got.ID, err)
	}
}

func TestTitleKeyMatchesEvidenceKey(t *testing.T) {
	for _, title := range []string{
		"Roblox", "  roblox ", "ROBLOX\t", "Khan  Academy\nKids", "Animal Crossing: New Horizons", "Pokémon GO", "İstanbul", "a|b", "", "   ",
	} {
		want := pipeline.EvidenceKey(title, "us", "model")
		got := strings.Join([]string{"evidence", pipeline.GatherVersion(), pipeline.ReaderVersion(), "us", "model", TitleKey(title)}, "|")
		if got != want {
			t.Errorf("title %q: TitleKey folds to %q, EvidenceKey's key is %q", title, TitleKey(title), want)
		}
	}
	if TitleKey("Roblox") != TitleKey("  rOBLOX ") {
		t.Error("case and spaces must fold together")
	}
}

func TestIDsMatchPublicCardIDFormat(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	main := mk("Main", 9, checklist.Yes, t0)
	alt := mk("Alt", 9, checklist.Yes, t0)
	main.Alternatives = pipeline.Alternatives{{Title: alt.Title, CardID: alt.ID, Status: pipeline.AltChecked, Verdict: checklist.Yes}}

	// Put
	putID, err := s.Put(ctx, mk("Single", 9, checklist.Yes, t0))
	if err != nil {
		t.Fatal(err)
	}
	if !publicID.MatchString(putID) {
		t.Errorf("Put ID %q does not match %s", putID, publicID)
	}
	if keelID.MatchString(putID) {
		t.Errorf("Put ID %q is bare hex; must have c_ prefix", putID)
	}

	// PutWithAlternatives
	mainID, altIDs, err := s.PutWithAlternatives(ctx, main, []pipeline.Card{alt})
	if err != nil {
		t.Fatal(err)
	}
	if !publicID.MatchString(mainID) {
		t.Errorf("PutWithAlternatives mainID %q does not match %s", mainID, publicID)
	}
	if len(altIDs) != 1 || !publicID.MatchString(altIDs[0]) {
		t.Errorf("PutWithAlternatives altID %q does not match %s", altIDs, publicID)
	}

	// Stored card's JSON body and relinked alternatives
	gotMain, err := s.Get(ctx, mainID)
	if err != nil {
		t.Fatalf("Get(mainID): %v", err)
	}
	if gotMain.ID != mainID || !publicID.MatchString(gotMain.ID) {
		t.Errorf("gotMain.ID = %q, want %q", gotMain.ID, mainID)
	}
	if len(gotMain.Alternatives) != 1 || gotMain.Alternatives[0].CardID != altIDs[0] {
		t.Errorf("gotMain.Alternatives[0].CardID = %q, want %q", gotMain.Alternatives[0].CardID, altIDs[0])
	}
	gotAlt, err := s.Get(ctx, gotMain.Alternatives[0].CardID)
	if err != nil {
		t.Fatalf("Get(relinked alt): %v", err)
	}
	if gotAlt.ID != altIDs[0] || !publicID.MatchString(gotAlt.ID) {
		t.Errorf("gotAlt.ID = %q, want %q", gotAlt.ID, altIDs[0])
	}

	// Latest
	latestID, err := s.Latest(ctx, "Single", 9, checklist.Version, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if !publicID.MatchString(latestID) {
		t.Errorf("Latest ID %q does not match %s", latestID, publicID)
	}
	if latestID != putID {
		t.Errorf("Latest ID = %q, want %q", latestID, putID)
	}

	// Recent
	recs, err := s.Recent(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) == 0 {
		t.Fatal("Recent returned 0 summaries")
	}
	for _, sm := range recs {
		if !publicID.MatchString(sm.ID) {
			t.Errorf("Recent summary ID %q does not match %s", sm.ID, publicID)
		}
	}

	// Ensure stored table row keys on the 32-hex Keel ID without prefix.
	rawPut := strings.TrimPrefix(putID, "c_")
	if !keelID.MatchString(rawPut) {
		t.Errorf("rawPut %q is not 32 lowercase hex", rawPut)
	}
	var colID string
	if err := s.r.QueryRowContext(ctx, `SELECT id FROM cards WHERE id = ?`, rawPut).Scan(&colID); err != nil || colID != rawPut {
		t.Errorf("cards table id column = %q (%v), want raw 32-hex %q", colID, err, rawPut)
	}
}
