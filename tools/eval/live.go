package main

// The live runs of T3.6b. With -models live, every reader and judge call goes
// to a real host instead of a recording or a stand-in. Each measured reply is
// saved as a Recording under testdata/eval/replies, the layout -models replay
// reads, so a live run and its replay print the same numbers.
//
// Nothing here calls a host unless -models live is set, and the gate never
// sets it. Keys are read from the environment at each call and never printed.
// The run is held to a spend cap by a ledger file kept outside the repository:
// a call is refused when its reservation would take the ledger past the cap.
//
// Run it from a shell that has exported the key variables, for example
//
//	go run ./tools/eval -models live -live-set tokenfactory -live-only roblox \
//	  -live-ledger "$SCRATCH/ledger.json"
//
// The hosts' settings are the ones before-yes.toml.example documents for each
// set, written here rather than read from a settings file, so the run does not
// depend on a file that the gate never sees.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/nrynss/keel/cost"

	"github.com/nrynss/before-yes/internal/model"
	"github.com/nrynss/before-yes/internal/pipeline"
)

var (
	liveSet        = flag.String("live-set", "tokenfactory", "with -models live: tokenfactory, also named ultra (Nano reads and Nemotron-3-Ultra judges, on Token Factory), or openrouter (the free stand-ins)")
	liveOnly       = flag.String("live-only", "", "with -models live: run only the title with this slug")
	liveCapUSD     = flag.Float64("live-cap-usd", 1.5, "with -models live: refuse a call that would take the ledger past this many dollars")
	liveLedger     = flag.String("live-ledger", "", "with -models live: file that holds the running spend in nanodollars, kept outside the repository")
	liveEntailOnly = flag.Bool("live-entail-only", false, "with -models live: refuse any uncached call except entailment")
	liveDry        = flag.Bool("live-dry", false, "with -models live: make no call; report what each title's calls would reserve, at most")
	// liveJudgeOut is the trial switch: with -reuse-gather and a live set, the
	// reader replays its saved replies from the gather and only the judge calls
	// the host, recording into this directory (one subdirectory per title). The
	// gather's own replies are never written.
	liveJudgeOut = flag.String("live-judge-out", "", "with -models live and -reuse-gather: the reader replays its saved replies; the judge calls the host and records under this directory, per title")
	// gatherN selects a saved live gather. 0 is the frozen testdata/titles
	// fixtures. 1 and 2 are the two live gathers under testdata/eval/gathers,
	// recorded when -models live is set and replayed with no network otherwise.
	reuseGather = flag.Bool("reuse-gather", false, "with live models, require saved gather fixtures and make no upstream gather calls")
	gatherN     = flag.Int("gather", 0, "0 uses testdata/titles. 1 or 2 records or replays testdata/eval/gathers/<n>, beside the fixture recordings")
	diagnoseOn  = flag.Bool("diagnose", false, "print each area's grade, cited quote and the check that acted")
)

// errLiveDry is the reply a dry judge call gives: the run stops there, and
// the reservation it would have made is already counted.
var errLiveDry = errors.New("live: dry run, no call made")

// liveSetting is one set of hosts: the reader and judge settings, the key
// variable both read, the judge's thinking switches for its two passes, and
// the recordings subdirectory.
type liveSetting struct {
	reader, judge model.Settings
	keyVar        string
	fast, think   map[string]any
	subdir        string
}

