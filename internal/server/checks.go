// Package server is the HTTP surface: start a check, follow it, read a card.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/nrynss/keel/cache"
	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/job"
	"github.com/nrynss/keel/job/sqlitestore"
	"github.com/nrynss/keel/sqlite"
	"github.com/nrynss/keel/stream"
	"github.com/nrynss/keel/wire"

	"github.com/nrynss/before-yes/internal/cards"
	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/pipeline"
	"github.com/nrynss/before-yes/internal/spend"
)

const (
	// JobKind names the Keel job kind for check jobs.
	JobKind = "check"

	// Codes returned by the server or carried by job error terminals.
	CodeBudgetExhausted = "budget_exhausted"
	CodeNoSources       = "no_sources"
	CodeCheckFailed     = "check_failed"

	// DefaultCheckLimit is the default number of concurrent check jobs.
	DefaultCheckLimit = 4
)

// Checks is the daily fresh check cap. startCheck reserves the main check
// through it on a card-cache miss (byok.go, T4.5); it must be the same
// meter as Pipeline.Checks, which reserves fresh alternatives.
type Checks interface {
	BeginCheck(ctx context.Context, ref string) (*spend.Check, error)
}

// Server is the HTTP server for Before Yes.
type Server struct {
	Pipeline      *pipeline.Pipeline
	Runner        *job.Runner
	Broker        *stream.Broker
	Cards         *cards.Store
	EvidenceCache *EvidenceCache
	CardCache     *CardCache
	CardMaxAge    time.Duration
	Log           *slog.Logger
	Checks        Checks
}

// NewRunner creates a Keel job.Runner configured for check jobs.
// Checks run under JobKind with limit (or DefaultCheckLimit if <= 0) and
// are not idempotent (Idempotent: false), ensuring an interrupted check
// is marked interrupted and not rerun across restarts.
func NewRunner(db *sqlite.DB, broker *stream.Broker, limit int, now func() time.Time, log *slog.Logger) (*job.Runner, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: db is required", ErrInvalidConfig)
	}
	if broker == nil {
		return nil, fmt.Errorf("%w: broker is required", ErrInvalidConfig)
	}
	store, err := sqlitestore.Open(context.Background(), sqlitestore.Config{
		DB: db,
	})
	if err != nil {
		return nil, fmt.Errorf("server: open job store: %w", err)
	}
	if limit <= 0 {
		limit = DefaultCheckLimit
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if now == nil {
		now = time.Now
	}
	return job.Open(context.Background(), job.Config{
		Broker: broker,
		Store:  store,
		Kinds: map[string]job.Kind{
			JobKind: {
				Limit:      limit,
				Idempotent: false,
			},
		},
		Log: log,
		Now: now,
	})
}

// Handler registers the server routes and returns an http.Handler.
func (s *Server) Handler() (http.Handler, error) {
	if s.Runner == nil {
		return nil, fmt.Errorf("%w: runner is required", ErrInvalidConfig)
	}
	if s.Broker == nil {
		return nil, fmt.Errorf("%w: broker is required", ErrInvalidConfig)
	}
	if s.Cards == nil {
		return nil, fmt.Errorf("%w: cards store is required", ErrInvalidConfig)
	}
	if s.CardCache == nil {
		cc, err := NewCardCache(CardCacheConfig{
			Cards:  s.Cards,
			MaxAge: s.CardMaxAge,
		})
		if err != nil {
			return nil, err
		}
		s.CardCache = cc
	}
	if s.Log == nil {
		s.Log = slog.New(slog.DiscardHandler)
	}
	if err := s.checkCapWiring(); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/checks", s.startCheck)
	mux.HandleFunc("GET /api/checks/{id}", s.getCheck)
	mux.HandleFunc("GET /api/checks/{id}/events", s.checkEvents)
	mux.HandleFunc("GET /api/cards/{id}", s.getCard)
	mux.HandleFunc("GET /api/cards", s.recentCards)

	return mux, nil
}

func (s *Server) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.New(slog.DiscardHandler)
}

func (s *Server) startCheck(w http.ResponseWriter, r *http.Request) {
	var req pipeline.Request
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		wire.WriteError(w, http.StatusBadRequest, wire.CodeInvalidRequest, "title is required and age must be between 3 and 17", nil)
		return
	}
	normReq, err := req.Normalize()
	if err != nil {
		wire.WriteError(w, http.StatusBadRequest, wire.CodeInvalidRequest, "title is required and age must be between 3 and 17", nil)
		return
	}
	req = normReq

	ctx := r.Context()
	if s.CardCache != nil {
		cid, ok, err := s.CardCache.Fresh(ctx, req.Title, req.Age)
		if err == nil && ok {
			if !strings.HasPrefix(cid, "c_") {
				cid = "c_" + cid
			}
			writeJSON(w, http.StatusOK, map[string]string{"card_id": cid})
			return
		}
	}

	ref, err := id.New()
	if err != nil {
		s.log().Error("start check id", "err", err)
		wire.WriteError(w, http.StatusInternalServerError, wire.CodeInternal, "could not start a check", nil)
		return
	}

	adm, ok := s.admit(w, r, ref)
	if !ok {
		return
	}
	jid, err := s.Runner.StartKind(ctx, JobKind, adm.wrap(s.work(adm.p, req, ref)))
	if err != nil {
		adm.release()
	}
	if errors.Is(err, job.ErrLimit) {
		w.Header().Set("Retry-After", "20")
		wire.WriteError(w, http.StatusServiceUnavailable, "busy", "too many checks are running; try again shortly", map[string]any{"retry_after_seconds": 20})
		return
	}
	if err != nil {
		s.log().Error("start check job", "err", err)
		wire.WriteError(w, http.StatusInternalServerError, wire.CodeInternal, "could not start a check", nil)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"check_id": "j_" + jid})
}

