package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/job"
	"github.com/nrynss/keel/wire"

	"github.com/nrynss/before-yes/internal/model"
	"github.com/nrynss/before-yes/internal/pipeline"
	"github.com/nrynss/before-yes/internal/spend"
	"github.com/nrynss/before-yes/internal/tavily"
)

// Visitor key headers on POST /api/checks. A visitor sends both or
// neither. The keys are held for this one check only: they live in the
// clients built for its job and in nothing that outlasts it.
const (
	HeaderTokenFactoryKey = "X-Token-Factory-Key"
	HeaderTavilyKey       = "X-Tavily-Key"
)

// maxVisitorKey bounds one pasted key.
const maxVisitorKey = 512

// visitorKeysMessage is the fixed refusal for unusable visitor keys. It
// never quotes what was sent.
const visitorKeysMessage = "send both a Token Factory key and a Tavily key, or neither"

// VisitorKeys is one visitor's pair of keys. Its fields are unexported and
// every printing path redacts, so a stray %v, a log attribute or a JSON
// encoding cannot write a key.
type VisitorKeys struct {
	tokenFactory string
	tavily       string
}

// NewVisitorKeys returns the pair. It is for tests and for the terminal
// path; the HTTP path reads the headers.
func NewVisitorKeys(tokenFactory, tavilyKey string) VisitorKeys {
	return VisitorKeys{tokenFactory: tokenFactory, tavily: tavilyKey}
}

func (VisitorKeys) String() string               { return "VisitorKeys{redacted}" }
func (VisitorKeys) GoString() string             { return "VisitorKeys{redacted}" }
func (VisitorKeys) LogValue() slog.Value         { return slog.StringValue("redacted") }
func (VisitorKeys) MarshalJSON() ([]byte, error) { return []byte(`"redacted"`), nil }

func (k VisitorKeys) secrets() []string { return []string{k.tokenFactory, k.tavily} }

// visitorKeysFrom reads the visitor key headers. ok is false when neither
// is sent. Both must be present, at most maxVisitorKey bytes, and printable
// ASCII with no spaces.
func visitorKeysFrom(r *http.Request) (VisitorKeys, bool, error) {
	tf := strings.TrimSpace(r.Header.Get(HeaderTokenFactoryKey))
	tv := strings.TrimSpace(r.Header.Get(HeaderTavilyKey))
	if tf == "" && tv == "" {
		return VisitorKeys{}, false, nil
	}
	if !usableKey(tf) || !usableKey(tv) {
		return VisitorKeys{}, true, errors.New(visitorKeysMessage)
	}
	return VisitorKeys{tokenFactory: tf, tavily: tv}, true, nil
}

func usableKey(k string) bool {
	if k == "" || len(k) > maxVisitorKey {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] <= ' ' || k[i] > '~' {
			return false
		}
	}
	return true
}

// VisitorMeter records what a visitor-key check spent and draws on none of
// our pools: the visitor's own accounts pay. It satisfies pipeline.Meter.
type VisitorMeter struct {
	mu    sync.Mutex
	spent map[spend.Pool]cost.Price
}

// Spend runs work and records its price under pool. Nothing is reserved
// or booked in the durable meter.
func (m *VisitorMeter) Spend(ctx context.Context, pool spend.Pool, kind, ref string, estimate cost.Price, work cost.Work) (cost.Usage, error) {
	u, err := work(ctx)
	if err == nil {
		m.mu.Lock()
		if m.spent == nil {
			m.spent = map[spend.Pool]cost.Price{}
		}
		m.spent[pool] += u.Price
		m.mu.Unlock()
	}
	return u, err
}

// Spent reports what the visitor's check spent in pool.
func (m *VisitorMeter) Spent(pool spend.Pool) cost.Price {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.spent[pool]
}

// ErrVisitorUnsupported is returned when the base pipeline's clients are
// not ones a visitor copy can be built from.
var ErrVisitorUnsupported = errors.New("server: visitor keys need model and Tavily clients")

// VisitorPipeline returns a copy of base whose Token Factory and Tavily
// clients are new ones built for this job alone, reading keys, and whose
// meter is a VisitorMeter. The settings (hosts, models, prices, caps,
// retry tuning) are base's, so the evidence cache key is the same as for a
// check on our keys. Checks is nil: a visitor's alternatives run on the
// visitor's keys and do not count against the daily cap. The App Store
// client and the evidence cache are shared; neither holds a key.
func VisitorPipeline(base *pipeline.Pipeline, keys VisitorKeys) (*pipeline.Pipeline, *VisitorMeter, error) {
	if base == nil || base.Gatherer == nil || base.Reader == nil || base.Judge == nil {
		return nil, nil, ErrVisitorUnsupported
	}
	meter := &VisitorMeter{}
	tfKey := func() (string, error) { return keys.tokenFactory, nil }
	tvKey := func() (string, error) { return keys.tavily, nil }

	search, ok := base.Gatherer.Search.(*tavily.Client)
	if !ok || search == nil {
		return nil, nil, ErrVisitorUnsupported
	}
	vs := tavily.New(search.BaseURL, tvKey)
	vs.Retry = search.Retry

	reader, err := visitorModel(base.Reader.Model, tfKey)
	if err != nil {
		return nil, nil, err
	}
	judge, err := visitorModel(base.Judge.Model, tfKey)
	if err != nil {
		return nil, nil, err
	}
	var entail pipeline.Chatter
	if base.Judge.Entail != nil {
		if entail, err = visitorModel(base.Judge.Entail, tfKey); err != nil {
			return nil, nil, err
		}
	}

	g := *base.Gatherer
	g.Search, g.Meter = vs, meter
	rd := *base.Reader
	rd.Model, rd.Meter = reader, meter
	j := *base.Judge
	j.Model, j.Meter = judge, meter
	if base.Judge.Entail != nil {
		j.Entail = entail
	}
	p := *base
	p.Gatherer, p.Reader, p.Judge = &g, &rd, &j
	p.Checks = nil
	return &p, meter, nil
}

