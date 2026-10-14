package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/nrynss/keel/cost"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/finding"
	"github.com/nrynss/before-yes/internal/model"
	"github.com/nrynss/before-yes/internal/spend"
)

// recorder stands in for Nano with recorded replies: for each segment ref a
// chunk carries, it answers the findings recorded for that ref, as one JSON
// object the way Nano replies. Extra adds findings to the chunk that holds a
// given ref, which is how a test plants a reply that cites a ref outside
// its chunk.
type recorder struct {
	byRef map[string][]finding.Raw
	extra map[string][]finding.Raw
	price cost.Price
	fail  func(user string) error

	calls    atomic.Int64
	inFlight atomic.Int64
	maxIn    atomic.Int64
	mu       sync.Mutex
	users    []string
}

var segRef = regexp.MustCompile(`(?m)^\[([RD]\d+)\] \(`)

func (r *recorder) Chat(ctx context.Context, msgs []model.Message, o model.Options) (model.Reply, error) {
	n := r.inFlight.Add(1)
	defer r.inFlight.Add(-1)
	for {
		m := r.maxIn.Load()
		if n <= m || r.maxIn.CompareAndSwap(m, n) {
			break
		}
	}
	r.calls.Add(1)
	user := msgs[len(msgs)-1].Content
	r.mu.Lock()
	r.users = append(r.users, user)
	r.mu.Unlock()
	if r.fail != nil {
		if err := r.fail(user); err != nil {
			return model.Reply{}, err
		}
	}
	var out []finding.Raw
	for _, m := range segRef.FindAllStringSubmatch(user, -1) {
		out = append(out, r.byRef[m[1]]...)
		out = append(out, r.extra[m[1]]...)
	}
	if out == nil {
		out = []finding.Raw{}
	}
	b, _ := json.Marshal(map[string]any{"findings": out})
	return model.Reply{Text: string(b), InputTokens: len(user) / 3, OutputTokens: len(b) / 3, Price: r.price, Measured: true}, nil
}

func (r *recorder) Estimate([]model.Message, model.Options) cost.Price { return 2 * r.price }
func (r *recorder) Settings() model.Settings {
	return model.Settings{Provider: "recorded", Model: "nano-recorded"}
}

func refOf(t *testing.T, docs []Doc, kind, url string) string {
	t.Helper()
	for i, d := range docs {
		if d.Source.Kind == kind && (url == "" || d.Source.URL == url) {
			return refFor(d, i)
		}
	}
	t.Fatalf("no %s doc %s", kind, url)
	return ""
}

