package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/nrynss/keel/job"

	"github.com/nrynss/before-yes/internal/usage"
)

// Usage receives one count per event (internal/usage). Count is called on
// the request path, so it must not block; usage.Recorder queues the count.
// A nil Usage counts nothing.
type Usage interface {
	Count(event string)
}

// countUsage passes event to u. A panicking counter is logged at warn and
// the request goes on: a counter fault never fails or slows a request.
func countUsage(u Usage, log *slog.Logger, event string) {
	if u == nil {
		return
	}
	defer func() {
		if v := recover(); v != nil {
			if log == nil {
				log = slog.New(slog.DiscardHandler)
			}
			log.Warn("usage count failed", "event", event, "panic", fmt.Sprint(v))
		}
	}()
	u.Count(event)
}

// count records event on the server's counter.
func (s *Server) count(event string) {
	countUsage(s.Usage, s.log(), event)
}

// statusWriter remembers the status a handler wrote.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// passcodeCounted counts passcode_refused when the gate answers 403 to a
// check. POST /api/checks answers 403 only for the passcode (its rate
// limit answers 429), so the status is the refusal.
func (s *Server) passcodeCounted(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == http.StatusForbidden {
			s.count(usage.EventPasscodeRefused)
		}
	})
}

// outcomeCounted counts how a fresh check ended: check_finished for a card,
// cap_refused for a budget or daily-cap refusal, and check_failed otherwise.
func (s *Server) outcomeCounted(fn job.Func) job.Func {
	return func(ctx context.Context, report func(job.Progress)) ([]byte, error) {
		data, err := fn(ctx, report)
		switch {
		case err == nil:
			s.count(usage.EventCheckFinished)
		case isBudgetFailure(err):
			s.count(usage.EventCapRefused)
		default:
			s.count(usage.EventCheckFailed)
		}
		return data, err
	}
}

// isBudgetFailure reports whether a check ended on a spend ceiling: the
// daily cap or a pool's budget (mapFailure's CodeBudgetExhausted).
func isBudgetFailure(err error) bool {
	var f job.Failure
	return errors.As(err, &f) && f.Code == CodeBudgetExhausted
}