// The Token Factory set is the one the demo runs on: Nano reads, and
// Nemotron-3-Ultra-550b-a55b judges. The judge's price is not on the host's
// model page, which renders empty; the third-party listings (Requesty,
// TypingMind, LLM Gateway) give $1.00 in and $3.00 out per million tokens, the
// highest figure seen, so the meter errs safe. The thinking switch was
// confirmed live: enable_thinking false gave 3 output tokens, true gave 18.
// The judge's max tokens is a runaway guard on a 1M-token-context model, not a
// tuning knob. The switches are the ones before-yes.toml.example names:
// chat_template_kwargs turns thinking off for Nano and for the judge's first
// pass, and on for the conflict pass. "ultra" is kept as another name for this
// set, for the commands that named it during the trial.
var liveSets = map[string]liveSetting{
	"tokenfactory": {
		reader: model.Settings{
			Provider: "tokenfactory", BaseURL: "https://api.tokenfactory.nebius.com/v1",
			Model: "nvidia/NVIDIA-Nemotron-3-Nano-30B-A3B", InputPerM: 0.06, OutputPerM: 0.24,
			MaxTokens: 8192, PerMinute: 100,
			Extra: map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": false}},
		},
		judge: model.Settings{
			Provider: "tokenfactory", BaseURL: "https://api.tokenfactory.nebius.com/v1",
			Model: "nvidia/Nemotron-3-Ultra-550b-a55b", InputPerM: 1.00, OutputPerM: 3.00,
			MaxTokens: 16000, PerMinute: 60,
		},
		keyVar: "NEBIUS_KEY",
		fast:   map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": false}},
		think:  map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": true}},
	},
	// The free OpenRouter stand-ins. Neither is Nano or Super. The switch is
	// reasoning.enabled, which this host reads and chat_template_kwargs ignores.
	"openrouter": {
		reader: model.Settings{
			Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1",
			Model: "nvidia/nemotron-3.5-lightning:free", MaxTokens: 4096, PerMinute: 20,
			Extra: map[string]any{"reasoning": map[string]any{"enabled": false}},
		},
		judge: model.Settings{
			Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1",
			Model: "nvidia/nemotron-3-super-120b-a12b:free", MaxTokens: 4096, PerMinute: 20,
		},
		keyVar: "OPENROUTER_KEY",
		fast:   map[string]any{"reasoning": map[string]any{"enabled": false}},
		think:  map[string]any{"reasoning": map[string]any{"enabled": true}},
		subdir: "openrouter-free",
	},
}

func init() { liveSets["ultra"] = liveSets["tokenfactory"] }

// conflictMaxTokens is the cap the judge sets on its conflict pass, the
// second call per check. A call with this cap is counted as a conflict call.
const conflictMaxTokens = pipeline.DefaultThinkMaxTokens

// liveWanted reports whether -live-only lets a title run.
func liveWanted(slug string) bool {
	return *liveOnly == "" || *liveOnly == slug
}

// judgeSwitches are the judge's thinking switches, for the pipeline's Judge
// to merge into its calls. Outside a live run they are the Token Factory ones,
// so the replay keys match the live recordings made on that host.
func judgeSwitches(models string) (fast, think map[string]any) {
	set := liveSets["tokenfactory"]
	if models == "live" {
		if s, ok := liveSets[*liveSet]; ok {
			set = s
		}
	}
	return set.fast, set.think
}

// liveChatters returns the reader and judge for one title. A bad -live-set
// or missing client returns chatters whose every call fails with the reason.
func liveChatters(root, slug string) (pipeline.Chatter, pipeline.Chatter) {
	set, ok := liveSets[*liveSet]
	if !ok {
		err := fmt.Errorf("live: unknown -live-set %q", *liveSet)
		return &liveChatter{role: "reader", err: err}, &liveChatter{role: "judge", err: err}
	}
	dir := replyDir(root, slug, set.subdir)
	key := func() (string, error) {
		v := os.Getenv(set.keyVar)
		if v == "" {
			return "", fmt.Errorf("%s is not set", set.keyVar)
		}
		return v, nil
	}
	rc, err := model.New(set.reader, key)
	if err != nil {
		return &liveChatter{role: "reader", err: err}, &liveChatter{role: "judge", err: err}
	}
	jc, err := model.New(set.judge, key)
	if err != nil {
		return &liveChatter{role: "reader", err: err}, &liveChatter{role: "judge", err: err}
	}
	if *liveJudgeOut != "" {
		// Judge-only trial: the reader is the saved gather's reply set, replayed
		// with no call, and every judge call goes live into its own directory,
		// so no recording the current judge made is ever read back as Super's.
		return &replayer{dir: dir, role: "reader", model: set.reader.Model},
			&liveChatter{role: "judge", c: jc, dir: filepath.Join(*liveJudgeOut, slug)}
	}
	return &liveChatter{role: "reader", c: rc, dir: dir}, &liveChatter{role: "judge", c: jc, dir: dir}
}