// TestReadRobloxRecorded runs the reader over the Roblox fixture documents
// with recorded replies, and checks the review that decides the card
// survives while every invented quote is dropped by its reason.
func TestReadRobloxRecorded(t *testing.T) {
	g, _, _ := gatherFixture(t, demos[0])
	terms := refOf(t, g.Docs, KindTerms, "")
	rec := &recorder{
		price: 5000,
		byRef: map[string][]finding.Raw{
			"R355": {
				{Ref: "R355", Area: "contact", Stance: "risk", Claim: "A parent reports that a child predator may ask a child where they are",
					Quote: "if your kid plays Roblox high chance a child predator might ask where you are"},
				// Invented: the words are not in the review.
				{Ref: "R355", Area: "contact", Stance: "risk", Claim: "A stranger asked for photos",
					Quote: "a stranger asked my daughter to send photos of herself"},
				// Repeated in the same reply.
				{Ref: "[R355]", Area: "contact", Stance: "risk", Claim: "A parent reports predators",
					Quote: "if your kid plays Roblox high chance a child predator might ask where you are"},
			},
			"R242": {{Ref: "R242", Area: "contact", Stance: "risk", Claim: "A player reports moderation does not stop predators",
				Quote: "they can’t stop the child predators and E-Daters"}},
			"R1": {
				{Ref: "R1", Area: "money", Stance: "risk", Claim: "A child reports a Robux purchase that did not arrive in full",
					Quote: "I got Robux and I got only 246 Robux instead of 1800"},
				{Ref: "R1", Area: "graphics", Stance: "risk", Claim: "Not an area", Quote: "I got Robux and I got only 246 Robux instead of 1800"},
				{Ref: "R1", Area: "money", Stance: "worry", Claim: "Not a stance", Quote: "I got Robux and I got only 246 Robux instead of 1800"},
				{Ref: "R1", Area: "money", Stance: "risk", Claim: "Too short", Quote: "Robux"},
			},
		},
		extra: map[string][]finding.Raw{
			// A real quote from the terms, cited from a chunk that does not
			// hold the terms, and a ref no document has.
			"R355": {
				{Ref: terms, Area: "data", Stance: "neutral", Claim: "Terms are being updated", Quote: "We are updating our Terms of Use to make them clearer"},
				{Ref: "R9999", Area: "contact", Stance: "risk", Claim: "No such review", Quote: "a review that the feed never served to anyone"},
			},
		},
	}
	m := openTestMeter(t)
	r := &Reader{Model: rec, Meter: m, Concurrency: 3}
	got, err := r.Read(context.Background(), "Roblox", "check-read", g.Docs)
	if err != nil {
		t.Fatal(err)
	}
	if got.Failed != 0 || got.Chunks == 0 || int64(got.Chunks) != rec.calls.Load() {
		t.Fatalf("chunks %d, failed %d, calls %d", got.Chunks, got.Failed, rec.calls.Load())
	}
	if rec.maxIn.Load() > 3 {
		t.Errorf("%d calls in flight, over the bound of 3", rec.maxIn.Load())
	}
	want := finding.Drops{BadArea: 1, BadStance: 1, NoQuote: 1, Unquoted: 3, Duplicates: 1}
	if got.Drops != want {
		t.Errorf("drops = %+v, want %+v", got.Drops, want)
	}
	if len(got.Findings) != 3 {
		t.Fatalf("findings = %d, want 3: %+v", len(got.Findings), got.Findings)
	}
	var decisive *finding.Finding
	for i, f := range got.Findings {
		if f.Source.Index == 355 {
			decisive = &got.Findings[i]
		}
	}
	if decisive == nil || decisive.Stance != finding.Risk || decisive.Area != checklist.Contact || decisive.Source.Kind != KindReview {
		t.Fatalf("review 355 did not survive as a contact risk: %+v", decisive)
	}
	if !strings.HasPrefix(decisive.Source.URL, "https://itunes.apple.com/") {
		t.Errorf("review 355 cites %s", decisive.Source.URL)
	}
	spent, err := m.Spent(context.Background(), spend.PoolModel, "check-read")
	if err != nil {
		t.Fatal(err)
	}
	if got.Cost != spent || got.Cost != cost.Price(got.Chunks)*rec.price {
		t.Errorf("cost %d, meter %d, want %d", got.Cost, spent, cost.Price(got.Chunks)*rec.price)
	}
	// Thinking stays off: the reader asks for JSON and adds no switch.
	for _, u := range rec.users {
		if !strings.HasPrefix(u, "Title: Roblox\n\n[") {
			t.Fatalf("user prompt starts %.40q", u)
		}
	}
}

func TestReadSkipsFailedChunks(t *testing.T) {
	docs := []Doc{
		{Source: finding.Source{URL: "https://example.com/a", Kind: KindForum}, Text: strings.Repeat("Chat with strangers is on by default for every account. ", 400)},
		{Source: finding.Source{URL: "https://example.com/b", Kind: KindForum}, Text: "Parents can turn off chat with strangers in the settings menu for younger kids."},
	}
	rec := &recorder{
		price: 7,
		byRef: map[string][]finding.Raw{"D2": {{Ref: "D2", Area: "contact", Stance: "mitigation", Claim: "Chat can be turned off",
			Quote: "Parents can turn off chat with strangers in the settings menu"}}},
		fail: func(user string) error {
			if strings.Contains(user, "[D1]") {
				return errors.New("rate limited")
			}
			return nil
		},
	}
	r := &Reader{Model: rec, Meter: openTestMeter(t)}
	got, err := r.Read(context.Background(), "X", "c", docs)
	if err != nil {
		t.Fatal(err)
	}
	if got.Failed == 0 || got.Failed != got.Chunks-1 || len(got.Findings) != 1 {
		t.Errorf("failed %d of %d, findings %d", got.Failed, got.Chunks, len(got.Findings))
	}
	if got.Cost != 7 {
		t.Errorf("cost %d: a failed call books nothing", got.Cost)
	}

	rec.fail = func(string) error { return errors.New("down") }
	if _, err := r.Read(context.Background(), "X", "c2", docs); err == nil {
		t.Error("every chunk failed but Read did not")
	}
}