func (s *Server) work(p *pipeline.Pipeline, req pipeline.Request, ref string) job.Func {
	return func(ctx context.Context, report func(job.Progress)) ([]byte, error) {
		progress := func(stage, msg string, cur, total *int64) {
			p := job.Progress{Stage: stage, Current: cur, Total: total}
			if msg != "" {
				p.Detail, _ = json.Marshal(map[string]string{"message": msg})
			}
			report(p)
		}

		progress("gather", "searching for the privacy policy", nil, nil)

		var ev pipeline.Evidence
		var hit bool
		var err error
		if s.EvidenceCache != nil && p != nil {
			ev, hit, err = s.EvidenceCache.Collect(ctx, p, req.Title, ref)
		} else if p != nil {
			ev, hit, err = p.Collect(ctx, req.Title, ref)
		} else {
			return nil, mapFailure(errors.New("no pipeline configured"))
		}
		if err != nil {
			return nil, mapFailure(err)
		}

		if hit {
			progress("read", "using sources read "+ev.CollectedAt.Format("2 Jan 2006"), nil, nil)
		} else {
			if ev.Stats.Reviews > 0 {
				cur, tot := int64(ev.Stats.Reviews), int64(ev.Stats.Reviews)
				progress("read", fmt.Sprintf("reading %d reviews", ev.Stats.Reviews), &cur, &tot)
			} else {
				progress("read", "reading sources", nil, nil)
			}
		}

		progress("judge", "grading 6 areas", nil, nil)

		card, err := p.Decide(ctx, ev, req.Age, ref)
		if err != nil {
			return nil, mapFailure(err)
		}

		if (card.Verdict == checklist.No || card.Verdict == checklist.YesWithSettings) && p.Gatherer != nil {
			progress("alternatives", "finding safer alternatives", nil, nil)
			pending, err := p.FindAlternatives(ctx, card, ref)
			if err != nil {
				if errors.Is(err, cost.ErrOverBudget) {
					return nil, mapFailure(err)
				}
				cid, err := s.Cards.Put(ctx, card)
				if err != nil {
					return nil, mapFailure(err)
				}
				if !strings.HasPrefix(cid, "c_") {
					cid = "c_" + cid
				}
				return []byte(cid), nil
			}
			if len(pending) > 0 {
				altRes := p.CheckAlternatives(ctx, card, pending, ref)
				card = altRes.Apply(card)
				cid, _, err := s.Cards.PutWithAlternatives(ctx, card, altRes.Cards)
				if err != nil {
					return nil, mapFailure(err)
				}
				if !strings.HasPrefix(cid, "c_") {
					cid = "c_" + cid
				}
				return []byte(cid), nil
			}
		}

		cid, err := s.Cards.Put(ctx, card)
		if err != nil {
			return nil, mapFailure(err)
		}
		if !strings.HasPrefix(cid, "c_") {
			cid = "c_" + cid
		}
		return []byte(cid), nil
	}
}

func mapFailure(err error) error {
	var f job.Failure
	if errors.As(err, &f) {
		return f
	}

	switch {
	case errors.Is(err, cost.ErrOverBudget):
		errStr := strings.ToLower(err.Error())
		pool := "token_factory"
		msg := "the Token Factory budget is spent"
		switch {
		case strings.Contains(errStr, "tavily"):
			pool = "tavily"
			msg = "the Tavily budget is spent"
		case strings.Contains(errStr, "checks") || strings.Contains(errStr, "daily"):
			pool = "daily_checks"
			msg = "today's budget for fresh checks is spent"
		}
		return job.Failure{
			Code:   CodeBudgetExhausted,
			Detail: map[string]string{"pool": pool},
			Err:    errors.New(msg),
		}
	case errors.Is(err, pipeline.ErrNoSources):
		return job.Failure{
			Code: CodeNoSources,
			Err:  errors.New("nothing could be read about that title; check the spelling, or try its full name"),
		}
	case errors.Is(err, cache.ErrRefused):
		return job.Failure{
			Code: CodeCheckFailed,
			Err:  errors.New("this title failed recently; try again later"),
		}
	default:
		return job.Failure{
			Code: CodeCheckFailed,
			Err:  err,
		}
	}
}