// visitorModel builds a model client with c's settings and tuning that
// reads key instead of ours.
func visitorModel(c pipeline.Chatter, key func() (string, error)) (pipeline.Chatter, error) {
	mc, ok := c.(*model.Client)
	if !ok || mc == nil {
		return nil, ErrVisitorUnsupported
	}
	v, err := model.New(mc.Settings(), key)
	if err != nil {
		return nil, fmt.Errorf("server: visitor model client: %w", err)
	}
	v.Retry, v.Timeouts = mc.Retry, mc.Timeouts
	return v, nil
}

// admission is what startCheck needs to run one fresh check: the pipeline
// to run it on, the daily-cap hold when it runs on our keys, and the
// secrets to scrub from its failure when it runs on a visitor's.
type admission struct {
	p       *pipeline.Pipeline
	hold    *spend.Check
	secrets []string
	log     *slog.Logger
}

// admit decides how a fresh check (a card-cache miss) runs. Visitor keys
// build a visitor pipeline and reserve nothing. Otherwise the main check
// reserves one fresh check through s.Checks before its first paid call;
// alternatives reserve their own through Pipeline.Checks, the same meter
// (T3.5), so they are never reserved here. A spent cap answers 402
// budget_exhausted with pool daily_checks. When admit returns false it has
// written the response.
func (s *Server) admit(w http.ResponseWriter, r *http.Request, ref string) (admission, bool) {
	keys, sent, err := visitorKeysFrom(r)
	if err != nil {
		wire.WriteError(w, http.StatusBadRequest, wire.CodeInvalidRequest, visitorKeysMessage, nil)
		return admission{}, false
	}
	if sent {
		p, _, err := VisitorPipeline(s.Pipeline, keys)
		if err != nil {
			s.log().Error("visitor pipeline", "err", err)
			wire.WriteError(w, http.StatusBadRequest, wire.CodeInvalidRequest, "this server does not take visitor keys", nil)
			return admission{}, false
		}
		return admission{p: p, secrets: keys.secrets(), log: s.log()}, true
	}
	a := admission{p: s.Pipeline, log: s.log()}
	if s.Checks == nil {
		return a, true
	}
	hold, err := s.Checks.BeginCheck(r.Context(), ref)
	if errors.Is(err, cost.ErrOverBudget) {
		wire.WriteError(w, http.StatusPaymentRequired, CodeBudgetExhausted, "today's budget for fresh checks is spent", map[string]string{"pool": "daily_checks"})
		return admission{}, false
	}
	if err != nil {
		s.log().Error("reserve check", "err", err)
		wire.WriteError(w, http.StatusInternalServerError, wire.CodeInternal, "could not start a check", nil)
		return admission{}, false
	}
	a.hold = hold
	return a, true
}

// release frees the hold of a check whose job never started.
func (a admission) release() {
	if a.hold != nil {
		if err := a.hold.Fail(); err != nil {
			a.log.Error("release check hold", "err", err)
		}
	}
}

// wrap ends the hold when the job ends, Done on a card and Fail on any
// failure or panic, and scrubs visitor keys from a failure before the
// runner stores or logs it.
func (a admission) wrap(fn job.Func) job.Func {
	return func(ctx context.Context, report func(job.Progress)) (data []byte, err error) {
		ended := false
		defer func() {
			if !ended && a.hold != nil {
				_ = a.hold.Fail()
			}
		}()
		data, err = fn(ctx, report)
		if a.hold != nil {
			ended = true
			if err == nil {
				if derr := a.hold.Done(); derr != nil {
					a.log.Error("book check", "err", derr)
				}
			} else if ferr := a.hold.Fail(); ferr != nil {
				a.log.Error("release check hold", "err", ferr)
			}
		}
		if err != nil && len(a.secrets) > 0 {
			err = scrub(err, a.secrets)
		}
		return data, err
	}
}

// scrub rebuilds err with every secret replaced, keeping its job.Failure
// code and detail. An upstream may echo the key it was sent in an error
// body, and that body reaches the error text.
func scrub(err error, secrets []string) error {
	clean := func(s string) string {
		for _, k := range secrets {
			if k != "" {
				s = strings.ReplaceAll(s, k, "[redacted]")
			}
		}
		return s
	}
	var f job.Failure
	if errors.As(err, &f) {
		msg := ""
		if f.Err != nil {
			msg = clean(f.Err.Error())
		}
		return job.Failure{Code: f.Code, Detail: f.Detail, Err: errors.New(msg)}
	}
	return errors.New(clean(err.Error()))
}

// checkCapWiring refuses a server whose pipeline would not share the
// server's daily-cap meter. Alternatives reserve through Pipeline.Checks
// and the main check through Server.Checks; both must be the same
// *spend.Meter, or alternatives run uncapped (Pipeline.Checks unset) or on
// a different ledger.
func (s *Server) checkCapWiring() error {
	if s.Pipeline == nil {
		return nil
	}
	sc, pc := any(s.Checks), any(s.Pipeline.Checks)
	if s.Checks == nil && s.Pipeline.Checks == nil {
		return nil
	}
	if s.Checks == nil || s.Pipeline.Checks == nil || sc != pc {
		return fmt.Errorf("%w: Server.Checks and Pipeline.Checks must be the same meter", ErrInvalidConfig)
	}
	return nil
}
