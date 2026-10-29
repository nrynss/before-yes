// Command eval runs the pipeline over the demo titles' fixtures and compares
// each card with a hand-written expectation under testdata/eval: per area,
// whether the grade agrees and whether the decisive quotes were found, and
// per title, the quote drop rate and the cost. It is offline: the App Store
// and Tavily are served from testdata/titles, and the models are either
// recorded replies (testdata/eval/replies, from T3.6b) or deterministic
// stand-ins.
//
// Usage, from the repository root:
//
//	go run ./tools/eval                  # replay where recorded, else stand-ins
//	go run ./tools/eval -models standin  # stand-ins only
//	go run ./tools/eval -models replay   # recorded replies only
//	go run ./tools/eval -models live     # real hosts, spend-capped, fixtures (live.go)
//	go run ./tools/eval -models live -gather 1 -live-only roblox
//	                                 # record one live gather, then replay it with -models replay -gather 1
//	go run ./tools/eval -json            # machine-readable
//
// The stand-ins are a keyword reader and a judge that reads stances. They
// are a floor for the harness, not a measure of Nano or Super. The user
// accepted them, and the hand-written replies in the internal/pipeline
// tests, as stand-ins until T3.6b records real replies under
// testdata/eval/replies. T3.6b must replace them, and the baseline then
// comes from the replay run, not from the stand-ins.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/nrynss/before-yes/internal/appstore"
	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/finding"
	"github.com/nrynss/before-yes/internal/pipeline"
	"github.com/nrynss/before-yes/internal/spend"
	"github.com/nrynss/before-yes/internal/tavily"
)

// Quote is one quote that matters, from a review ("R355") or a URL.
type Quote struct {
	Source string `json:"source"`
	Quote  string `json:"quote"`
}

// AreaWant is a careful parent's reading of one area.
type AreaWant struct {
	Area       checklist.Area  `json:"area"`
	Level      checklist.Level `json:"level"`
	Setting    string          `json:"setting,omitempty"`
	Note       string          `json:"note"`
	Decisive   []Quote         `json:"decisive"`
	Supporting []Quote         `json:"supporting,omitempty"`
}

// Expectation is one title and age, written by hand.
type Expectation struct {
	Title   string            `json:"title"`
	Slug    string            `json:"slug"`
	Age     int               `json:"age"`
	Verdict checklist.Verdict `json:"verdict"`
	Areas   []AreaWant        `json:"areas"`
}

// AreaResult compares one area.
type AreaResult struct {
	Area    checklist.Area  `json:"area"`
	Want    checklist.Level `json:"want"`
	Got     checklist.Level `json:"got"`
	Agree   bool            `json:"agree"`
	Found   []Quote         `json:"found"`
	Missed  []Quote         `json:"missed"`
	GotNote string          `json:"got_reason,omitempty"`
}

// Result compares one title and age.
type Result struct {
	Title        string                `json:"title"`
	Grades       []checklist.Grade     `json:"grades"`
	Gate         []pipeline.GateAction `json:"gate"`
	EvidenceHash string                `json:"evidence_sha256"`
	Age          int                   `json:"age"`
	Models       string                `json:"models"`
	WantVerdict  checklist.Verdict     `json:"want_verdict"`
	GotVerdict   checklist.Verdict     `json:"got_verdict"`
	Areas        []AreaResult          `json:"areas"`
	Agreed       int                   `json:"agreed"`
	DecisiveHit  int                   `json:"decisive_found"`
	DecisiveAll  int                   `json:"decisive_total"`
	Findings     int                   `json:"findings"`
	Drops        finding.Drops         `json:"drops"`
	DropRate     float64               `json:"drop_rate"`
	Chunks       int                   `json:"chunks"`
	FailedChunks int                   `json:"failed_chunks"`
	Missing      []string              `json:"missing_kinds"`
	Credits      int                   `json:"tavily_credits"`
	ReadNano     int64                 `json:"read_cost_nano"`
	JudgeNano    int64                 `json:"judge_cost_nano"`
	Err          string                `json:"error,omitempty"`

	// slug and card are the title's slug and the card Decide built. The demo
	// cards test reads them; they are not part of the report.
	slug string
	card pipeline.Card
}

func main() {
	root := flag.String("root", ".", "repository root")
	models := flag.String("models", "auto", "auto, standin or replay")
	asJSON := flag.Bool("json", false, "print JSON instead of tables")
	flag.Parse()
	results, err := evaluate(*root, *models)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(results)
	} else {
		report(os.Stdout, results)
	}
	for _, r := range results {
		if r.Err != "" {
			os.Exit(1)
		}
	}
}

