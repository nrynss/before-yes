package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/nrynss/keel/flag"
	"github.com/nrynss/keel/gate"
	"github.com/nrynss/keel/wire"

	"github.com/nrynss/before-yes/internal/pipeline"
)

// CodeLiveChecksOff is the refusal a new check gets while the live switch
// is off. A title with a fresh cached card still gets its card.
const CodeLiveChecksOff = "live_checks_off"

// liveOffMessage is the fixed sentence for CodeLiveChecksOff
// (testdata/contracts/api/post-checks-503-live-off.json).
const liveOffMessage = "live checks are switched off; cached cards are still served"

// LiveFlag declares the live switch as a Keel runtime flag. It is a flag,
// not a config value read at start, so an operator turns fresh checks off
// (or back on) with one write to the flag store and no redeploy or restart,
// while cached cards keep being served. The config's `live` setting becomes
// the flag's default: it holds until someone sets the flag.
func LiveFlag(def bool) flag.Bool {
	return flag.Bool{
		Name:    "live_checks",
		Default: def,
		Help:    "When false, POST /api/checks answers only from cached cards and refuses new checks with live_checks_off.",
	}
}

// CheckRule is the default bucket shape for POST /api/checks. One client
// gets 3 back-to-back checks, then one more every 2 minutes; the whole
// route gets 10, then one every 30 seconds. A check spends money and the
// daily fresh-check cap is 5, so the buckets only need to stop a loop, not
// pace a reader.
var CheckRule = gate.Rule{
	Name:      "checks",
	PerClient: gate.Limit{Burst: 3, Every: 2 * time.Minute},
	Global:    gate.Limit{Burst: 10, Every: 30 * time.Second},
}

// TrustedProxies is the default set of peers whose forwarding headers are
// believed: loopback, and the Docker bridge range (172.16.0.0/12) that
// Traefik reaches the tenant's container from on the Hetzner box. It is
// narrower than Keel's default (all private ranges) on purpose: a peer on
// any other private network is keyed by its own address.
var TrustedProxies = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("172.16.0.0/12"),
}

// GateConfig configures GatedHandler.
type GateConfig struct {
	// Passcode, when non-empty, is required on POST /api/checks.
	Passcode string
	// TrustedProxies overrides the package default. Nil means
	// TrustedProxies; an empty non-nil slice trusts no peer.
	TrustedProxies []netip.Prefix
	// Rule overrides CheckRule. A zero Rule means CheckRule.
	Rule gate.Rule
	// Flags is the store the live switch is read from on every request.
	// Required.
	Flags flag.Store
	// Live is the live switch's declaration, normally LiveFlag(config live).
	// A zero value means LiveFlag(true).
	Live flag.Bool
	// Now is the clock the buckets refill from. Nil means time.Now.
	Now func() time.Time
	// Log receives one line per refusal. Nil discards.
	Log *slog.Logger
}

// GatedHandler returns the server's routes with POST /api/checks behind
// Keel's gate (passcode, then the per-client and global buckets) and the
// live switch. Every other route is ungated.
//
// The order follows docs/api.md: the gate, then validation, then the card
// cache, then live_checks_off. A fresh cached card is answered even while
// live checks are off, because it costs nothing.
func (s *Server) GatedHandler(cfg GateConfig) (http.Handler, error) {
	if cfg.Flags == nil {
		return nil, fmt.Errorf("%w: a flag store is required for the live switch", ErrInvalidConfig)
	}
	inner, err := s.Handler()
	if err != nil {
		return nil, err
	}
	live := cfg.Live
	if live.Name == "" {
		live = LiveFlag(true)
	}
	proxies := cfg.TrustedProxies
	if proxies == nil {
		proxies = TrustedProxies
	}
	rule := cfg.Rule
	if rule.Name == "" {
		rule = CheckRule
	}
	g, err := gate.New(gate.Config{
		Passcode:       cfg.Passcode,
		TrustedProxies: proxies,
		Now:            cfg.Now,
		Log:            cfg.Log,
	})
	if err != nil {
		return nil, fmt.Errorf("server: gate: %w", err)
	}
	checks, err := g.Protect(rule, s.liveSwitch(cfg.Flags, live, inner))
	if err != nil {
		return nil, fmt.Errorf("server: gate: %w", err)
	}
	checks = s.passcodeCounted(checks)
	mux := http.NewServeMux()
	mux.Handle("POST /api/checks", checks)
	mux.Handle("/", inner)
	return mux, nil
}

// liveSwitch refuses a new check while the live flag is off. An invalid
// request and a fresh cached card pass through to the check handler, which
// answers them as it always does. A flag that cannot be read refuses: the
// switch fails closed, because an open one spends money.
func (s *Server) liveSwitch(flags flag.Store, live flag.Bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			wire.WriteError(w, http.StatusBadRequest, wire.CodeInvalidRequest, "title is required and age must be between 3 and 17", nil)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		if s.cachedOrInvalid(r.Context(), body) {
			next.ServeHTTP(w, r)
			return
		}
		on, _, err := flags.Bool(r.Context(), live)
		if err != nil {
			s.log().Error("read live switch", "err", err)
			on = false
		}
		if !on {
			// Live is off. A request that would join a running check goes
			// through marked join-only: the handler joins it or refuses it
			// under its lock, and never starts a new check (T4.6).
			if s.joinsRunning(r, body) {
				next.ServeHTTP(w, r.WithContext(withJoinOnly(r.Context())))
				return
			}
			wire.WriteError(w, http.StatusServiceUnavailable, CodeLiveChecksOff, liveOffMessage, nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// cachedOrInvalid reports whether the check handler answers this body
// without starting a check: a body that does not validate (400) or a title
// and age with a fresh card (200).
func (s *Server) cachedOrInvalid(ctx context.Context, body []byte) bool {
	var req pipeline.Request
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&req); err != nil {
		return true
	}
	req, err := req.Normalize()
	if err != nil {
		return true
	}
	if s.CardCache == nil {
		return false
	}
	cid, ok, err := s.CardCache.Fresh(ctx, req.Title, req.Age)
	return err == nil && ok && strings.TrimSpace(cid) != ""
}

// joinsRunning reports whether the check handler would join a running
// check for this body (docs/api.md step 4, before live_checks_off; T4.6).
func (s *Server) joinsRunning(r *http.Request, body []byte) bool {
	var req pipeline.Request
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&req); err != nil {
		return false
	}
	req, err := req.Normalize()
	if err != nil {
		return false
	}
	return s.joinable(r, req.Title, req.Age)
}
