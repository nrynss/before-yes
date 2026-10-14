//go:build live

// Live probe for the model client. It never runs in the gate or in CI: the
// live tag keeps it out. Run it with
//
//	go test -tags live -v -count=1 -run TestLive ./internal/model/
//
// Each host is read from its own environment variable and skipped when that
// variable is empty, so a machine holding one key probes that host alone. No
// key value is printed: the probe logs what a host answered and nothing from
// the request.
//
// Token Factory is the only paid host. The probe caps its own spend at
// liveBudget and stops before a call that would cross it.
package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/keel/cost"
)

// livePrompt is one line, and its answer is short when thinking is off and
// long when it is on, which is what makes the two token counts comparable.
// A prompt the model can answer without counting shows almost no gap.
const livePrompt = "How many integers from 1 to 300 are divisible by neither 3 nor 7? " +
	"Reply with only the number, no explanation."

// liveMaxTokens caps every reply the probe asks for. It is high enough to let
// a thinking reply show its extra output tokens and low enough to keep the
// paid total tiny.
const liveMaxTokens = 512

// liveBudget caps Token Factory spend for the whole probe, guard and all.
var liveBudget = cost.USD(0.05)

// liveRole is one model role on one host: the id the host serves it under, its
// rate card, and the pace to start calls at.
type liveRole struct {
	name      string
	model     string
	inPerM    float64
	outPerM   float64
	perMinute int
}

// liveHost is one host the probe covers.
type liveHost struct {
	name    string
	env     string
	baseURL string
	// paid marks the one host that bills, so the budget guard applies to it.
	paid bool
	// fast and think are the host's confirmed thinking switches, merged into
	// the request body. fast is the switch the example settings file carries
	// for the judge's main pass; think is the conflict pass's.
	fast  map[string]any
	think map[string]any
	// refused are ids the probe re-checks because a completion for them
	// answered a refusal: the lowercase spelling the example settings file
	// used to carry, and an id build.nvidia.com lists but does not run.
	// Empty when there is nothing to re-check.
	refused []string
}

var liveHosts = []liveHost{
	{
		name:    "Token Factory",
		env:     "NEBIUS_KEY",
		baseURL: "https://api.tokenfactory.nebius.com/v1",
		paid:    true,
		// The id's spelling is the host's, capitals and all; the lowercase
		// spelling the example file carried answers 404.
		refused: []string{"nvidia/nvidia-nemotron-3-nano-30b-a3b"},
		fast:    map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": false}},
		think:   map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": true}},
	},
	{
		name:    "OpenRouter",
		env:     "OPENROUTER_KEY",
		baseURL: "https://openrouter.ai/api/v1",
		fast:    map[string]any{"reasoning": map[string]any{"enabled": false}},
		think:   map[string]any{"reasoning": map[string]any{"enabled": true}},
	},
	{
		name:    "build.nvidia.com",
		env:     "NVIDIA_KEY",
		baseURL: "https://integrate.api.nvidia.com/v1",
		// The catalogue lists the Nano 30B-A3B, and a completion for it
		// answers "Function ... not found for account", which is why this
		// host's reader is the Lightning 30B-A3B.
		refused: []string{"nvidia/nemotron-nano-3-30b-a3b"},
		fast:    map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": false}},
		think:   map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": true}},
	},
}

// roles returns the two roles as the example settings file configures them for
// this host. The reader id is the model the host serves, which is not always
// the id the brief names: the Nano 30B-A3B the brief wants 404s on
// build.nvidia.com, whose nearest reader is the Lightning 30B-A3B the
// OpenRouter stand-in also names.
func (h liveHost) roles() []liveRole {
	switch h.name {
	case "Token Factory":
		return []liveRole{
			{name: "reader", model: "nvidia/NVIDIA-Nemotron-3-Nano-30B-A3B",
				inPerM: 0.06, outPerM: 0.24, perMinute: 100},
			{name: "judge", model: "nvidia/nemotron-3-super-120b-a12b",
				inPerM: 0.30, outPerM: 0.90, perMinute: 300},
		}
	case "OpenRouter":
		return []liveRole{
			{name: "reader", model: "nvidia/nemotron-3.5-lightning:free", perMinute: 20},
			{name: "judge", model: "nvidia/nemotron-3-super-120b-a12b:free", perMinute: 20},
		}
	default:
		return []liveRole{
			{name: "reader", model: "nvidia/nemotron-3.5-lightning-30b-a3b", perMinute: 20},
			{name: "judge", model: "nvidia/nemotron-3-super-120b-a12b", perMinute: 20},
		}
	}
}

