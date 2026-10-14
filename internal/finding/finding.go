// Package finding holds the reader's finding format and the quote check.
//
// A finding is one claim with a verbatim quote. It survives only if its quote
// appears, after normalization, in the whole source text of the segment its
// ref names. The check is a string match, never the model's word.
package finding

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/nrynss/before-yes/internal/checklist"
)

// Stance says which way a finding cuts.
type Stance string

// Stances.
const (
	Risk       Stance = "risk"
	Mitigation Stance = "mitigation" // a setting or control
	Neutral    Stance = "neutral"
)

// Valid reports whether s is one of the three stances.
func (s Stance) Valid() bool { return s == Risk || s == Mitigation || s == Neutral }

// Source names where a quote came from.
type Source struct {
	URL       string    `json:"url"`
	Kind      string    `json:"kind"`            // review, policy, terms, reddit, forum, news, rating
	Title     string    `json:"title,omitempty"` // page or review title
	Index     int       `json:"index,omitempty"` // review index, 1-based; 0 for a document
	FetchedAt time.Time `json:"fetched_at"`
}

// Finding is one verified claim.
type Finding struct {
	ID     string         `json:"id"`
	Area   checklist.Area `json:"area"`
	Claim  string         `json:"claim"`
	Stance Stance         `json:"stance"`
	Quote  string         `json:"quote"`
	Source Source         `json:"source"`
}

// Raw is one finding as the model writes it. Ref names the input segment the
// quote comes from, such as "R312" or "D3".
type Raw struct {
	Ref    string `json:"ref"`
	Area   string `json:"area"`
	Claim  string `json:"claim"`
	Stance string `json:"stance"`
	Quote  string `json:"quote"`
}

// Segment is the whole source text behind one ref, and its source.
type Segment struct {
	Text   string
	Source Source
}

// Drop reasons.
const (
	DropBadArea   = "bad_area"
	DropBadStance = "bad_stance"
	DropNoQuote   = "no_quote" // empty, or under MinQuote normalized characters
	DropUnquoted  = "unquoted" // the quote is not in the source text, or the ref names no segment
)

// Drops counts dropped findings by reason. Duplicates are counted apart and
// are not drops: they collapse into the finding they repeat.
type Drops struct {
	BadArea    int `json:"bad_area"`
	BadStance  int `json:"bad_stance"`
	NoQuote    int `json:"no_quote"`
	Unquoted   int `json:"unquoted"`
	Duplicates int `json:"duplicates"`
}

// Add folds o into d.
func (d *Drops) Add(o Drops) {
	d.BadArea += o.BadArea
	d.BadStance += o.BadStance
	d.NoQuote += o.NoQuote
	d.Unquoted += o.Unquoted
	d.Duplicates += o.Duplicates
}

// MinQuote is the shortest normalized quote, in runes, that proves anything.
const MinQuote = 12

// Normalize folds what the check forgives: case, runs of whitespace,
// typographic quotes and dashes, and the ellipsis character (as "...").
// It changes no word.
func Normalize(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		switch r {
		case '\u2018', '\u2019', '\u201A', '\u201B', '\u2032', '`', '\u00B4':
			r = '\''
		case '\u201C', '\u201D', '\u201E', '\u201F', '\u2033', '\u00AB', '\u00BB':
			r = '"'
		case '\u2010', '\u2011', '\u2012', '\u2013', '\u2014', '\u2015', '\u2212':
			r = '-'
		case '\u2026':
			if space {
				b.WriteByte(' ')
				space = false
			}
			b.WriteString("...")
			continue
		}
		if unicode.IsSpace(r) {
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// ID is a finding's stable id: a hash of source URL, area and normalized
// quote. A review shares its feed URL with every other review, so the review
// index is part of the URL key when set.
func ID(src Source, area checklist.Area, quote string) string {
	key := src.URL
	if src.Index > 0 {
		key += "#" + itoa(src.Index)
	}
	h := sha256.Sum256([]byte(key + "\x00" + string(area) + "\x00" + Normalize(quote)))
	return "f_" + hex.EncodeToString(h[:8])
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// Verify keeps the raw findings whose quote appears in their segment's text,
// in input order, with duplicates collapsed. It counts every drop by reason.
func Verify(raws []Raw, segs map[string]Segment) ([]Finding, Drops) {
	var d Drops
	out := []Finding{}
	seen := map[string]bool{}
	norm := map[string]string{}
	for _, r := range raws {
		area := checklist.Area(strings.TrimSpace(r.Area))
		if !area.Valid() {
			d.BadArea++
			continue
		}
		st := Stance(strings.TrimSpace(r.Stance))
		if !st.Valid() {
			d.BadStance++
			continue
		}
		q := strings.TrimSpace(r.Quote)
		nq := Normalize(q)
		if len([]rune(nq)) < MinQuote {
			d.NoQuote++
			continue
		}
		ref := strings.TrimSpace(r.Ref)
		seg, ok := segs[ref]
		if !ok {
			d.Unquoted++
			continue
		}
		nt, ok := norm[ref]
		if !ok {
			nt = Normalize(seg.Text)
			norm[ref] = nt
		}
		if !strings.Contains(nt, nq) {
			d.Unquoted++
			continue
		}
		id := ID(seg.Source, area, q)
		if seen[id] {
			d.Duplicates++
			continue
		}
		seen[id] = true
		out = append(out, Finding{ID: id, Area: area, Claim: strings.TrimSpace(r.Claim), Stance: st, Quote: q, Source: seg.Source})
	}
	return out, d
}

// ErrParse is returned when model output holds no findings JSON.
var ErrParse = errors.New("finding: no findings JSON in model output")

// Parse reads model output: a JSON object with a findings array, a bare
// array, or either one inside a code fence.
func Parse(out string) ([]Raw, error) {
	s := strings.TrimSpace(out)
	if i := strings.Index(s, "```"); i >= 0 {
		rest := s[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if j := strings.Index(rest, "```"); j >= 0 {
			rest = rest[:j]
		}
		s = strings.TrimSpace(rest)
	}
	var arr []Raw
	if err := json.Unmarshal([]byte(s), &arr); err == nil {
		return nonNil(arr), nil
	}
	var obj struct {
		Findings *[]Raw `json:"findings"`
	}
	if err := json.Unmarshal([]byte(s), &obj); err == nil && obj.Findings != nil {
		return nonNil(*obj.Findings), nil
	}
	return nil, ErrParse
}

func nonNil(r []Raw) []Raw {
	if r == nil {
		return []Raw{}
	}
	return r
}

// Cited maps findings to the view checklist.Enforce needs.
func Cited(fs []Finding) map[string]checklist.Cited {
	m := make(map[string]checklist.Cited, len(fs))
	for _, f := range fs {
		m[f.ID] = checklist.Cited{Area: f.Area, Risk: f.Stance == Risk}
	}
	return m
}
