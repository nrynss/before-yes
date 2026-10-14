package model

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nrynss/keel/cost"
)

// The Token Factory rates for Nemotron 3 Nano, dollars per million tokens.
const (
	nanoInPerM  = 0.06
	nanoOutPerM = 0.24
)

func keyFunc(k string) func() (string, error) {
	return func() (string, error) { return k, nil }
}

// fast shortens the retry waits, so a test exercises attempt counts and not
// the clock.
func fast(c *Client) {
	c.Retry.Backoff = time.Millisecond
	c.Retry.Max = time.Millisecond
	c.Retry.WaitBudget = 0
	c.Retry.Name = ""
}

func TestChatPricesReportedUsageAtConfiguredRates(t *testing.T) {
	var got map[string]any
	var path, auth, ctype string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth, ctype = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"hi"}}],`+
			`"usage":{"prompt_tokens":1000000,"completion_tokens":500000,"total_tokens":1500000}}`)
	}))
	defer srv.Close()

	c, err := New(Settings{
		Provider:   "tokenfactory",
		BaseURL:    srv.URL + "/v1/",
		Model:      "nano",
		InputPerM:  nanoInPerM,
		OutputPerM: nanoOutPerM,
	}, keyFunc("k"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "hi" {
		t.Errorf("text %q, want %q", r.Text, "hi")
	}
	if !r.Measured {
		t.Error("the host reported usage, so the reply is measured")
	}
	if r.InputTokens != 1000000 || r.OutputTokens != 500000 {
		t.Errorf("tokens %d in, %d out; want 1000000 and 500000", r.InputTokens, r.OutputTokens)
	}
	// A million input tokens at $0.06 and half a million output at $0.24.
	if want := cost.USD(0.06 + 0.12); r.Price != want {
		t.Errorf("price %v, want %v", r.Price, want)
	}
	if want := c.Price(1000000, 500000); r.Price != want {
		t.Errorf("price %v disagrees with the rate card's %v", r.Price, want)
	}
	if path != "/v1/chat/completions" {
		t.Errorf("path %q, want /v1/chat/completions", path)
	}
	if auth != "Bearer k" {
		t.Errorf("authorization %q, want Bearer k", auth)
	}
	if ctype != "application/json" {
		t.Errorf("content-type %q, want application/json", ctype)
	}
	if got["model"] != "nano" {
		t.Errorf("body model %v, want nano", got["model"])
	}
}

func TestReplyWithoutUsableUsageIsUnmeasured(t *testing.T) {
	bodies := map[string]string{
		"no usage member":     `{"choices":[{"message":{"content":"hi"}}]}`,
		"empty usage block":   `{"choices":[{"message":{"content":"hi"}}],"usage":{}}`,
		"all-zero token pair": `{"choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":0,"completion_tokens":0}}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, body)
			}))
			defer srv.Close()

			c, err := New(Settings{BaseURL: srv.URL, Model: "m", InputPerM: nanoInPerM, OutputPerM: nanoOutPerM}, keyFunc("k"))
			if err != nil {
				t.Fatal(err)
			}
			msgs := []Message{{Role: "user", Content: "hello there"}}
			o := Options{MaxTokens: 100}
			r, err := c.Chat(context.Background(), msgs, o)
			if err != nil {
				t.Fatal(err)
			}
			if r.Measured {
				t.Error("no usable usage was reported, so the reply must be unmeasured")
			}
			if want := c.Estimate(msgs, o); r.Price != want || r.Price <= 0 {
				t.Errorf("price %v, want the estimate %v", r.Price, want)
			}
		})
	}
}