// role returns the named role, which the probe's order names.
func (h liveHost) role(name string) liveRole {
	for _, r := range h.roles() {
		if r.name == name {
			return r
		}
	}
	panic("live: no role named " + name)
}

// liveCandidates are the request fields the probe tries as a thinking switch,
// each off and on. The field that moves the output-token count is that host's
// switch; the fields that leave it alone are recorded as rejected. A field
// that moves the count by a token or two is noise, so the verdict uses the
// same margin the declared switch is held to.
var liveCandidates = []struct {
	field string
	off   map[string]any
	on    map[string]any
}{
	{"chat_template_kwargs.enable_thinking",
		map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": false}},
		map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": true}}},
	{"reasoning.enabled",
		map[string]any{"reasoning": map[string]any{"enabled": false}},
		map[string]any{"reasoning": map[string]any{"enabled": true}}},
	{"reasoning_effort",
		map[string]any{"reasoning_effort": "none"},
		map[string]any{"reasoning_effort": "high"}},
	{"thinking.type",
		map[string]any{"thinking": map[string]any{"type": "disabled"}},
		map[string]any{"thinking": map[string]any{"type": "enabled"}}},
}

// movesThinking reports whether a pair of counts is the gap the task claims:
// thinking on many more output tokens than off, on the same prompt.
func movesThinking(on, off int) bool {
	return on >= off+20 && on >= 2*off
}

// switchVerdict names what a candidate switch did. A switch that cuts the
// output tokens but leaves the thinking-off reply empty has not turned thinking
// off, it has broken the answer, so the token gap alone never confirms one. The
// thinking-on reply may be empty at the probe's small token cap (a thinking
// reply can be all reasoning), so only the off reply is held to having text.
func switchVerdict(on, off Reply) string {
	if !movesThinking(on.OutputTokens, off.OutputTokens) {
		return "no effect"
	}
	if strings.TrimSpace(off.Text) == "" {
		return "breaks the answer (fewer tokens but empty reply text)"
	}
	return "moves thinking"
}

func TestLiveHosts(t *testing.T) {
	for _, h := range liveHosts {
		h := h
		t.Run(h.name, func(t *testing.T) {
			key := os.Getenv(h.env)
			if key == "" {
				t.Skipf("%s is empty, so %s is not probed", h.env, h.name)
			}
			// Eight minutes leaves the go test default ten-minute timeout
			// room to report the probe's own failure rather than being
			// killed by it.
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
			defer cancel()
			// No client-wide timeout: raw gives each request the deadline
			// its max tokens earn, as the production client does.
			p := &liveProbe{t: t, host: h, key: key, http: &http.Client{}}
			p.listModels(ctx)
			p.refusedIDs(ctx)
			p.roleCompletion(ctx, h.role("reader"))
			p.roleCompletion(ctx, h.role("judge"))
			// The reader's sweep finds the host's switch. The judge's
			// model is a different one, so its own declared switch is held
			// to the same claim.
			p.thinkingSweep(ctx, h.role("reader"))
			p.declaredSwitch(ctx, h.role("judge"))
			p.rateLimits(ctx)
			p.spend()
		})
	}
}

// liveProbe carries one host's run: the key, the running spend, and the last
// response's headers.
type liveProbe struct {
	t       *testing.T
	host    liveHost
	key     string
	http    *http.Client
	total   cost.Price
	headers http.Header
}

// client returns a client for one role, configured as the example settings
// file configures it. The key is a function, as a rotated key would be.
func (p *liveProbe) client(r liveRole) *Client {
	p.t.Helper()
	c, err := New(Settings{
		Provider:   p.host.name,
		BaseURL:    p.host.baseURL,
		Model:      r.model,
		InputPerM:  r.inPerM,
		OutputPerM: r.outPerM,
		MaxTokens:  liveMaxTokens,
		PerMinute:  r.perMinute,
	}, func() (string, error) { return p.key, nil })
	if err != nil {
		p.t.Fatalf("%s: %s", r.model, scrubErr(err))
	}
	return c
}

// listModels records the exact ids the host serves, and checks that the two
// the example settings file carries are among them.
func (p *liveProbe) listModels(ctx context.Context) {
	p.t.Helper()
	raw, status, err := p.raw(ctx, 0, http.MethodGet, "/models", nil)
	if err != nil {
		p.t.Fatalf("GET /v1/models: %v", err)
	}
	if status != http.StatusOK {
		p.t.Fatalf("GET /v1/models: HTTP %d (%d-byte body withheld)", status, len(raw))
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		p.t.Fatalf("GET /v1/models: decode: %v", err)
	}
	all := make([]string, 0, len(out.Data))
	nemotron := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		all = append(all, m.ID)
		if strings.Contains(strings.ToLower(m.ID), "nemotron") {
			nemotron = append(nemotron, m.ID)
		}
	}
	sort.Strings(nemotron)
	p.t.Logf("GET /v1/models: %d models, %d Nemotron ids:\n%s",
		len(all), len(nemotron), strings.Join(nemotron, "\n"))
	for _, r := range p.host.roles() {
		if !slices.Contains(all, r.model) {
			p.t.Errorf("GET /v1/models does not list %s's id %q", r.name, r.model)
		}
	}
}