func fixedErrorMessage(code string, detail any) string {
	switch code {
	case CodeBudgetExhausted:
		if m, ok := detail.(map[string]any); ok {
			if pool, ok := m["pool"].(string); ok {
				switch pool {
				case "token_factory":
					return "the Token Factory budget is spent"
				case "tavily":
					return "the Tavily budget is spent"
				case "daily_checks":
					return "today's budget for fresh checks is spent"
				}
			}
		}
		return "the Token Factory budget is spent"
	case CodeNoSources:
		return "nothing could be read about that title; check the spelling, or try its full name"
	case CodeCheckFailed:
		return "the check could not finish; try again shortly"
	default:
		return "the check could not finish; try again shortly"
	}
}

func (s *Server) getCheck(w http.ResponseWriter, r *http.Request) {
	rawID := r.PathValue("id")
	if !strings.HasPrefix(rawID, "j_") || !id.Valid(strings.TrimPrefix(rawID, "j_")) {
		wire.WriteError(w, http.StatusNotFound, wire.CodeNotFound, "no such check", nil)
		return
	}
	jid := strings.TrimPrefix(rawID, "j_")
	res, err := s.Runner.Result(r.Context(), jid)
	if errors.Is(err, job.ErrUnknownJob) {
		wire.WriteError(w, http.StatusNotFound, wire.CodeNotFound, "no such check", nil)
		return
	}
	if err != nil {
		wire.WriteError(w, http.StatusInternalServerError, wire.CodeInternal, "could not read the check", nil)
		return
	}

	switch res.Status {
	case job.StatusRunning:
		writeJSON(w, http.StatusOK, map[string]any{
			"id":     rawID,
			"status": "running",
		})
	case job.StatusQueued:
		writeJSON(w, http.StatusOK, map[string]any{
			"id":     rawID,
			"status": "queued",
		})
	case job.StatusDone:
		cardID := string(res.Data)
		if !strings.HasPrefix(cardID, "c_") {
			cardID = "c_" + cardID
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":      rawID,
			"status":  "done",
			"card_id": cardID,
		})
	case job.StatusError:
		code := res.Code
		if code == "" {
			code = "job_failed"
		}
		var detail any
		if len(res.Detail) > 0 {
			_ = json.Unmarshal(res.Detail, &detail)
		}
		msg := fixedErrorMessage(code, detail)
		out := map[string]any{
			"id":      rawID,
			"status":  "error",
			"code":    code,
			"message": msg,
		}
		if detail != nil {
			out["detail"] = detail
		}
		writeJSON(w, http.StatusOK, out)
	case job.StatusInterrupted:
		writeJSON(w, http.StatusOK, map[string]any{
			"id":      rawID,
			"status":  "interrupted",
			"code":    CodeCheckFailed,
			"message": "The server restarted during this check. Ask again; sources already read are reused.",
		})
	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"id":     rawID,
			"status": string(res.Status),
		})
	}
}

func (s *Server) checkEvents(w http.ResponseWriter, r *http.Request) {
	rawID := r.PathValue("id")
	if !strings.HasPrefix(rawID, "j_") || !id.Valid(strings.TrimPrefix(rawID, "j_")) {
		wire.WriteError(w, http.StatusNotFound, wire.CodeNotFound, "no such check", nil)
		return
	}
	jid := strings.TrimPrefix(rawID, "j_")
	if _, err := s.Runner.Result(r.Context(), jid); err != nil {
		if errors.Is(err, job.ErrUnknownJob) {
			wire.WriteError(w, http.StatusNotFound, wire.CodeNotFound, "no such check", nil)
			return
		}
		wire.WriteError(w, http.StatusInternalServerError, wire.CodeInternal, "could not read the check", nil)
		return
	}
	s.Broker.ServeTopic(w, r, job.Topic(jid))
}

func (s *Server) getCard(w http.ResponseWriter, r *http.Request) {
	rawID := r.PathValue("id")
	if !strings.HasPrefix(rawID, "c_") || !id.Valid(strings.TrimPrefix(rawID, "c_")) {
		wire.WriteError(w, http.StatusNotFound, wire.CodeNotFound, "no such card", nil)
		return
	}
	cid := strings.TrimPrefix(rawID, "c_")
	card, err := s.Cards.Get(r.Context(), rawID)
	if errors.Is(err, cards.ErrNotFound) {
		card, err = s.Cards.Get(r.Context(), cid)
	}
	if errors.Is(err, cards.ErrNotFound) {
		wire.WriteError(w, http.StatusNotFound, wire.CodeNotFound, "no such card", nil)
		return
	}
	if err != nil {
		wire.WriteError(w, http.StatusInternalServerError, wire.CodeInternal, "could not read the card", nil)
		return
	}
	if !strings.HasPrefix(card.ID, "c_") {
		card.ID = "c_" + card.ID
	}
	writeJSON(w, http.StatusOK, card)
}

func (s *Server) recentCards(w http.ResponseWriter, r *http.Request) {
	list, err := s.Cards.Recent(r.Context(), 12)
	if err != nil {
		wire.WriteError(w, http.StatusInternalServerError, wire.CodeInternal, "could not list cards", nil)
		return
	}
	if list == nil {
		list = []cards.Summary{}
	}
	for i := range list {
		if !strings.HasPrefix(list[i].ID, "c_") {
			list[i].ID = "c_" + list[i].ID
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": list})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