// liveChatter is one role's live client: it answers from a recording when one
// exists, and otherwise calls the host under the spend cap and records the
// reply. It counts what it did for the per-title summary.
type liveChatter struct {
	role string
	c    *model.Client
	dir  string
	err  error

	calls, cached, refused, failed, conflict, capped atomic.Int64
	dry                                              atomic.Int64

	mu      sync.Mutex
	in, out int64
	booked  cost.Price
	dryNano cost.Price
}

func (l *liveChatter) Settings() model.Settings {
	if l.c == nil {
		return model.Settings{Provider: "live", Model: "unavailable"}
	}
	return l.c.Settings()
}

func (l *liveChatter) Estimate(msgs []model.Message, o model.Options) cost.Price {
	if l.c == nil {
		return 0
	}
	return l.c.Estimate(msgs, o)
}

func (l *liveChatter) Chat(ctx context.Context, msgs []model.Message, o model.Options) (model.Reply, error) {
	if l.err != nil {
		return model.Reply{}, l.err
	}
	key := ReplyKey(l.role, msgs, o)
	path := filepath.Join(l.dir, l.role, key+".json")
	if rec, ok := readRecording(path); ok {
		l.cached.Add(1)
		l.count(rec.InputTokens, rec.OutputTokens)
		return model.Reply{Text: rec.Text, InputTokens: rec.InputTokens, OutputTokens: rec.OutputTokens, Price: rec.PriceNano, Measured: rec.Measured, FinishReason: rec.FinishReason}, nil
	}
	if *liveEntailOnly && (l.role != "judge" || len(msgs) == 0 || !strings.HasPrefix(msgs[0].Content, "You check one grade of a parent checklist") && !strings.HasPrefix(msgs[0].Content, "Classify the observed event in one source quote about age checks.")) {
		return model.Reply{}, errors.New("live: entail-only refuses uncached reader or grade call")
	}
	est := l.c.Estimate(msgs, o)
	if *liveDry {
		// A dry reader call answers with no findings, so the title reaches the
		// judge. A dry judge call counts its first pass and a conflict pass at
		// its cap, the most one check can reserve, and stops.
		l.dry.Add(1)
		l.mu.Lock()
		l.dryNano += est
		if l.role == "judge" {
			l.dryNano += l.c.Estimate(msgs, model.Options{MaxTokens: conflictMaxTokens})
		}
		l.mu.Unlock()
		if l.role == "reader" {
			return model.Reply{Text: `{"findings": []}`}, nil
		}
		return model.Reply{}, errLiveDry
	}
	if err := ledger.reserve(*liveLedger, est, cost.USD(*liveCapUSD)); err != nil {
		l.refused.Add(1)
		return model.Reply{}, err
	}
	l.calls.Add(1)
	if o.MaxTokens == conflictMaxTokens && l.role == "judge" {
		l.conflict.Add(1)
	}
	r, err := l.c.Chat(ctx, msgs, o)
	// A timed-out call comes back priced at its estimate, because the host
	// may have billed the tokens it wrote. Any other failure carries no price.
	if berr := ledger.settle(*liveLedger, est, r.Price); berr != nil {
		return model.Reply{}, berr
	}
	l.mu.Lock()
	l.booked += r.Price
	l.mu.Unlock()
	if err != nil {
		l.failed.Add(1)
		return r, err
	}
	rec := Recording{Text: r.Text, InputTokens: r.InputTokens, OutputTokens: r.OutputTokens,
		PriceNano: r.Price, Measured: r.Measured, Model: l.c.Settings().Model, FinishReason: r.FinishReason}
	maxTok := o.MaxTokens
	if maxTok <= 0 {
		maxTok = l.c.Settings().MaxTokens
	}
	if r.Capped(maxTok) {
		l.capped.Add(1)
		fmt.Fprintf(os.Stderr, "live %s: reply reached its %d-token cap (finish_reason %q, %d output tokens), recorded as is\n", l.role, maxTok, r.FinishReason, r.OutputTokens)
	}
	if err := writeRecording(l.dir, l.role, key, rec); err != nil {
		return model.Reply{}, fmt.Errorf("live: record reply: %w", err)
	}
	l.count(r.InputTokens, r.OutputTokens)
	return r, nil
}