func loadExpectations(dir string) ([]Expectation, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	var out []Expectation
	for _, n := range names {
		b, err := os.ReadFile(n)
		if err != nil {
			return nil, err
		}
		var e Expectation
		dec := json.NewDecoder(strings.NewReader(string(b)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&e); err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no expectations in %s", dir)
	}
	return out, nil
}

func evaluate(root, models string) ([]Result, error) {
	if *reasonCases != "" {
		return evaluateReasonCases(root, models)
	}
	if *gatherN < 0 || *gatherN > 2 {
		return nil, fmt.Errorf("gather: -gather must be 0, 1 or 2")
	}
	exps, err := loadExpectations(filepath.Join(root, "testdata", "eval"))
	if err != nil {
		return nil, err
	}
	var out []Result
	for _, e := range exps {
		if !liveWanted(e.Slug) {
			continue
		}
		out = append(out, runOne(root, models, e))
	}
	return out, nil
}

var evalNow = time.Date(2026, 10, 13, 19, 0, 0, 0, time.UTC)

func runOne(root, models string, e Expectation) (res Result) {
	// res is the named result so the deferred checks below (replay misses,
	// live refusals) change what the caller receives. With an unnamed result
	// the deferred assignment was lost and a replay with unrecorded calls
	// reported none.
	res = Result{Title: e.Title, Age: e.Age, WantVerdict: e.Verdict, slug: e.Slug}
	ctx := context.Background()
	var f *fixture
	var rec *gatherRecorder
	var srv *httptest.Server
	capDir := captureDir(root, e.Slug)
	if models == "live" && *gatherN > 0 && !*liveEntailOnly && !*reuseGather {
		var err error
		rec, err = newGatherRecorder(capDir, e.Title, itunesHost, tavilyHost)
		if err != nil {
			res.Err = err.Error()
			return res
		}
		srv = httptest.NewServer(rec)
	} else {
		if *gatherN > 0 {
			if st, err := os.Stat(capDir); err != nil || !st.IsDir() {
				res.Err = fmt.Sprintf("gather %d: no saved capture for %s (want %s)", *gatherN, e.Title, capDir)
				return res
			}
		}
		f, srv = serveFixture(capDir, e.Title)
	}
	defer srv.Close()

	tmp, err := os.MkdirTemp("", "before-yes-eval-")
	if err != nil {
		res.Err = err.Error()
		return res
	}
	defer os.RemoveAll(tmp)
	meter, err := spend.Open(ctx, spend.Config{Path: filepath.Join(tmp, "spend.db"), Pools: map[spend.Pool]spend.PoolConfig{
		spend.PoolModel: {Limit: 1_000_000_000_000}, spend.PoolTavily: {Limit: 1_000_000},
	}})
	if err != nil {
		res.Err = err.Error()
		return res
	}
	defer meter.Close()

	key := func() (string, error) { return "offline", nil }
	if rec != nil {
		key = func() (string, error) {
			v := os.Getenv("TAVILY_KEY")
			if v == "" {
				return "", fmt.Errorf("TAVILY_KEY is not set")
			}
			return v, nil
		}
	}
	tc := tavily.New(srv.URL, key)
	if rec == nil {
		// Fixture captures are local. A live gather keeps the client's own
		// pace and retries, because a refused call was not saved.
		tc.Retry.PerMinute = 0
		tc.Retry.Name = ""
		tc.Retry.Backoff = time.Millisecond
		tc.Retry.Max = time.Millisecond
		tc.Retry.Sleep = func(context.Context, time.Duration) error { return nil }
	}

	reader, judge, err := chatters(root, models, e.Slug)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	tr := &callTrace{}
	reader = &tracingChatter{inner: reader, role: "reader", tr: tr}
	judge = &tracingChatter{inner: judge, role: "judge", tr: tr}
	if models == "replay" {
		dir := replyDir(root, e.Slug, "")
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			res.Err = fmt.Sprintf("replay: no recordings for %s (want %s; record them with T3.6b)", e.Title, dir)
			return res
		}
	}
	defer func() {
		liveSummarise(e.Title, bare(reader), bare(judge))
		if rec != nil {
			fmt.Fprintf(os.Stderr, "gather %d %s: upstream calls %d\n", *gatherN, e.Title, rec.upstream.Load())
		}
		if res.Err != "" {
			return
		}
		if msg := replayMisses(bare(reader), bare(judge)); msg != "" {
			res.Err = msg
			return
		}
		res.Err = liveRefusals(bare(reader), bare(judge))
	}()
	res.Models = reader.Settings().Model + " + " + judge.Settings().Model
	fast, think := judgeSwitches(models)
	now := func() time.Time { return evalNow }
	p := &pipeline.Pipeline{
		Gatherer: &pipeline.Gatherer{Store: publicFeed{appstore.New(srv.URL, "us"), appstore.New("", "us")}, Search: tc, Meter: meter, Now: now},
		Reader:   &pipeline.Reader{Model: reader, Meter: meter},
		// Entailment runs on the judge model, thinking off. The reader model
		// accepted quotes the area definitions exclude. The server wires
		// the same judge client in cmd/before-yes/build.go.
		Judge: &pipeline.Judge{CheckedReasons: tr.captureCheckedReasons, Model: judge, Meter: meter, Fast: fast, Think: think, Entail: entailChatter(judge)},
		Now:   now,
	}
	ev, _, err := p.Collect(ctx, e.Title, "eval-"+e.Slug)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	card, err := p.Decide(ctx, ev, e.Age, "eval-"+e.Slug)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	var errs []string
	if f != nil {
		errs = append(errs, f.errs...)
	}
	if rec != nil {
		errs = append(errs, rec.errors()...)
		rec.saved.mu.Lock()
		errs = append(errs, rec.saved.errs...)
		rec.saved.mu.Unlock()
	}
	if len(errs) > 0 {
		res.Err = "fixture server: " + strings.Join(errs, "; ")
		return res
	}
	compare(&res, e, card, ev)
	if *diagnoseOn {
		var buf strings.Builder
		diagnose(&buf, card, ev, tr)
		fmt.Fprint(os.Stderr, buf.String())
		// Only a fresh live recording writes the gather's diagnosis. A replay
		// and a judge-only trial leave the tracked file alone.
		if *gatherN > 0 && models == "live" && !*reuseGather {
			_ = os.WriteFile(filepath.Join(capDir, "diagnosis.txt"), []byte(buf.String()), 0o644)
		}
	}
	return res
}

