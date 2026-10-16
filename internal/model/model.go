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
	// which differs between hosts. New and Settings copy it, nested maps
	// included.
	Extra map[string]any `toml:"extra"`
}

// Timeouts sets how long one call may wait for its reply. The deadline is
// scaled to the call's max tokens, because a thinking call that writes 16,000
// tokens runs far longer than a short label. For a call with n max tokens:
//
//	deadline = clamp(Slack + n / MinTokensPerSecond, Floor, Ceiling)
//
// MinTokensPerSecond is a rate below the slowest measured output rate of any
// host, so a call at its cap is not cut while the host is still writing it.
// Slack covers queueing and the prompt. Floor keeps a short call from getting
// a deadline that a slow first byte could miss. Ceiling bounds a call whose
// max tokens are set very high.
type Timeouts struct {
	Floor              time.Duration
	Ceiling            time.Duration
	Slack              time.Duration
	MinTokensPerSecond float64
}

// DefaultTimeouts are the production bounds. The slowest measured rate is 35
// tokens a second (OpenRouter's free judge, T2.1b), so 30 leaves margin. The
// measurements are in dev-diary/PHASE-2-clients.md under T2.1c.
var DefaultTimeouts = Timeouts{
	Floor:              90 * time.Second,
	Ceiling:            15 * time.Minute,
	Slack:              60 * time.Second,
	MinTokensPerSecond: 30,
}

// For returns the deadline for a call that may write maxTokens tokens.
func (t Timeouts) For(maxTokens int) time.Duration {
	if maxTokens < 0 {
		maxTokens = 0
	}
	d := t.Slack + time.Duration(float64(maxTokens)/t.MinTokensPerSecond*float64(time.Second))
	return min(max(d, t.Floor), t.Ceiling)
}

// Client calls one model.
type Client struct {
	s    Settings
	key  func() (string, error)
	http *http.Client
	// Retry tunes the attempts. Tests shorten it.
	Retry throttle.Config
	// Timeouts sets the per-call deadline. Tests shorten it.
	Timeouts Timeouts
}

// errTimeout marks a call that got no reply before its deadline. The host may
// have billed the tokens it wrote before the deadline, so Chat prices such a
// call at the estimate and never at zero.
var errTimeout = errors.New("model: no reply before the call's deadline")

// New returns a client for s. key is read on every call, so a rotated secret
// takes effect without a restart. A nil key reads as no key at all.
//
// Extra is copied, and every nested map[string]any inside it, so a caller
// that keeps the map it passed in cannot change a later request through it.
// A map reached through any other container — inside a slice, or a
// map[string]string — is kept as it is, so do not mutate one after New.
func New(s Settings, key func() (string, error)) (*Client, error) {
	if s.BaseURL == "" || s.Model == "" {
		return nil, errors.New("model: base_url and model are required")
	}
	if s.MaxTokens <= 0 {
		s.MaxTokens = DefaultMaxTokens
	}
	s.Extra = cloneExtra(s.Extra)
	if key == nil {
		key = func() (string, error) { return "", nil }
	}
	return &Client{
		s:   s,
		key: key,
		// No client-wide timeout: each attempt gets the deadline its call's
		// max tokens earn, set in post.
		http:     &http.Client{},
		Timeouts: DefaultTimeouts,
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

// Settings returns the role's settings. Extra is copied, and every nested
// map[string]any inside it, so nothing a caller does to the result can change
// the client; a map reached through any other container is shared, as in New.
func (c *Client) Settings() Settings {
	s := c.s
	s.Extra = cloneExtra(s.Extra)
	return s
}

// cloneExtra copies m so a caller and the client cannot reach each other
// through it. Nested map[string]any values — the thinking-switch objects
// hosts take — are copied all the way down. Values of any other type,
// slices included, are copied as they are. A map reached through any other
// container — a map[string]string, or a map inside a slice — is stored as
// the caller's own map, so a caller must not mutate one after New or
// Settings.
func cloneExtra(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneExtraValue(v)
	}
	return out
}

func cloneExtraValue(v any) any {
	if nested, ok := v.(map[string]any); ok {
		return cloneExtra(nested)
	}
	return v
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
	// FinishReason is the host's finish_reason for the reply ("stop",
	// "length", ...), or "" when the host sent none or the reply was replayed
	// from a recording that predates the field.
	FinishReason string
}

// Capped reports whether the reply stopped at its max-token cap: the host said
// "length", or it said nothing and the output tokens reached max. A capped
// reply may end mid-thought, with no answer in it.
func (r Reply) Capped(max int) bool {
	if r.FinishReason != "" {
		return r.FinishReason == "length"
	}
	return max > 0 && r.OutputTokens >= max
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
//
// The call's wait is bounded by Timeouts.For(max tokens). A call that gets no
// reply before that deadline returns errTimeout (it matches
// context.DeadlineExceeded) with an unmeasured Reply priced at the estimate.
// The host may have billed the tokens it wrote, so a caller that settles on
// error must book that price, not zero.
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
	maxTokens := c.s.MaxTokens
	if o.MaxTokens > 0 {
		maxTokens = o.MaxTokens
	}
	deadline := c.Timeouts.For(maxTokens)
	cfg := c.Retry
	cfg.Transient = transient
	cfg.RetryAfter = retryAfter
	var reply Reply
	err = throttle.Retry(ctx, cfg, Retryable, func() error {
		r, err := c.post(ctx, payload, deadline)
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
		if errors.Is(err, errTimeout) {
			// No reply came back, so there is no usage to price. The host
			// may still have billed the tokens it wrote before the
			// deadline, so the call is settled at the estimate it was
			// reserved at, unmeasured, and the error still reports the
			// timeout. A caller that settles on error must book this price.
			return Reply{Price: estimate}, err
		}
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

// post makes one attempt. The attempt's deadline is d, and it ends early if
// ctx does.
func (c *Client) post(ctx context.Context, payload []byte, d time.Duration) (Reply, error) {
	key, err := c.key()
	if err != nil {
		return Reply{}, fmt.Errorf("%w: key: %v", errPermanent, err)
	}
	call, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	req, err := http.NewRequestWithContext(call, http.MethodPost,
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
		return Reply{}, deadlined(ctx, call, d, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return Reply{}, deadlined(ctx, call, d, err)
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
			FinishReason string `json:"finish_reason"`
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
		// Unlike an undecodable 200, this body parsed, so the task's
		// rule against retrying a billed 200 does not cover it: it is a
		// synthetic 502 on the smaller two-attempt host-fault budget.
		// That budget still retries once, and the retry may be a second
		// billed call, so the task's own reason for the rule ("That 200
		// was billed") applies here too. Bounded to one extra call; if a
		// host is seen answering this way (T2.1b), empty choices should
		// be permanent.
		return Reply{}, &Error{Status: http.StatusBadGateway, Body: "no choices in reply"}
	}
	r := Reply{Text: out.Choices[0].Message.Content, FinishReason: out.Choices[0].FinishReason}
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

// deadlined reports a failure caused by the attempt's own deadline as
// errTimeout. It wraps context.DeadlineExceeded too, so Retryable does not
// retry it: the host may still be writing the reply, and a second call would
// bill twice. A failure from the caller's own context, or from anywhere else,
// is returned as it came.
func deadlined(ctx, call context.Context, d time.Duration, err error) error {
	if ctx.Err() == nil && errors.Is(call.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w after %v: %w", errTimeout, d, context.DeadlineExceeded)
	}
	return err
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