func TestExtraIsMergedAndPerCallOverrideLeavesSettingsUntouched(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Errorf("decode request: %v", err)
		}
		bodies = append(bodies, b)
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	c, err := New(Settings{
		Provider:  "tokenfactory",
		BaseURL:   srv.URL,
		Model:     "nano",
		MaxTokens: 128,
		Extra: map[string]any{
			"chat_template_kwargs": map[string]any{"enable_thinking": false},
			"top_k":                40,
		},
	}, keyFunc("k"))
	if err != nil {
		t.Fatal(err)
	}
	msgs := []Message{{Role: "user", Content: "hello"}}
	if _, err := c.Chat(context.Background(), msgs, Options{}); err != nil {
		t.Fatal(err)
	}
	// The judge's conflict pass turns thinking on for one call, and only
	// that call.
	if _, err := c.Chat(context.Background(), msgs, Options{
		Extra:     map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": true}},
		MaxTokens: 512,
	}); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 {
		t.Fatalf("sent %d bodies, want 2", len(bodies))
	}
	if got := thinkingSwitch(t, bodies[0]); got {
		t.Error("first call turned thinking on; the settings turn it off")
	}
	if got := thinkingSwitch(t, bodies[1]); !got {
		t.Error("second call left thinking off; the per-call option turns it on")
	}
	if got := bodies[1]["top_k"]; got != float64(40) {
		t.Errorf("sibling extra key lost by the override: top_k %v, want 40", got)
	}
	if got := bodies[0]["max_tokens"]; got != float64(128) {
		t.Errorf("first call max_tokens %v, want the settings' 128", got)
	}
	if got := bodies[1]["max_tokens"]; got != float64(512) {
		t.Errorf("second call max_tokens %v, want the option's 512", got)
	}
	if got := thinkingSwitch(t, c.Settings().Extra); got {
		t.Error("the settings' thinking switch changed, so a call mutated them")
	}
	// A caller that edits what Settings returned must not edit the client.
	s := c.Settings()
	s.Extra["top_k"] = 999
	if got := c.Settings().Extra["top_k"]; got != 40 {
		t.Errorf("Settings returned the live Extra map: top_k %v, want 40", got)
	}
}

func TestExtraNestedMapsAreNotSharedWithCallers(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Errorf("decode request: %v", err)
		}
		bodies = append(bodies, b)
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	// The caller keeps the map it hands to New, and the client must not
	// see later changes to it: a top-level key, the thinking switch, and
	// a map nested one level below the switch.
	extra := map[string]any{
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
		"nested":               map[string]any{"deeper": map[string]any{"level": 1}},
	}
	c, err := New(Settings{Provider: "tokenfactory", BaseURL: srv.URL, Model: "nano", Extra: extra}, keyFunc("k"))
	if err != nil {
		t.Fatal(err)
	}
	msgs := []Message{{Role: "user", Content: "hello"}}
	chat := func() {
		t.Helper()
		if _, err := c.Chat(context.Background(), msgs, Options{}); err != nil {
			t.Fatal(err)
		}
	}
	chat()

	extra["top_level_added"] = true
	extra["chat_template_kwargs"].(map[string]any)["enable_thinking"] = true
	extra["nested"].(map[string]any)["deeper"].(map[string]any)["level"] = 999
	chat()

	// The same for the map Settings returns.
	s := c.Settings()
	s.Extra["settings_added"] = true
	s.Extra["chat_template_kwargs"].(map[string]any)["enable_thinking"] = true
	s.Extra["nested"].(map[string]any)["deeper"].(map[string]any)["level"] = 999
	chat()

	if len(bodies) != 3 {
		t.Fatalf("sent %d bodies, want 3", len(bodies))
	}
	for i, b := range bodies {
		if got := thinkingSwitch(t, b); got {
			t.Errorf("body %d: thinking is on, so a caller's mutation reached the request", i)
		}
		nested, ok := b["nested"].(map[string]any)
		if !ok {
			t.Fatalf("body %d: no nested map in %v", i, b)
		}
		deeper, ok := nested["deeper"].(map[string]any)
		if !ok {
			t.Fatalf("body %d: no nested.deeper in %v", i, nested)
		}
		if got := deeper["level"]; got != float64(1) {
			t.Errorf("body %d: nested.deeper.level %v, want 1", i, got)
		}
		if _, ok := b["top_level_added"]; ok {
			t.Errorf("body %d carries a key added to the map given to New", i)
		}
		if _, ok := b["settings_added"]; ok {
			t.Errorf("body %d carries a key added to the settings copy", i)
		}
	}

	// A per-call map is not reachable: Chat marshals it into the request
	// it is making and keeps no reference, so mutating one after the call
	// cannot reach a later body. A key only the per-call map carries must
	// appear in its own request and in no later one.
	perCall := map[string]any{
		"chat_template_kwargs": map[string]any{"enable_thinking": true},
		"per_call_marker":      "original",
	}
	if _, err := c.Chat(context.Background(), msgs, Options{Extra: perCall}); err != nil {
		t.Fatal(err)
	}
	perCall["per_call_marker"] = "mutated"
	perCall["chat_template_kwargs"].(map[string]any)["enable_thinking"] = false
	chat()
	if len(bodies) != 5 {
		t.Fatalf("sent %d bodies, want 5", len(bodies))
	}
	if got := thinkingSwitch(t, bodies[3]); !got {
		t.Error("the per-call override did not turn thinking on")
	}
	if got := bodies[3]["per_call_marker"]; got != "original" {
		t.Errorf("the per-call override did not reach its own request: marker %v", got)
	}
	if got, ok := bodies[4]["per_call_marker"]; ok {
		t.Errorf("the per-call map reached a later request: marker %v", got)
	}
}

