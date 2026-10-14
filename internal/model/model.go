// Package model is one small client for the OpenAI-style chat API that Nebius
// Token Factory, OpenRouter and NVIDIA's API all speak. The base URL, the
// model name, the rate card and the pace come from config, so moving a role
// from a free development tier to Token Factory is two settings.
package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/throttle"
)

// DefaultMaxTokens caps one reply when Settings.MaxTokens is unset.
const DefaultMaxTokens = 4096

// Settings configures one model role: the reader or judge table of the
// settings file.
type Settings struct {
	// Provider names the account, for the ledger and the pacer window, such
	// as "tokenfactory" or "openrouter".
	Provider string `toml:"provider"`
	// BaseURL is the host's API root; the client adds "/chat/completions".
	BaseURL string `toml:"base_url"`
	// Model is the host's model id, such as
	// "nvidia/nvidia-nemotron-3-nano-30b-a3b".
	Model string `toml:"model"`
	// InputPerM and OutputPerM are dollars per million tokens, the unit
	// both hosts publish. Zero on a free tier.
	InputPerM  float64 `toml:"input_per_m"`
	OutputPerM float64 `toml:"output_per_m"`
	// MaxTokens caps one reply. Zero or less means DefaultMaxTokens.
	MaxTokens int `toml:"max_tokens"`
	// PerMinute paces call starts under the account's per-minute cap. Zero
	// means no pacing.
	PerMinute int `toml:"per_minute"`
	// Extra is merged into every request body on top of the fields the
	// client sets. It carries the provider-specific switch for thinking,
	// which differs between hosts.
	Extra map[string]any `toml:"extra"`
}

// Client calls one model.
type Client struct {
	s    Settings
	key  func() (string, error)
	http *http.Client
	// Retry tunes the attempts. Tests shorten it.
	Retry throttle.Config
}

// New returns a client for s. key is read on every call, so a rotated secret
// takes effect without a restart. A nil key reads as no key at all.
func New(s Settings, key func() (string, error)) (*Client, error) {
	if s.BaseURL == "" || s.Model == "" {
		return nil, errors.New("model: base_url and model are required")
	}
	if s.MaxTokens <= 0 {
		s.MaxTokens = DefaultMaxTokens
	}
	if key == nil {
		key = func() (string, error) { return "", nil }
	}
	return &Client{
		s:    s,
		key:  key,
		http: &http.Client{Timeout: 3 * time.Minute},
		Retry: throttle.Config{
			// A rate-limit refusal is usually free, so it gets the larger
			// budget. A 5xx may have been billed already, so it gets the
			// smaller one.
			Attempts:          4,
			TransientAttempts: 2,
			Backoff:           5 * time.Second,
			Max:               60 * time.Second,
			WaitBudget:        3 * time.Minute,
			PerMinute:         s.PerMinute,
			Name:              s.Provider + "/" + s.Model,
		},
	}, nil
}

// Settings returns the role's settings. The Extra map is copied one level, so
// adding or replacing a top-level key in the result cannot change the client.
func (c *Client) Settings() Settings {
	s := c.s
	if s.Extra != nil {
		s.Extra = maps.Clone(s.Extra)
	}
	return s
}

// Message is one chat turn.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Options tune one call.
type Options struct {
	// JSON asks for a JSON object reply where the host supports it.
	JSON bool
	// Extra overrides Settings.Extra keys for this call, so the judge's
	// conflict pass can turn thinking on without a second client.
	Extra map[string]any
	// MaxTokens overrides Settings.MaxTokens when positive.
	MaxTokens int
}

// Reply is one completion and what it cost.
type Reply struct {
	Text         string
	InputTokens  int
	OutputTokens int
	// Price is what the usage cost on the role's rate card, or the
	// estimate Estimate reported when the host sent no usable usage. The
	// meter reserves the estimate and settles an unmeasured reply at it.
	Price cost.Price
	// Measured is false when the host reported no usable usage, so Price is
	// the estimate rather than a measured cost.
	Measured bool
}

// Error is a non-2xx answer from the host.
type Error struct {
	Status     int
	RetryAfter time.Duration
	Body       string
}

func (e *Error) Error() string {
	return fmt.Sprintf("model: HTTP %d: %s", e.Status, e.Body)
}

// Retryable reports whether waiting can clear err: a rate limit, an
// overloaded or failing host, or a dropped connection. A bad request, a
// rejected key and a reply that would not decode are permanent.
func Retryable(err error) bool {
	var he *Error
	if errors.As(err, &he) {
		return he.Status == http.StatusTooManyRequests || he.Status >= 500
	}
	return !errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded) &&
		!errors.Is(err, errPermanent)
}

// transient claims host faults, which draw on the smaller retry budget
// because each retry may be billed again. A refused rate limit does not draw
// on it: the refusal was free.
func transient(err error) bool {
	var he *Error
	if errors.As(err, &he) {
		return he.Status >= 500
	}
	return true
}