// publicFeed keeps review sources on Apple's public host, as a live check
// cites them, while the reads go to the fixture server.
type publicFeed struct {
	*appstore.Client
	public *appstore.Client
}

func (s publicFeed) FeedURL(id int64, page int) string { return s.public.FeedURL(id, page) }

// Storefront is the fixture client's country, for the evidence key.
func (s publicFeed) Storefront() string { return s.Client.Country }

// entailChatter is the judge model. The reader model accepted quotes the
// area definitions exclude, so the check does not run on it.
func entailChatter(judge pipeline.Chatter) pipeline.Chatter {
	return judge
}

func chatters(root, models, slug string) (pipeline.Chatter, pipeline.Chatter, error) {
	if models == "live" {
		r, j := liveChatters(root, slug)
		return r, j, nil
	}
	dir := replyDir(root, slug, "")
	recorded := false
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		recorded = true
	}
	if models == "replay" || (models == "auto" && recorded) {
		rm, err := recordedModel(dir, "reader")
		if err != nil {
			return nil, nil, err
		}
		jm, err := recordedModel(dir, "judge")
		if err != nil {
			return nil, nil, err
		}
		return &replayer{dir: dir, role: "reader", model: rm}, &replayer{dir: dir, role: "judge", model: jm}, nil
	}
	return keywordReader{}, stanceJudge{}, nil
}

