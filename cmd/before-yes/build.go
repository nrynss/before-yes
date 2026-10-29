package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	cachestore "github.com/nrynss/keel/cache/sqlitestore"
	"github.com/nrynss/keel/config"
	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/flag"
	flagstore "github.com/nrynss/keel/flag/sqlitestore"
	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/sqlite"
	"github.com/nrynss/keel/stream"

	"github.com/nrynss/before-yes/internal/appstore"
	"github.com/nrynss/before-yes/internal/cards"
	"github.com/nrynss/before-yes/internal/model"
	"github.com/nrynss/before-yes/internal/pipeline"
	"github.com/nrynss/before-yes/internal/server"
	"github.com/nrynss/before-yes/internal/spend"
	"github.com/nrynss/before-yes/internal/tavily"
	counts "github.com/nrynss/before-yes/internal/usage"
)

// dbName is the application's database file under data_dir: cards, jobs,
// the evidence cache and flags. spendName is the meter's own file for the
// three spend pools. The meter opens its own handle, and two handles
// writing one file lose writes to SQLITE_BUSY under concurrent checks
// (feedback.md), so the meter keeps a file to itself.
const (
	dbName    = "before-yes.db"
	spendName = "spend.db"
)

// reveal is a key reader over a secret. A secret the settings do not name
// reads as no key, so a keyless local stub host works; the clients refuse
// or the host answers 401.
func reveal(sec config.Secret) func() (string, error) {
	return func() (string, error) {
		v, err := sec.Reveal()
		if errors.Is(err, config.ErrUnresolved) {
			return "", nil
		}
		return v, err
	}
}

// core is what serve and check share: the pipeline and its meter.
type core struct {
	Pipeline *pipeline.Pipeline
	Meter    *spend.Meter
	DB       *sqlite.DB
	DBPath   string
	lock     *os.File
	// usageDB and recorder are set by NewServeApp only.
	usageDB  *sqlite.DB
	recorder *counts.Recorder
}

func (c *core) Close() {
	// The recorder drains first, while its database is still open.
	if c.recorder != nil {
		c.recorder.Close()
	}
	if c.usageDB != nil {
		_ = c.usageDB.Close()
	}
	if c.Meter != nil {
		_ = c.Meter.Close()
	}
	if c.DB != nil {
		_ = c.DB.Close()
	}
	if c.lock != nil {
		_ = c.lock.Close()
	}
}

// buildCore builds the pipeline the way tools/eval does, on real clients.
// Entailment is the judge client, and the entail call uses JudgeFast
// (thinking off), the same wiring as tools/eval. The reader is not that
// check (T3.11). Every paid call goes through one durable *spend.Meter,
// which is also the pipeline's Checks so fresh alternatives count against
// the daily cap (T3.5).
func buildCore(ctx context.Context, s Settings) (*core, error) {
	if err := os.MkdirAll(s.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	path := filepath.Join(s.DataDir, dbName)
	c := &core{DBPath: path}
	lock, err := lockMeter(filepath.Join(s.DataDir, spendName))
	if err != nil {
		return nil, err
	}
	c.lock = lock
	reader, err := model.New(s.Reader, reveal(s.Secrets.Reader))
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("reader: %w", err)
	}
	judge, err := model.New(s.Judge, reveal(s.Secrets.Judge))
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("judge: %w", err)
	}
	c.Meter, err = spend.Open(ctx, spend.Config{
		Path: filepath.Join(s.DataDir, spendName),
		Pools: map[spend.Pool]spend.PoolConfig{
			spend.PoolModel:       {Limit: cost.USD(s.Budget.ModelUSD)},
			spend.PoolTavily:      {Limit: cost.Price(s.Budget.TavilyCreditsMonthly * float64(spend.CreditUnit))},
			spend.PoolChecksDaily: {Limit: cost.Price(s.Budget.DailyFreshChecks)},
		},
	})
	if err != nil {
		c.Close()
		return nil, err
	}
	c.DB, err = sqlite.Open(ctx, sqlite.Config{Path: path, BusyTimeout: 5 * time.Second})
	if err != nil {
		c.Close()
		return nil, err
	}
	c.Pipeline = &pipeline.Pipeline{
		Gatherer: &pipeline.Gatherer{
			Store:       appstore.New("", s.Country),
			Search:      tavily.New("", reveal(s.Secrets.Tavily)),
			Meter:       c.Meter,
			ReviewPages: s.ReviewPages,
		},
		Reader: &pipeline.Reader{Model: reader, Meter: c.Meter},
		Judge: &pipeline.Judge{
			Model: judge, Meter: c.Meter,
			Fast: s.JudgeFast, Think: s.JudgeThink,
			Entail: judge,
		},
		Checks: c.Meter,
	}
	return c, nil
}