// refusedIDs records what the host answers for the ids worth re-checking: the
// spelling the example settings file used to carry, and an id
// build.nvidia.com lists but does not run. It is not an assertion: a host may
// change either answer later, so the status is evidence for the file as it
// stands rather than a fact the probe must hold. A refusal is not billed.
func (p *liveProbe) refusedIDs(ctx context.Context) {
	p.t.Helper()
	for _, id := range p.host.refused {
		body := map[string]any{
			"model":       id,
			"messages":    []Message{{Role: "user", Content: "Reply with the single word: ok"}},
			"max_tokens":  8,
			"temperature": 0,
		}
		for k, v := range p.host.fast {
			body[k] = v
		}
		payload, err := json.Marshal(body)
		if err != nil {
			p.t.Fatalf("encode: %v", err)
		}
		_, status, err := p.raw(ctx, 8, http.MethodPost, "/chat/completions", payload)
		if err != nil {
			p.t.Fatalf("refused id %q: %s", id, scrubErr(err))
		}
		// The host's refusal body names the account, so the log carries the
		// status and a fixed reason, never the body.
		p.t.Logf("id %q answered HTTP %d: %s", id, status, refusalReason(status))
	}
}

// refusalReason is the fixed text logged for a status in place of the host's
// body, which can carry an account identifier.
// scrubErr renders an error for the probe's log without a host's response
// body. A *Error carries the body, which can name the account, so only its
// status is logged. Any other error has no body and is logged as is.
func scrubErr(err error) string {
	var he *Error
	if errors.As(err, &he) {
		return fmt.Sprintf("HTTP %d (body withheld)", he.Status)
	}
	return err.Error()
}

func refusalReason(status int) string {
	switch {
	case status == http.StatusNotFound:
		return "id not found or not available to this account (body withheld)"
	case status >= 200 && status < 300:
		return "id accepted (body withheld)"
	default:
		return "refused (body withheld)"
	}
}