// replayMisses says how many replayed calls had no recording, or "" when
// none missed or the models are not replayers. A miss would otherwise show
// only as a failed chunk or a quietly worse score.
func replayMisses(cs ...pipeline.Chatter) string {
	var parts []string
	for _, c := range cs {
		if r, ok := c.(*replayer); ok && r.misses.Load() > 0 {
			parts = append(parts, fmt.Sprintf("%s %d of %d calls", r.role, r.misses.Load(), r.misses.Load()+r.hits.Load()))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "replay: no recording for " + strings.Join(parts, ", ")
}

// quoteCover is the share of a decisive quote's words a finding's quote
// must carry, when it quotes only a stretch of it, to count as finding it.
const quoteCover = 2.0 / 3

// compare fills res from the card and the evidence. A decisive quote counts
// as found when a verified finding in its area, from the same review or
// URL, quotes all of it, or a stretch of it holding at least quoteCover of
// its words, after normalizing both. The evidence is
// searched, not only the card, so a quote the reader found but the judge did
// not cite still counts as found by the reader.
func compare(res *Result, e Expectation, card pipeline.Card, ev pipeline.Evidence) {
	res.GotVerdict = card.Verdict
	res.card = card
	res.Grades = card.Grades
	res.Gate = card.Gate
	evidenceJSON, _ := json.Marshal(ev.Findings)
	res.EvidenceHash = fmt.Sprintf("%x", sha256.Sum256(evidenceJSON))
	got := map[checklist.Area]checklist.Grade{}
	for _, g := range card.Grades {
		got[g.Area] = g
	}
	for _, w := range e.Areas {
		g := got[w.Area]
		ar := AreaResult{Area: w.Area, Want: w.Level, Got: g.Level, Agree: g.Level == w.Level, GotNote: g.Reason, Found: []Quote{}, Missed: []Quote{}}
		for _, q := range w.Decisive {
			if foundQuote(ev.Findings, w.Area, q) {
				ar.Found = append(ar.Found, q)
			} else {
				ar.Missed = append(ar.Missed, q)
			}
		}
		if ar.Agree {
			res.Agreed++
		}
		res.DecisiveAll += len(w.Decisive)
		res.DecisiveHit += len(ar.Found)
		res.Areas = append(res.Areas, ar)
	}
	st := ev.Stats
	res.Findings = len(ev.Findings)
	res.Drops = st.Drops
	dropped := st.Drops.BadArea + st.Drops.BadStance + st.Drops.NoQuote + st.Drops.Unquoted + st.Drops.Duplicates
	if total := dropped + res.Findings; total > 0 {
		res.DropRate = float64(dropped) / float64(total)
	}
	res.Chunks, res.FailedChunks = st.Chunks, st.FailedChunks
	res.Missing = st.MissingKinds
	res.Credits = card.Stats.TavilyCredits
	res.ReadNano, res.JudgeNano = card.Stats.ReadCostNano, card.Stats.JudgeCostNano
}

func sameSource(src finding.Source, want string) bool {
	if strings.HasPrefix(want, "R") && src.Kind == pipeline.KindReview {
		return fmt.Sprintf("R%d", src.Index) == want
	}
	return src.URL == want
}

func foundQuote(fs []finding.Finding, area checklist.Area, q Quote) bool {
	want := finding.Normalize(q.Quote)
	for _, f := range fs {
		if f.Area != area || !sameSource(f.Source, q.Source) {
			continue
		}
		got := finding.Normalize(f.Quote)
		if strings.Contains(got, want) {
			return true
		}
		if len(got) >= finding.MinQuote && strings.Contains(want, got) &&
			float64(len(strings.Fields(got))) >= quoteCover*float64(len(strings.Fields(want))) {
			return true
		}
	}
	return false
}

func report(w io.Writer, rs []Result) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	totalAgree, totalAreas, totalHit, totalDec := 0, 0, 0, 0
	for _, r := range rs {
		fmt.Fprintf(tw, "\n%s, age %d (%s)\n", r.Title, r.Age, r.Models)
		if r.Err != "" {
			fmt.Fprintf(tw, "  ERROR: %s\n", r.Err)
			continue
		}
		fmt.Fprintf(tw, "  verdict\twant %s\tgot %s\t%s\n", r.WantVerdict, r.GotVerdict, mark(r.WantVerdict == r.GotVerdict))
		fmt.Fprintln(tw, "  area\twant\tgot\tagree\tdecisive found\tmissed")
		for _, a := range r.Areas {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%d/%d\t%s\n", a.Area, a.Want, a.Got, mark(a.Agree), len(a.Found), len(a.Found)+len(a.Missed), missedList(a.Missed))
		}
		fmt.Fprintf(tw, "  grades agree %d/%d; decisive quotes found %d/%d\n", r.Agreed, len(r.Areas), r.DecisiveHit, r.DecisiveAll)
		fmt.Fprintf(tw, "  findings %d; dropped %d (rate %.1f%%: bad_area %d, bad_stance %d, no_quote %d, unquoted %d, duplicates %d)\n",
			r.Findings, r.Drops.BadArea+r.Drops.BadStance+r.Drops.NoQuote+r.Drops.Unquoted+r.Drops.Duplicates, 100*r.DropRate,
			r.Drops.BadArea, r.Drops.BadStance, r.Drops.NoQuote, r.Drops.Unquoted, r.Drops.Duplicates)
		fmt.Fprintf(tw, "  chunks %d (failed %d); missing sources %v\n", r.Chunks, r.FailedChunks, r.Missing)
		fmt.Fprintf(tw, "  cost: Tavily %d credits; read $%.6f; judge $%.6f\n", r.Credits, float64(r.ReadNano)/1e9, float64(r.JudgeNano)/1e9)
		totalAgree += r.Agreed
		totalAreas += len(r.Areas)
		totalHit += r.DecisiveHit
		totalDec += r.DecisiveAll
	}
	fmt.Fprintf(tw, "\nTOTAL grades agree %d/%d; decisive quotes found %d/%d\n", totalAgree, totalAreas, totalHit, totalDec)
	_ = tw.Flush()
}

func mark(ok bool) string {
	if ok {
		return "yes"
	}
	return "NO"
}

func missedList(qs []Quote) string {
	var s []string
	for _, q := range qs {
		s = append(s, q.Source)
	}
	return strings.Join(s, ", ")
}
