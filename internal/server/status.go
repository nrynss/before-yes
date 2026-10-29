package server

import (
	"log/slog"
	"net/http"

	"github.com/nrynss/keel/flag"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/spend"
)

// Status serves GET /api/status, GET /api/checklist and GET /healthz.
type Status struct {
	// Meter is the server's durable meter; its three pools give the
	// budget figures. Nil reports zeros.
	Meter *spend.Meter
	// Flags and Live are the live switch, read through on every request
	// (T4.3). Nil Flags reports Live's default.
	Flags flag.Store
	Live  flag.Bool
	// Usage counts the page views the app serves (home, compare_view). Nil
	// counts nothing. Log receives a counter's fault; nil discards.
	Usage Usage
	Log   *slog.Logger
}

// log returns the status's logger, discarding when none is set.
func (st *Status) log() *slog.Logger {
	if st.Log != nil {
		return st.Log
	}
	return slog.New(slog.DiscardHandler)
}

// statusBody is the GET /api/status body
// (testdata/contracts/api/get-status.json).
type statusBody struct {
	Live             bool         `json:"live"`
	ChecklistVersion string       `json:"checklist_version"`
	Budget           statusBudget `json:"budget"`
}

type statusBudget struct {
	TokenFactoryRemainingNano int64 `json:"token_factory_remaining_nano"`
	TavilyCreditsRemaining    int64 `json:"tavily_credits_remaining"`
	FreshChecksRemainingToday int64 `json:"fresh_checks_remaining_today"`
}

func (st *Status) status(w http.ResponseWriter, r *http.Request) {
	live := st.Live.Default
	if st.Flags != nil && st.Live.Name != "" {
		v, _, err := st.Flags.Bool(r.Context(), st.Live)
		if err != nil {
			// The live switch fails closed (T4.3), so status says off.
			v = false
		}
		live = v
	}
	out := statusBody{Live: live, ChecklistVersion: checklist.Version}
	if st.Meter != nil {
		rem := func(p spend.Pool) int64 {
			s, err := st.Meter.Status(r.Context(), p)
			if err != nil || s.Remaining < 0 {
				return 0
			}
			return int64(s.Remaining)
		}
		out.Budget.TokenFactoryRemainingNano = rem(spend.PoolModel)
		out.Budget.TavilyCreditsRemaining = rem(spend.PoolTavily) / int64(spend.CreditUnit)
		out.Budget.FreshChecksRemainingToday = rem(spend.PoolChecksDaily)
	}
	writeJSON(w, http.StatusOK, out)
}

// checklistBody is the GET /api/checklist body
// (testdata/contracts/api/get-checklist.json).
type checklistBody struct {
	Version  string               `json:"version"`
	Areas    []checklist.AreaSpec `json:"areas"`
	Levels   []checklist.Level    `json:"levels"`
	Verdicts []checklist.Verdict  `json:"verdicts"`
}

func (st *Status) checklist(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, checklistBody{
		Version:  checklist.Version,
		Areas:    checklist.Areas(),
		Levels:   []checklist.Level{checklist.OK, checklist.OKWithSetting, checklist.Flag, checklist.Unknown},
		Verdicts: []checklist.Verdict{checklist.Yes, checklist.YesWithSettings, checklist.No, checklist.Unclear},
	})
}

func healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok\n"))
}