// roleCompletion is one short completion per role, with the host's thinking
// switch off, recording the reported usage and the price the client computes
// from it.
func (p *liveProbe) roleCompletion(ctx context.Context, r liveRole) {
	p.t.Helper()
	reply := p.chat(ctx, r, p.host.fast, fmt.Sprintf("%s completion", r.name))
	if reply.OutputTokens == 0 {
		p.t.Errorf("%s: the host reported no output tokens", r.name)
	}
	if !reply.Measured {
		p.t.Errorf("%s: the host reported no usable usage, so the price is the estimate, not a measurement", r.name)
	}
}

// thinkingSweep tries each candidate field off and on for the reader and
// records the output-token counts, then holds the host's declared switch to
// the task's claim. It is where the host's switch field is found: the field
// that moves the count is the one the example settings file must carry.
func (p *liveProbe) thinkingSweep(ctx context.Context, r liveRole) {
	p.t.Helper()
	base := p.chat(ctx, r, nil, r.name+" default (no switch)")
	p.t.Logf("%s: no switch sent: %d output tokens", r.name, base.OutputTokens)

	for _, c := range liveCandidates {
		off := p.chat(ctx, r, c.off, r.name+" "+c.field+" off")
		on := p.chat(ctx, r, c.on, r.name+" "+c.field+" on")
		verdict := switchVerdict(on, off)
		p.t.Logf("%s: %s off=%d on=%d output tokens: %s",
			r.name, c.field, off.OutputTokens, on.OutputTokens, verdict)
	}
	p.declaredSwitch(ctx, r)
}

// declaredSwitch holds the host's confirmed thinking switch to the task's
// claim: thinking on must report many more output tokens than thinking off on
// the same prompt.
func (p *liveProbe) declaredSwitch(ctx context.Context, r liveRole) {
	p.t.Helper()
	off := p.chat(ctx, r, p.host.fast, r.name+" declared switch off")
	on := p.chat(ctx, r, p.host.think, r.name+" declared switch on")
	if on.OutputTokens < off.OutputTokens+20 || on.OutputTokens < 2*off.OutputTokens {
		p.t.Errorf("%s: the declared switch reports %d output tokens thinking on and %d thinking off, "+
			"so thinking is not the difference the task claims", r.name, on.OutputTokens, off.OutputTokens)
	}
	if strings.TrimSpace(off.Text) == "" {
		p.t.Errorf("%s: the declared switch cuts output tokens but the thinking-off reply text is empty, "+
			"so it breaks the answer rather than turning thinking off", r.name)
	}
	p.t.Logf("%s: declared switch off=%d on=%d output tokens (%.1fx)",
		r.name, off.OutputTokens, on.OutputTokens, ratio(on.OutputTokens, off.OutputTokens))
}

// rateLimits makes one small raw call per role and records the host's
// rate-limit headers. They are where the per-minute pacing value comes from,
// and two roles' separate counters say whether the cap is per model or per
// account.
func (p *liveProbe) rateLimits(ctx context.Context) {
	p.t.Helper()
	for _, r := range p.host.roles() {
		msgs := []Message{{Role: "user", Content: "Reply with the single word: ok"}}
		o := Options{Extra: p.host.fast, MaxTokens: 8}
		c := p.client(r)
		p.guard(c, msgs, o, r.name+" rate-limit call")
		body := map[string]any{
			"model":       r.model,
			"messages":    msgs,
			"max_tokens":  o.MaxTokens,
			"temperature": 0,
		}
		for k, v := range o.Extra {
			body[k] = v
		}
		payload, err := json.Marshal(body)
		if err != nil {
			p.t.Fatalf("encode: %v", err)
		}
		raw, status, err := p.raw(ctx, o.MaxTokens, http.MethodPost, "/chat/completions", payload)
		if err != nil {
			p.t.Fatalf("%s rate limits: %s", r.name, scrubErr(err))
		}
		var out struct {
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			p.t.Logf("%s rate-limit call: HTTP %d, undecodable %d-byte body (withheld)", r.name, status, len(raw))
			p.t.Logf("%s rate-limit headers: %s", r.name, p.rateLimitLine())
			continue
		}
		in, outTokens := 0, 0
		if out.Usage != nil {
			in, outTokens = out.Usage.PromptTokens, out.Usage.CompletionTokens
		}
		if in > 0 || outTokens > 0 {
			price := c.Price(in, outTokens)
			p.total += price
			p.t.Logf("%s rate-limit call: HTTP %d, %d in / %d out tokens, price %s",
				r.name, status, in, outTokens, price)
		}
		p.t.Logf("%s rate-limit headers: %s", r.name, p.rateLimitLine())
	}
}

