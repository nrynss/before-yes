package main

import (
	"context"
	"fmt"

	"github.com/nrynss/keel/config"
	"github.com/nrynss/keel/config/source"

	"github.com/nrynss/before-yes/internal/model"
)

// Settings is the whole configuration, the shape of before-yes.toml.example.
// The values in defaults are the defaults; the file overlays them, then
// environment overrides, then flags (Keel config). Secrets are references.
type Settings struct {
	Listen  string `toml:"listen"`
	DataDir string `toml:"data_dir"`
	WebDir  string `toml:"web_dir"`
	// Live is the live switch's default (T4.3): the Keel flag reads it
	// until an operator sets the flag.
	Live        bool   `toml:"live"`
	Country     string `toml:"country"`
	ReviewPages int    `toml:"review_pages"`

	CardMaxAgeHours     int `toml:"card_max_age_hours"`
	EvidenceMaxAgeHours int `toml:"evidence_max_age_hours"`

	// JudgeFast and JudgeThink are the judge's thinking switches for its
	// main and conflict passes.
	JudgeFast  map[string]any `toml:"judge_fast"`
	JudgeThink map[string]any `toml:"judge_think"`

	Reader model.Settings `toml:"reader"`
	Judge  model.Settings `toml:"judge"`

	Budget struct {
		ModelUSD             float64 `toml:"model_usd"`
		TavilyCreditsMonthly float64 `toml:"tavily_credits_monthly"`
		DailyFreshChecks     int     `toml:"daily_fresh_checks"`
	} `toml:"budget"`

	Gate struct {
		// Passcode is optional; unset means rate limits alone.
		Passcode config.Secret `toml:"passcode"`
	} `toml:"gate"`

	Secrets struct {
		Reader config.Secret `toml:"reader"`
		Judge  config.Secret `toml:"judge"`
		Tavily config.Secret `toml:"tavily"`
	} `toml:"secrets"`
}

func defaults() Settings {
	var s Settings
	s.Listen = "127.0.0.1:8090"
	s.DataDir = "data"
	s.WebDir = "web/build"
	s.Live = true
	s.Country = "us"
	s.ReviewPages = 10
	s.CardMaxAgeHours = 168
	s.EvidenceMaxAgeHours = 168
	s.Budget.ModelUSD = 20
	s.Budget.TavilyCreditsMonthly = 900
	s.Budget.DailyFreshChecks = 5
	return s
}

// Load reads the settings. path empty means $BEFORE_YES_CONFIG, then
// ./before-yes.toml. lookup nil means the process environment.
func Load(ctx context.Context, path string, lookup func(string) (string, bool)) (Settings, config.Plan, error) {
	s := defaults()
	reg := config.NewRegistry()
	if err := source.RegisterDefaults(reg, source.Config{LookupEnv: lookup}); err != nil {
		return s, config.Plan{}, err
	}
	plan, err := config.Load(ctx, &s, config.Config{
		Path:      path,
		PathVar:   "BEFORE_YES_CONFIG",
		Search:    []string{"before-yes.toml"},
		LookupEnv: lookup,
		Registry:  reg,
	})
	if err != nil {
		return s, plan, fmt.Errorf("load settings: %w", err)
	}
	return s, plan, nil
}