// thinkingSwitch reads the Nemotron thinking switch out of a request body or
// a settings Extra map.
func thinkingSwitch(t *testing.T, body map[string]any) bool {
	t.Helper()
	kwargs, ok := body["chat_template_kwargs"].(map[string]any)
	if !ok {
		t.Fatalf("no chat_template_kwargs in %v", body)
	}
	v, ok := kwargs["enable_thinking"].(bool)
	if !ok {
		t.Fatalf("no enable_thinking in %v", kwargs)
	}
	return v
}

func TestDefaultMaxTokensAndJSONResponseFormat(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"{}"}}]}`)
	}))
	defer srv.Close()

	c, err := New(Settings{BaseURL: srv.URL, Model: "m"}, keyFunc("k"))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Settings().MaxTokens; got != DefaultMaxTokens {
		t.Errorf("settings max_tokens %d, want the default %d", got, DefaultMaxTokens)
	}
	if _, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "x"}}, Options{JSON: true}); err != nil {
		t.Fatal(err)
	}
	if got := got["max_tokens"]; got != float64(DefaultMaxTokens) {
		t.Errorf("body max_tokens %v, want %d", got, DefaultMaxTokens)
	}
	format, ok := got["response_format"].(map[string]any)
	if !ok || format["type"] != "json_object" {
		t.Errorf("response_format %v, want a json_object request", got["response_format"])
	}
}

func TestKeyIsReadOnEveryCall(t *testing.T) {
	var calls atomic.Int32
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	keys := []string{"first", "second"}
	c, err := New(Settings{BaseURL: srv.URL, Model: "m"}, func() (string, error) {
		return keys[calls.Add(1)-1], nil
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs := []Message{{Role: "user", Content: "x"}}
	for range 2 {
		if _, err := c.Chat(context.Background(), msgs, Options{}); err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("key read %d times, want once per call", got)
	}
	if len(auths) != 2 || auths[0] != "Bearer first" || auths[1] != "Bearer second" {
		t.Errorf("authorization headers %q, want a rotated key to take effect", auths)
	}
}

func TestKeyErrorIsPermanentAndNeverSends(t *testing.T) {
	var sent atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	c, err := New(Settings{BaseURL: srv.URL, Model: "m"}, func() (string, error) {
		return "", errors.New("key store unavailable")
	})
	if err != nil {
		t.Fatal(err)
	}
	fast(c)
	_, err = c.Chat(context.Background(), []Message{{Role: "user", Content: "x"}}, Options{})
	if err == nil {
		t.Fatal("a key error must fail the call")
	}
	if got := sent.Load(); got != 0 {
		t.Errorf("sent %d requests without a key", got)
	}
	if Retryable(err) {
		t.Errorf("a key failure is permanent, but Retryable(%v) is true", err)
	}
}

func TestRateLimitIsRetriedUntilItSucceeds(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
	}))
	defer srv.Close()

	c, err := New(Settings{BaseURL: srv.URL, Model: "m", InputPerM: nanoInPerM, OutputPerM: nanoOutPerM}, keyFunc("k"))
	if err != nil {
		t.Fatal(err)
	}
	fast(c)
	r, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "x"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "ok" {
		t.Errorf("text %q after the rate limits cleared", r.Text)
	}
	if got := n.Load(); got != 3 {
		t.Errorf("sent %d requests, want 3: two refusals and the success", got)
	}
}

func TestRateLimitBudgetIsFourAttempts(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"message":"rate limited"}}`)
	}))
	defer srv.Close()

	c, err := New(Settings{BaseURL: srv.URL, Model: "m"}, keyFunc("k"))
	if err != nil {
		t.Fatal(err)
	}
	fast(c)
	_, err = c.Chat(context.Background(), []Message{{Role: "user", Content: "x"}}, Options{})
	var he *Error
	if !errors.As(err, &he) || he.Status != http.StatusTooManyRequests {
		t.Fatalf("error %v, want an HTTP 429", err)
	}
	if got := n.Load(); got != 4 {
		t.Errorf("sent %d requests, want the 4-attempt rate-limit budget", got)
	}
	if !Retryable(err) {
		t.Errorf("Retryable(%v) is false for an exhausted rate limit", err)
	}
}