// retryAfter reads the wait the host asked for, in seconds or as an HTTP
// date.
func retryAfter(err error) (time.Duration, bool) {
	var he *Error
	if errors.As(err, &he) && he.RetryAfter > 0 {
		return he.RetryAfter, true
	}
	return 0, false
}

// errPermanent marks a failure a retry cannot clear.
var errPermanent = errors.New("model: permanent")

// Chat sends msgs and returns the reply.
func (c *Client) Chat(ctx context.Context, msgs []Message, o Options) (Reply, error) {
	body := map[string]any{
		"model":       c.s.Model,
		"messages":    msgs,
		"max_tokens":  c.s.MaxTokens,
		"temperature": 0,
	}
	if o.MaxTokens > 0 {
		body["max_tokens"] = o.MaxTokens
	}
	if o.JSON {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	// The host's own switches go on top: a key in Extra overrides the
	// client's default for this host, and the call's options override the
	// settings.
	for k, v := range c.s.Extra {
		body[k] = v
	}
	for k, v := range o.Extra {
		body[k] = v
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Reply{}, err
	}
	estimate := c.Estimate(msgs, o)
	cfg := c.Retry
	cfg.Transient = transient
	cfg.RetryAfter = retryAfter
	var reply Reply
	err = throttle.Retry(ctx, cfg, Retryable, func() error {
		r, err := c.post(ctx, payload)
		if err != nil {
			return err
		}
		if !r.Measured {
			// The host left the call unmeasured. The meter settles the
			// estimate it reserved; carry the same figure here for a
			// caller that has no meter.
			r.Price = estimate
		}
		reply = r
		return nil
	})
	if err != nil {
		// Annotate a rate limit that spent its budget, so a log can tell
		// it from one that was never retried. A host fault drew on the
		// smaller budget, which Note does not know about, so it is
		// returned as the host sent it.
		if cfg.Transient != nil && cfg.Transient(err) {
			return Reply{}, err
		}
		return Reply{}, throttle.Note(err, cfg, Retryable)
	}
	return reply, nil
}

// Estimate is what one call is reserved at before it runs: about three bytes
// of message text per input token, plus the reply's max tokens as output. It
// is the number the meter holds while the call runs and the number an
// unmeasured reply is settled at.
func (c *Client) Estimate(msgs []Message, o Options) cost.Price {
	in := 0
	for _, m := range msgs {
		in += len(m.Content)/3 + 1
	}
	out := o.MaxTokens
	if out <= 0 {
		out = c.s.MaxTokens
	}
	return c.Price(in, out)
}

// Price is what in input and out output tokens cost on this role's rate card.
func (c *Client) Price(in, out int) cost.Price {
	return cost.Price(nanosPerToken(c.s.InputPerM)*int64(in) +
		nanosPerToken(c.s.OutputPerM)*int64(out))
}

// nanosPerToken converts a dollar-per-million-token rate into nanodollars
// per token, which is the rate times a thousand. It rounds, so the two- and
// three-decimal rates the hosts publish land on exact nanodollar prices.
func nanosPerToken(perM float64) int64 {
	return int64(math.Round(perM * 1e3))
}

func (c *Client) post(ctx context.Context, payload []byte) (Reply, error) {
	key, err := c.key()
	if err != nil {
		return Reply{}, fmt.Errorf("%w: key: %v", errPermanent, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.s.BaseURL, "/")+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Reply{}, fmt.Errorf("%w: %v", errPermanent, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Reply{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return Reply{}, err
	}
	if resp.StatusCode/100 != 2 {
		return Reply{}, &Error{
			Status:     resp.StatusCode,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
			Body:       snippet(raw),
		}
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage *usage `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		// A 200 was billed; asking again would pay twice for the same
		// fault, so this one is permanent.
		return Reply{}, fmt.Errorf("%w: decode reply: %v", errPermanent, err)
	}
	if out.Error != nil {
		// OpenRouter reports an upstream failure inside a 200. It is a
		// host fault, not a reply, and waits can clear it.
		msg := out.Error.Message
		if msg == "" {
			msg = "no message in the host's error"
		}
		return Reply{}, &Error{Status: http.StatusBadGateway, Body: snippet([]byte(msg))}
	}
	if len(out.Choices) == 0 {
		return Reply{}, &Error{Status: http.StatusBadGateway, Body: "no choices in reply"}
	}
	r := Reply{Text: out.Choices[0].Message.Content}
	if out.Usage.usable() {
		r.InputTokens = out.Usage.PromptTokens
		r.OutputTokens = out.Usage.CompletionTokens
		r.Price = c.Price(r.InputTokens, r.OutputTokens)
		r.Measured = true
	}
	return r, nil
}

// usage is the token report some hosts omit or leave empty. Reading it
// shallowly is enough: both hosts put the counts at the top level.
type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// usable reports whether the host reported a token count worth pricing. An
// absent block, a negative count and an all-zero block cannot be told apart
// from a host that reported nothing, and pricing them at zero would record a
// paid call as free.
func (u *usage) usable() bool {
	return u != nil && u.PromptTokens >= 0 && u.CompletionTokens >= 0 &&
		(u.PromptTokens > 0 || u.CompletionTokens > 0)
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if s, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
