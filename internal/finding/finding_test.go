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
	cut := strings.LastIndex(body[:80], " ")
	quote := strings.ReplaceAll(body[:cut], "’", "'")
	s := map[string]Segment{"R1": {Text: body, Source: Source{URL: "u", Kind: "review", Index: 1}}}
	fs, d := Verify([]Raw{{Ref: "R1", Area: "money", Stance: "risk", Quote: quote}}, s)
	if len(fs) != 1 {
		t.Fatalf("fixture quote failed: %+v", d)
	}
}

// T1.2 round 1 H1: a quote may not start or end inside a word.
func TestWordBoundaries(t *testing.T) {
	s := map[string]Segment{"D1": {Text: "This game is unsafe for young kids, chat is open to anyone.", Source: Source{URL: "u", Kind: "forum"}}}
	for _, q := range []string{"safe for young kids", "s unsafe for young k", "is unsafe for young k", "his game is unsafe"} {
		fs, d := Verify([]Raw{{Ref: "D1", Area: "contact", Stance: "mitigation", Quote: q}}, s)
		if len(fs) != 0 || d.Unquoted != 1 {
			t.Errorf("%q verified across a word cut", q)
		}
	}
	for _, q := range []string{"is unsafe for young kids", "This game is unsafe", "chat is open to anyone.", "unsafe for young kids, chat"} {
		if fs, _ := Verify([]Raw{{Ref: "D1", Area: "contact", Stance: "risk", Quote: q}}, s); len(fs) != 1 {
			t.Errorf("%q did not verify", q)
		}
	}
	// A later whole-word occurrence still verifies after an earlier partial one.
	s2 := map[string]Segment{"D1": {Text: "it is unsafe for kids. it is safe for kids at home", Source: Source{URL: "u"}}}
	if fs, _ := Verify([]Raw{{Ref: "D1", Area: "contact", Stance: "risk", Quote: "safe for kids at home"}}, s2); len(fs) != 1 {
		t.Error("whole-word occurrence after a partial one was missed")
	}
}

// T1.2 round 1 M1: risk and reassurance readings of one quote collapse to the risk.
func TestDuplicateRiskWins(t *testing.T) {
	q := "the filter didn't catch it when someone asked"
	mit := Raw{Ref: "R312", Area: "contact", Stance: "mitigation", Claim: "filter exists", Quote: q}
	risk := Raw{Ref: "R312", Area: "contact", Stance: "risk", Claim: "filter failed", Quote: q}
	for name, in := range map[string][]Raw{"mitigation first": {mit, risk}, "risk first": {risk, mit}} {
		fs, d := Verify(in, segs)
		if len(fs) != 1 || fs[0].Stance != Risk || fs[0].Claim != "filter failed" || d.Duplicates != 1 {
			t.Errorf("%s: got %+v %+v", name, fs, d)
		}
	}
	other := Raw{Ref: "R312", Area: "contact", Stance: "neutral", Quote: "asked my 7 year old for her number"}
	fs, _ := Verify([]Raw{mit, other, risk}, segs)
	if len(fs) != 2 || fs[0].Stance != Risk || fs[1].Stance != Neutral {
		t.Errorf("order not kept: %+v", fs)
	}
}

// T1.2 round 1 L1: composed and decomposed accents match.
func TestNFC(t *testing.T) {
	s := map[string]Segment{"D1": {Text: "caf\u00e9 chat is filtered well", Source: Source{URL: "u"}}}
	if fs, _ := Verify([]Raw{{Ref: "D1", Area: "contact", Stance: "mitigation", Quote: "cafe\u0301 chat is filtered"}}, s); len(fs) != 1 {
		t.Error("decomposed accent did not match composed source")
	}
	if Normalize("cafe\u0301") != Normalize("caf\u00e9") {
		t.Error("Normalize is not NFC")
	}
}

// T1.2 round 1 L2: prose before unfenced JSON.
func TestParseProse(t *testing.T) {
	item := `{"ref":"R1","area":"money","claim":"c","stance":"risk","quote":"q"}`
	for _, in := range []string{
		"Here are the findings: {\"findings\":[" + item + "]}",
		"Findings [see below]:\n[" + item + "]\nDone.",
	} {
		if rs, err := Parse(in); err != nil || len(rs) != 1 {
			t.Errorf("%q: %v %v", in, rs, err)
		}
	}
	if _, err := Parse("I found {nothing} today"); !errors.Is(err, ErrParse) {
		t.Error("prose with braces parsed")
	}
}

// T1.2 round 2 H1 and L1: contractions, leading or trailing punctuation, underscore.
func TestWordBoundariesRound2(t *testing.T) {
	cases := []struct {
		text, quote string
		want        bool
	}{
		{"Parents say strangers can't message my kid at all.", "Parents say strangers can", false},
		{"Parents say strangers can’t message my kid at all.", "Parents say strangers can", false},
		{"Parents say strangers can’t message my kid at all.", "Parents say strangers can't", true},
		{"Parents say strangers can't message my kid at all.", "say strangers can’t message", true},
		{"Parents say strangers can message my kid at all.", "Parents say strangers can", true},
		{"The chat isn't filtered at all", "The chat isn", false},
		{"The chat isn’t filtered at all", "t filtered at all", false},
		{"Kids don't see ads here", "Kids don'", false},
		{"This is un-safe for young kids", "-safe for young kids", false},
		{"This is filter_safe for young kids", "safe for young kids", false},
		{"This is filter_safe for young kids", "filter_safe for young kids", true},
		{"She said: \"chat is open to everyone\" today", "\"chat is open to everyone\"", true},
		{"(chat is open to everyone) today", "chat is open to everyone", true},
		{"the kids' chat is open to anyone", "the kids' chat is open", true},
	}
	for _, c := range cases {
		s := map[string]Segment{"D1": {Text: c.text, Source: Source{URL: "u"}}}
		fs, _ := Verify([]Raw{{Ref: "D1", Area: "contact", Stance: "risk", Quote: c.quote}}, s)
		if (len(fs) == 1) != c.want {
			t.Errorf("%q in %q: verified=%v, want %v", c.quote, c.text, len(fs) == 1, c.want)
		}
	}
}