// ServeApp is everything serve runs.
type ServeApp struct {
	*core
	Server    *server.Server
	Handler   http.Handler
	WebServed bool
	// Flags is the live switch's store; an operator writes LiveFlag here.
	Flags *flagstore.Store
	Live  flag.Bool
}

// NewServeApp builds the serve path: the pipeline, the job runner, the
// card store and both caches over the one database file, the live flag
// store, and the gated handler. Server.Checks and Pipeline.Checks are the
// same *spend.Meter; Handler() refuses anything else (T4.5).
func NewServeApp(ctx context.Context, s Settings, log *slog.Logger) (*ServeApp, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	c, err := buildCore(ctx, s)
	if err != nil {
		return nil, err
	}
	a := &ServeApp{core: c}
	fail := func(err error) (*ServeApp, error) { c.Close(); return nil, err }

	broker := stream.New(stream.Config{})
	runner, err := server.NewRunner(c.DB, broker, 0, nil, log)
	if err != nil {
		return fail(err)
	}
	cs, err := cards.Open(ctx, c.DB)
	if err != nil {
		return fail(err)
	}
	es, err := cachestore.Open(ctx, cachestore.Config{DB: c.DB, Namespace: "evidence", Logger: log})
	if err != nil {
		return fail(err)
	}
	ev, err := server.NewEvidenceCache(server.EvidenceCacheConfig{Store: es, MaxAge: time.Duration(s.EvidenceMaxAgeHours) * time.Hour, Log: log})
	if err != nil {
		return fail(err)
	}
	// The usage counts sit in their own file, so their writes never share
	// a handle with the cards and jobs.
	c.usageDB, err = sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(s.DataDir, usageName), BusyTimeout: 5 * time.Second})
	if err != nil {
		return fail(err)
	}
	us, err := counts.Open(ctx, c.usageDB)
	if err != nil {
		return fail(err)
	}
	c.recorder = counts.NewRecorder(us, log)
	c.Pipeline.Cache = ev
	flags, err := flagstore.Open(ctx, flagstore.Config{DB: c.DB})
	if err != nil {
		return fail(err)
	}
	passcode, err := reveal(s.Gate.Passcode)()
	if err != nil {
		return fail(fmt.Errorf("gate passcode: %w", err))
	}
	live := server.LiveFlag(s.Live)
	a.Flags, a.Live = flags, live
	a.Server = &server.Server{
		Pipeline: c.Pipeline, Runner: runner, Broker: broker, Cards: cs,
		EvidenceCache: ev, CardMaxAge: time.Duration(s.CardMaxAgeHours) * time.Hour,
		Log: log, Checks: c.Meter, Usage: c.recorder,
	}
	api, err := a.Server.GatedHandler(server.GateConfig{Passcode: passcode, Flags: flags, Live: live, Log: log})
	if err != nil {
		return fail(err)
	}
	var web fs.FS
	if st, err := os.Stat(s.WebDir); err == nil && st.IsDir() {
		web = os.DirFS(s.WebDir)
		a.WebServed = true
	}
	a.Handler = server.App(api, &server.Status{Meter: c.Meter, Flags: flags, Live: live, Usage: c.recorder, Log: log}, web)
	return a, nil
}

// NewCheckApp builds the check path: the same pipeline and meter as serve.
func NewCheckApp(ctx context.Context, s Settings) (*core, error) {
	return buildCore(ctx, s)
}

// check runs one fresh check and prints the card. It reserves one check
// against the daily cap like the server does, and ends it with Done or
// Fail. It does not look for alternatives.
func check(ctx context.Context, log *slog.Logger, s Settings, title string, age int, stdout, stderr io.Writer) error {
	c, err := NewCheckApp(ctx, s)
	if err != nil {
		return err
	}
	defer c.Close()
	ref, err := id.New()
	if err != nil {
		return err
	}
	hold, err := c.Meter.BeginCheck(ctx, ref)
	if err != nil {
		return err
	}
	card, err := c.Pipeline.Run(ctx, pipeline.Request{Title: title, Age: age}, ref)
	if err != nil {
		_ = hold.Fail()
		return err
	}
	if err := hold.Done(); err != nil {
		log.Error("book check", "err", err)
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(card)
}