func TestReadSkipsUnparsableReply(t *testing.T) {
	docs := []Doc{{Source: finding.Source{URL: "https://example.com/a", Kind: KindForum}, Text: "Some text here about the app."}}
	r := &Reader{Model: badReply{}, Meter: openTestMeter(t)}
	if _, err := r.Read(context.Background(), "X", "c", docs); err == nil {
		t.Error("the only chunk's reply did not parse, but Read did not fail")
	}
}

type badReply struct{}

func (badReply) Chat(context.Context, []model.Message, model.Options) (model.Reply, error) {
	return model.Reply{Text: "I could not find anything.", Price: 1, Measured: true}, nil
}
func (badReply) Estimate([]model.Message, model.Options) cost.Price { return 1 }
func (badReply) Settings() model.Settings                           { return model.Settings{Provider: "bad"} }

func TestChunkDocsReviewsShare(t *testing.T) {
	var docs []Doc
	for i := 1; i <= 100; i++ {
		docs = append(docs, Doc{Source: finding.Source{Kind: KindReview, Index: i}, Text: strings.Repeat("word ", 60)})
	}
	cs := chunkDocs(docs)
	if len(cs) >= 100 || len(cs) < 2 {
		t.Fatalf("%d chunks for 100 short reviews", len(cs))
	}
	seen := 0
	for _, c := range cs {
		if c.size() > chunkChars {
			t.Errorf("chunk of %d bytes", c.size())
		}
		for _, s := range c.Segments {
			seen++
			if s.Ref != refFor(docs[s.Doc], s.Doc) || !strings.HasPrefix(s.Ref, "R") {
				t.Errorf("segment ref %s", s.Ref)
			}
		}
	}
	if seen != 100 {
		t.Errorf("%d segments, want 100", seen)
	}
}

func TestChunkDocsSplitsLongDoc(t *testing.T) {
	// Multibyte text with no spaces in places, so a cut must find a rune
	// boundary on its own.
	text := strings.Repeat("Les données des enfants sont protégées. ", 300) + strings.Repeat("é", 20000) + "\n\nfin."
	docs := []Doc{{Source: finding.Source{Kind: KindPolicy, URL: "https://example.com/p"}, Text: text}}
	cs := chunkDocs(docs)
	if len(cs) < 3 {
		t.Fatalf("%d chunks", len(cs))
	}
	for i, c := range cs {
		if len(c.Segments) != 1 || c.Segments[0].Ref != "D1" {
			t.Fatalf("chunk %d segments %+v", i, len(c.Segments))
		}
		p := c.Segments[0].Text
		if len(p) > chunkChars || !utf8.ValidString(p) || !strings.Contains(text, p) {
			t.Errorf("piece %d: %d bytes, valid %v", i, len(p), utf8.ValidString(p))
		}
		if i > 0 {
			prev := cs[i-1].Segments[0].Text
			tail := prev[len(prev)-50:]
			if !strings.Contains(p, tail) && !strings.Contains(prev, p[:50]) {
				t.Errorf("piece %d does not overlap the one before", i)
			}
		}
	}
	if !strings.HasSuffix(cs[len(cs)-1].Segments[0].Text, "fin.") {
		t.Error("the end of the document is lost")
	}
}

func TestReaderSystemNamesEveryArea(t *testing.T) {
	s := readerSystem()
	for _, a := range checklist.Areas() {
		if !strings.Contains(s, "- "+string(a.ID)+": ") {
			t.Errorf("prompt misses area %s", a.ID)
		}
	}
	for _, w := range []string{"10 to 40 words", "No outside knowledge", "bug reports", "first-hand"} {
		if !strings.Contains(s, w) {
			t.Errorf("prompt misses %q", w)
		}
	}
}