// rateLimitLine formats the last response's rate-limit headers.
func (p *liveProbe) rateLimitLine() string {
	if p.headers == nil {
		return "(none)"
	}
	keys := []string{}
	for k := range p.headers {
		if strings.HasPrefix(strings.ToLower(k), "x-ratelimit") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+p.headers.Get(k))
	}
	if len(parts) == 0 {
		return "(no x-ratelimit headers)"
	}
	return strings.Join(parts, " ")
}

// guard stops a paid host's next call before it is made when the probe's total
// plus the call's estimate would cross the cap.
func (p *liveProbe) guard(c *Client, msgs []Message, o Options, label string) {
	p.t.Helper()
	if !p.host.paid {
		return
	}
	if est := c.Estimate(msgs, o); p.total+est > liveBudget {
		p.t.Fatalf("stopping before %s: %s spent plus a %s estimate would cross the %s cap",
			label, p.total, est, liveBudget)
	}
}

// chat makes one call through the client, charges what it cost and logs what
// the host reported. extra is the thinking switch for the call, or nil.
func (p *liveProbe) chat(ctx context.Context, r liveRole, extra map[string]any, label string) Reply {
	p.t.Helper()
	c := p.client(r)
	msgs := []Message{{Role: "user", Content: livePrompt}}
	o := Options{Extra: extra, MaxTokens: liveMaxTokens}
	p.guard(c, msgs, o, label)
	start := time.Now()
	reply, err := c.Chat(ctx, msgs, o)
	elapsed := time.Since(start)
	if err != nil {
		p.t.Fatalf("%s: %s", label, scrubErr(err))
	}
	if reply.Measured {
		p.total += reply.Price
	}
	text := strings.TrimSpace(reply.Text)
	if len(text) > 40 {
		text = text[:40] + "…"
	}
	p.t.Logf("%s: %d in / %d out tokens in %.2fs, price %s, measured=%v, text %q",
		label, reply.InputTokens, reply.OutputTokens, elapsed.Seconds(),
		reply.Price, reply.Measured, text)
	return reply
}

// spend logs the host's total. It fails a paid host that has crossed the cap,
// which the guard should have stopped before.
func (p *liveProbe) spend() {
	p.t.Helper()
	p.t.Logf("%s: total spend this probe %s", p.host.name, p.total)
	if p.host.paid && p.total > liveBudget {
		p.t.Errorf("%s: spent %s, over the %s cap", p.host.name, p.total, liveBudget)
	}
}

// raw sends one request straight to the host. This is the only place the probe
// sends a request itself; chat goes through Client.Chat, which sets the same
// deadline in post. The request's deadline is DefaultTimeouts.For(maxTokens),
// with maxTokens 0 for a request that has none, such as a model list, and it
// sits inside ctx, the probe's overall timeout. The headers are kept, because
// the client does not expose them, and the body is returned for the caller to
// read.
func (p *liveProbe) raw(ctx context.Context, maxTokens int, method, path string, body []byte) ([]byte, int, error) {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeouts.For(maxTokens))
	defer cancel()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method,
		strings.TrimRight(p.host.baseURL, "/")+path, rdr)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.key)
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, 0, err
	}
	p.headers = resp.Header
	return raw, resp.StatusCode, nil
}

func ratio(on, off int) float64 {
	if off <= 0 {
		return 0
	}
	return float64(on) / float64(off)
}
