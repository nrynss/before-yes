package finding

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/before-yes/internal/checklist"
)

var src = Source{URL: "https://itunes.apple.com/us/rss/customerreviews/id=431946152/json", Kind: "review", Index: 312, FetchedAt: time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)}

const text = "My daughter’s 7 and the filter didn’t catch it when someone asked my 7 year old for her number — we deleted it…"

var segs = map[string]Segment{"R312": {Text: text, Source: src}}

func TestCurlyApostrophesVerify(t *testing.T) {
	raws := []Raw{{Ref: "R312", Area: "contact", Stance: "risk", Claim: "Filter failed", Quote: "the filter didn't catch it when someone asked my 7 year old for her number"}}
	fs, d := Verify(raws, segs)
	if len(fs) != 1 || d != (Drops{}) {
		t.Fatalf("got %v %+v", fs, d)
	}
	if fs[0].Source != src || fs[0].Area != checklist.Contact {
		t.Errorf("finding lost its source: %+v", fs[0])
	}
}

func TestForgivesCaseSpaceDashEllipsis(t *testing.T) {
	q := "Asked  my 7 YEAR old for her\n number - we deleted it..."
	fs, d := Verify([]Raw{{Ref: "R312", Area: "contact", Stance: "risk", Quote: q}}, segs)
	if len(fs) != 1 {
		t.Fatalf("got %+v", d)
	}
}

func TestChangedWordFails(t *testing.T) {
	q := "the filter didn't catch it when someone asked my 8 year old for her number"
	fs, d := Verify([]Raw{{Ref: "R312", Area: "contact", Stance: "risk", Quote: q}}, segs)
	if len(fs) != 0 || d.Unquoted != 1 {
		t.Fatalf("got %v %+v", fs, d)
	}
}

func TestInventedQuoteCountedUnquoted(t *testing.T) {
	raws := []Raw{
		{Ref: "R312", Area: "contact", Stance: "risk", Quote: "strangers sent my son messages every day"},
		{Ref: "R999", Area: "contact", Stance: "risk", Quote: "the filter didn't catch it when someone asked"},
	}
	fs, d := Verify(raws, segs)
	if len(fs) != 0 || d.Unquoted != 2 {
		t.Fatalf("got %v %+v", fs, d)
	}
}

func TestDropReasons(t *testing.T) {
	raws := []Raw{
		{Ref: "R312", Area: "vibes", Stance: "risk", Quote: "the filter didn't catch it"},
		{Ref: "R312", Area: "contact", Stance: "scary", Quote: "the filter didn't catch it"},
		{Ref: "R312", Area: "contact", Stance: "risk", Quote: ""},
		{Ref: "R312", Area: "contact", Stance: "risk", Quote: "we deleted it"}, // 13 runes: long enough, and verifies
		{Ref: "R312", Area: "contact", Stance: "risk", Quote: "deleted it"},
	}
	_, d := Verify(raws, segs)
	want := Drops{BadArea: 1, BadStance: 1, NoQuote: 2}
	if d != want {
		t.Errorf("got %+v want %+v", d, want)
	}
}

func TestStableIDAndDuplicates(t *testing.T) {
	a := ID(src, checklist.Contact, "The filter didn’t catch it")
	b := ID(src, checklist.Contact, "the  filter didn't catch it")
	if a != b || !strings.HasPrefix(a, "f_") {
		t.Errorf("ids differ: %s %s", a, b)
	}
	if ID(src, checklist.Content, "the filter didn't catch it") == a {
		t.Error("area not in id")
	}
	other := src
	other.Index = 313
	if ID(other, checklist.Contact, "the filter didn't catch it") == a {
		t.Error("review index not in id")
	}
	r := Raw{Ref: "R312", Area: "contact", Stance: "risk", Quote: "the filter didn't catch it"}
	r2 := r
	r2.Quote = "The filter didn’t catch it"
	fs, d := Verify([]Raw{r, r2}, segs)
	if len(fs) != 1 || d.Duplicates != 1 || fs[0].ID != a {
		t.Errorf("got %v %+v", fs, d)
	}
	fs2, _ := Verify([]Raw{r}, segs)
	if fs2[0].ID != fs[0].ID {
		t.Error("id not stable across runs")
	}
}

func TestQuoteAcrossChunkEdge(t *testing.T) {
	// The check runs on the segment's whole text, not on a chunk of it.
	long := strings.Repeat("filler sentence. ", 400) + "the chat let a stranger ask for our address" + strings.Repeat(" more filler.", 400)
	s := map[string]Segment{"D3": {Text: long, Source: Source{URL: "https://example.org/policy", Kind: "policy"}}}
	fs, _ := Verify([]Raw{{Ref: "D3", Area: "contact", Stance: "risk", Quote: "the chat let a stranger ask for our address"}}, s)
	if len(fs) != 1 {
		t.Fatal("quote in long document did not verify")
	}
}

func TestParse(t *testing.T) {
	item := `{"ref":"R1","area":"money","claim":"c","stance":"risk","quote":"q"}`
	for name, in := range map[string]string{
		"object":       `{"findings":[` + item + `]}`,
		"bare":         `[` + item + `]`,
		"fenced obj":   "Here you go:\n```json\n{\"findings\":[" + item + "]}\n```\nthanks",
		"fenced array": "```\n[" + item + "]\n```",
	} {
		rs, err := Parse(in)
		if err != nil || len(rs) != 1 || rs[0].Ref != "R1" || rs[0].Area != "money" {
			t.Errorf("%s: %v %v", name, rs, err)
		}
	}
	if rs, err := Parse(`{"findings":[]}`); err != nil || rs == nil || len(rs) != 0 {
		t.Errorf("empty: %v %v", rs, err)
	}
	for _, bad := range []string{"", "no json here", `{"other":1}`, "```\nnot json\n```"} {
		if _, err := Parse(bad); !errors.Is(err, ErrParse) {
			t.Errorf("%q: got %v", bad, err)
		}
	}
}

// A real fixture review verifies against its own text.
func TestFixtureReview(t *testing.T) {
	b, err := os.ReadFile("../../testdata/titles/roblox/reviews-page-01.json")
	if err != nil {
		t.Fatal(err)
	}
	var feed struct {
		Feed struct {
			Entry []struct {
				Content struct{ Label string } `json:"content"`
			} `json:"entry"`
		} `json:"feed"`
	}
	if err := json.Unmarshal(b, &feed); err != nil || len(feed.Feed.Entry) == 0 {
		t.Fatal(err)
	}
	body := feed.Feed.Entry[0].Content.Label
	quote := strings.ReplaceAll(body[:80], "’", "'")
	s := map[string]Segment{"R1": {Text: body, Source: Source{URL: "u", Kind: "review", Index: 1}}}
	fs, d := Verify([]Raw{{Ref: "R1", Area: "money", Stance: "risk", Quote: quote}}, s)
	if len(fs) != 1 {
		t.Fatalf("fixture quote failed: %+v", d)
	}
}