func TestBadRequestIsNotRetried(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"model not found"}}`)
	}))
	defer srv.Close()

	c, err := New(Settings{BaseURL: srv.URL, Model: "m"}, keyFunc("k"))
	if err != nil {
		t.Fatal(err)
	}
	fast(c)
	_, err = c.Chat(context.Background(), []Message{{Role: "user", Content: "x"}}, Options{})
	var he *Error
	if !errors.As(err, &he) || he.Status != http.StatusBadRequest {
		t.Fatalf("error %v, want an HTTP 400", err)
	}
	if got := n.Load(); got != 1 {
		t.Errorf("sent %d requests, want 1: a bad request is permanent", got)
	}
	if Retryable(err) {
		t.Errorf("Retryable(%v) is true for a 400", err)
	}
}

func TestUpstreamFailureInsideTwoHundredIsRetried(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			// OpenRouter reports an upstream failure this way, with a 200.
			io.WriteString(w, `{"error":{"message":"upstream connect error","code":502}}`)
			return
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	c, err := New(Settings{BaseURL: srv.URL, Model: "m"}, keyFunc("k"))
	if err != nil {
		t.Fatal(err)
	}
	fast(c)
	r, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "x"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "ok" {
		t.Errorf("text %q after the upstream failure cleared", r.Text)
	}
	if got := n.Load(); got != 2 {
		t.Errorf("sent %d requests, want 2: the error-in-200 is a host fault, not a reply", got)
	}
}

func TestErrorMemberInsideTwoHundredBeatsChoices(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		// The error member makes the body a host fault even when a reply
		// came with it.
		io.WriteString(w, `{"error":{"message":"upstream connect error","code":502},`+
			`"choices":[{"message":{"content":"partial"}}]}`)
	}))
	defer srv.Close()

	c, err := New(Settings{BaseURL: srv.URL, Model: "m"}, keyFunc("k"))
	if err != nil {
		t.Fatal(err)
	}
	fast(c)
	r, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "x"}}, Options{})
	var he *Error
	if !errors.As(err, &he) || he.Status != http.StatusBadGateway {
		t.Fatalf("reply %q, error %v; want an HTTP 502", r.Text, err)
	}
	if he.Body != "upstream connect error" {
		t.Errorf("error body %q, want the host's message", he.Body)
	}
	if got := n.Load(); got != 2 {
		t.Errorf("sent %d requests, want the 2-attempt host-fault budget", got)
	}
}

func TestHostFaultsDrawOnTheSmallerBudget(t *testing.T) {
	cases := map[string]struct {
		reply    string
		code     int
		wantBody string
	}{
		"server error": {
			reply:    `{"error":{"message":"overloaded"}}`,
			code:     http.StatusServiceUnavailable,
			wantBody: `{"error":{"message":"overloaded"}}`,
		},
		"error inside a 200": {
			reply:    `{"error":{"message":"upstream connect error","code":502}}`,
			code:     http.StatusBadGateway,
			wantBody: "upstream connect error",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var n atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n.Add(1)
				if tc.code == http.StatusBadGateway {
					io.WriteString(w, tc.reply)
					return
				}
				w.WriteHeader(tc.code)
				io.WriteString(w, tc.reply)
			}))
			defer srv.Close()

			c, err := New(Settings{BaseURL: srv.URL, Model: "m"}, keyFunc("k"))
			if err != nil {
				t.Fatal(err)
			}
			fast(c)
			_, err = c.Chat(context.Background(), []Message{{Role: "user", Content: "x"}}, Options{})
			var he *Error
			if !errors.As(err, &he) || he.Status != tc.code {
				t.Fatalf("error %v, want an HTTP %d", err, tc.code)
			}
			if he.Body != tc.wantBody {
				t.Errorf("error body %q, want %q", he.Body, tc.wantBody)
			}
			if got := n.Load(); got != 2 {
				t.Errorf("sent %d requests, want the 2-attempt host-fault budget", got)
			}
			if !Retryable(err) {
				t.Errorf("Retryable(%v) is false for a host fault", err)
			}
		})
	}
}

func TestUndecodableTwoHundredIsNotRetried(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		io.WriteString(w, `{"choices":[`)
	}))
	defer srv.Close()

	c, err := New(Settings{BaseURL: srv.URL, Model: "m"}, keyFunc("k"))
	if err != nil {
		t.Fatal(err)
	}
	fast(c)
	_, err = c.Chat(context.Background(), []Message{{Role: "user", Content: "x"}}, Options{})
	if err == nil {
		t.Fatal("a reply that will not decode must fail the call")
	}
	if got := n.Load(); got != 1 {
		t.Errorf("sent %d requests, want 1: that 200 was billed", got)
	}
	if Retryable(err) {
		t.Errorf("Retryable(%v) is true for a billed 200", err)
	}
	var he *Error
	if errors.As(err, &he) {
		t.Errorf("a decode failure is not an HTTP status error: %v", he)
	}
}

func TestRetryAfterHintIsHonoured(t *testing.T) {
	var n atomic.Int32
	var waits []time.Duration
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	c, err := New(Settings{BaseURL: srv.URL, Model: "m"}, keyFunc("k"))
	if err != nil {
		t.Fatal(err)
	}
	c.Retry.Backoff = time.Millisecond
	c.Retry.Max = 30 * time.Second
	c.Retry.WaitBudget = 0
	c.Retry.Name = ""
	c.Retry.Sleep = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	r, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "x"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "ok" {
		t.Errorf("text %q after the wait", r.Text)
	}
	if got := n.Load(); got != 2 {
		t.Fatalf("sent %d requests, want 2", got)
	}
	if len(waits) != 1 {
		t.Fatalf("waited %d times, want once", len(waits))
	}
	// The hint is two seconds, plus the spread that stops a refused
	// fan-out from returning in lockstep.
	if waits[0] < 2*time.Second || waits[0] > 3*time.Second {
		t.Errorf("waited %s, want the 2s hint plus no more than a second of spread", waits[0])
	}
}

func TestParseRetryAfter(t *testing.T) {
	future := time.Now().Add(10 * time.Minute).UTC().Format(http.TimeFormat)
	cases := map[string]struct {
		in   string
		want time.Duration // exact, when positive
	}{
		"absent":    {in: ""},
		"zero":      {in: "0"},
		"seconds":   {in: "7", want: 7 * time.Second},
		"spaced":    {in: " 12 ", want: 12 * time.Second},
		"garbage":   {in: "soon"},
		"past date": {in: time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := parseRetryAfter(tc.in); got != tc.want {
				t.Errorf("parseRetryAfter(%q) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
	if got := parseRetryAfter(future); got <= 0 || got > 11*time.Minute {
		t.Errorf("parseRetryAfter(%q) = %s, want about 10 minutes", future, got)
	}
}

func TestStartsArePacedUnderThePerMinuteCap(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	// 600 starts a minute is one every 100ms, plus the pacer's margin. The
	// model name is this test's own, so its rate window is nobody else's.
	c, err := New(Settings{Provider: "test", BaseURL: srv.URL, Model: "paced-600", PerMinute: 600}, keyFunc("k"))
	if err != nil {
		t.Fatal(err)
	}
	c.Retry.Attempts = 1
	c.Retry.TransientAttempts = 1
	msgs := []Message{{Role: "user", Content: "x"}}
	start := time.Now()
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Chat(context.Background(), msgs, Options{}); err != nil {
				t.Errorf("chat: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := n.Load(); got != 2 {
		t.Fatalf("sent %d requests, want 2", got)
	}
	if d := time.Since(start); d < 90*time.Millisecond {
		t.Errorf("two starts took %s, want the second paced a start interval behind the first", d)
	}
}

func TestNewRequiresBaseURLAndModel(t *testing.T) {
	cases := map[string]Settings{
		"no base URL": {Model: "m"},
		"no model":    {BaseURL: "https://example.invalid/v1"},
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New(s, keyFunc("k")); err == nil {
				t.Errorf("New(%+v) accepted an incomplete role", s)
			}
		})
	}
}