func (l *liveChatter) count(in, out int) {
	l.mu.Lock()
	l.in += int64(in)
	l.out += int64(out)
	l.mu.Unlock()
}

// liveLedgerState is the spend across every live run, in nanodollars, kept in
// the file -live-ledger names. reserved is what calls in flight may still
// take, and it is held in memory only.
type liveLedgerState struct {
	mu       sync.Mutex
	spent    cost.Price
	reserved cost.Price
}

var ledger liveLedgerState

// reserve holds est against the cap, or refuses. It reads the file first, so
// the cap holds across processes.
func (s *liveLedgerState) reserve(path string, est cost.Price, cap cost.Price) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if path == "" {
		return errors.New("live: -live-ledger is required for a live run")
	}
	if err := s.load(path); err != nil {
		return err
	}
	if s.spent+s.reserved+est > cap {
		return fmt.Errorf("live: spend cap: %s spent and %s reserved, plus a %s call, would pass the cap of %s",
			usd(s.spent), usd(s.reserved), usd(est), usd(cap))
	}
	s.reserved += est
	return nil
}

// settle releases est and books what the call cost.
func (s *liveLedgerState) settle(path string, est, booked cost.Price) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reserved -= est
	s.spent += booked
	return s.save(path)
}

type ledgerFile struct {
	SpentNano cost.Price `json:"spent_nano"`
}

func (s *liveLedgerState) load(path string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		s.spent = 0
		return nil
	}
	if err != nil {
		return err
	}
	var f ledgerFile
	if err := json.Unmarshal(b, &f); err != nil {
		return fmt.Errorf("live: ledger %s: %w", path, err)
	}
	s.spent = f.SpentNano
	return nil
}

func (s *liveLedgerState) save(path string) error {
	b, err := json.Marshal(ledgerFile{SpentNano: s.spent})
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// usd formats a price in dollars for an error or a summary.
func usd(p cost.Price) string {
	return fmt.Sprintf("$%.6f", float64(p)/1e9)
}

func readRecording(path string) (Recording, bool) {
	var rec Recording
	b, err := os.ReadFile(path)
	if err != nil {
		return rec, false
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		return rec, false
	}
	return rec, true
}

func writeRecording(dir, role, key string, rec Recording) error {
	if err := os.MkdirAll(filepath.Join(dir, role), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, role, key+".json"), append(b, '\n'), 0o644)
}

// liveSummarise prints one title's live counts to stderr: calls made, calls
// answered from recordings, calls refused, tokens by role and what the host
// booked. It prints nothing outside a live run.
func liveSummarise(title string, cs ...pipeline.Chatter) {
	for _, c := range cs {
		l, ok := c.(*liveChatter)
		if !ok {
			continue
		}
		l.mu.Lock()
		in, out, booked, dry := l.in, l.out, l.booked, l.dryNano
		l.mu.Unlock()
		fmt.Fprintf(os.Stderr, "live %s %s: calls %d (conflict %d), recorded %d, refused %d, failed %d, capped %d; tokens in %d out %d; booked %s; dry reserve %s\n",
			title, l.role, l.calls.Load(), l.conflict.Load(), l.cached.Load(), l.refused.Load(), l.failed.Load(), l.capped.Load(),
			in, out, usd(booked), usd(dry))
	}
}

// liveRefusals says how many calls the spend cap refused, or "" when none did.
// A refused call leaves its chunk or check failed, so the title's card is not
// a full run and the run must fail.
func liveRefusals(cs ...pipeline.Chatter) string {
	n := int64(0)
	for _, c := range cs {
		if l, ok := c.(*liveChatter); ok {
			n += l.refused.Load()
		}
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("live: the spend cap refused %d calls; the run is incomplete", n)
}