// T1.2 round 3: hyphens join words; punctuation beside punctuation is a fine
// cut. One broad table so boundary rules are pinned together.
func TestWordBoundaryTable(t *testing.T) {
	cases := []struct {
		text, quote string
		want        bool
	}{
		// hyphen joins (H1)
		{"This chat is un-safe for young kids.", "safe for young kids", false},
		{"This chat is un-safe for young kids.", "-safe for young kids", false},
		{"This chat is un-safe for young kids.", "un-safe for young kids", true},
		{"Only non-violent content for little kids", "violent content for little kids", false},
		{"Only non\u2011violent content for little kids", "violent content for little kids", false},
		{"Only non\u2011violent content for little kids", "non-violent content for little kids", true},
		{"Only non\u2013violent content for little kids", "violent content for little kids", false},
		{"the chat is safe - mostly for older kids", "mostly for older kids", true},
		{"the chat is safe - mostly for older kids", "the chat is safe", true},
		{"a well-known chat filter problem", "well-known chat filter", true},
		{"a well-known chat filter problem", "known chat filter problem", false},
		// invisible format characters and hyphen forms (round 4)
		{"This chat is un\u00adsafe for young kids.", "safe for young kids", false},
		{"This chat is un\u200bsafe for young kids.", "safe for young kids", false},
		{"This chat is un\u200csafe for young kids.", "safe for young kids", false},
		{"This chat is un\u2060safe for young kids.", "safe for young kids", false},
		{"This chat is un\u00adsafe for young kids.", "unsafe for young kids", true},
		{"\ufeffThis chat is unsafe", "This chat is unsafe", true},
		{"This chat is unsafe for young kids.", "un\u200bsafe for young kids", true},
		{"Only non\uff0dviolent content for little kids", "violent content for little kids", false},
		{"Only non\ufe63violent content for little kids", "violent content for little kids", false},
		{"Only non\u2010violent content for little kids", "violent content for little kids", false},
		{"Only non\u2012violent content for little kids", "violent content for little kids", false},
		{"Only non\u2212violent content for little kids", "violent content for little kids", false},
		{"Only non\u2043violent content for little kids", "violent content for little kids", false},
		{"This chat is un--safe for young kids.", "safe for young kids", false},
		{"This chat is un---safe for young kids.", "-safe for young kids", false},
		{"This chat is un--safe for young kids.", "un-safe for young kids", true},
		{"Only non\uff0dviolent content", "non-violent content", true},
		{"it is fine -- mostly for older kids", "mostly for older kids", true},
		// punctuation at the edges (M1)
		{"(the chat is totally filtered.)", "the chat is totally filtered.", true},
		{"She said \"the chat is totally filtered.\"", "the chat is totally filtered.", true},
		{"She said \u201cthe chat is totally filtered.\u201d", "\"the chat is totally filtered.\"", true},
		{"the chat is totally filtered...!", "the chat is totally filtered.", true},
		{"[the chat is totally filtered], she wrote", "[the chat is totally filtered]", true},
		{"filtered.chat is totally open", "chat is totally open", true},
		// earlier rules still hold
		{"This game is unsafe for young kids", "safe for young kids", false},
		{"Parents say strangers can\u2019t message my kid", "Parents say strangers can", false},
		{"Parents say strangers can't message my kid", "strangers can't message", true},
		{"This is filter_safe for young kids", "safe for young kids", false},
		{"100% safe for little kids", "00% safe for little kids", false},
		{"100% safe for little kids", "100% safe for little kids", true},
		{"it is unsafe for kids. it is safe for kids at home", "safe for kids at home", true},
		{"kids' chat is open to anyone", "kids' chat is open", true},
	}
	for _, c := range cases {
		s := map[string]Segment{"D1": {Text: c.text, Source: Source{URL: "u"}}}
		fs, _ := Verify([]Raw{{Ref: "D1", Area: "contact", Stance: "risk", Quote: c.quote}}, s)
		if (len(fs) == 1) != c.want {
			t.Errorf("%q in %q: verified=%v, want %v", c.quote, c.text, len(fs) == 1, c.want)
		}
	}
}

func TestNormalizeRound4(t *testing.T) {
	for in, want := range map[string]string{
		"un\u00adsafe":     "unsafe",
		"un\u200bsafe":     "unsafe",
		"a\u200db":         "ab",
		"\ufeffhi":         "hi",
		"non\uff0dviolent": "non-violent",
		"un--safe":         "un-safe",
		"a - - b":          "a - - b",
		"wait...":          "wait...",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}
