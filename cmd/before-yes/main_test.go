package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/spend"
)

// The hosts point at a closed port: no test here makes a model or Tavily
// call, and none needs a key.

type testEnv struct {
	path string
	dir  string
	web  string
}

func writeSettings(t *testing.T, extra string) testEnv {
	t.Helper()
	dir := t.TempDir()
	web := filepath.Join(dir, "web")
	if err := os.MkdirAll(filepath.Join(web, "_app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<!doctype html><title>Before Yes shell</title>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "_app", "app.js"), []byte("console.log('app')"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "listen = \"127.0.0.1:0\"\ndata_dir = \"" + filepath.Join(dir, "data") + "\"\nweb_dir = \"" + web + "\"\n" + extra + `
[reader]
provider = "test"
base_url = "http://127.0.0.1:1/v1"
model    = "reader-model"

[judge]
provider = "test"
base_url = "http://127.0.0.1:1/v1"
model    = "judge-model"
`
	path := filepath.Join(dir, "before-yes.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return testEnv{path: path, dir: dir, web: web}
}

func noEnv(string) (string, bool) { return "", false }

func serveApp(t *testing.T, extra string, lookup func(string) (string, bool)) (*ServeApp, *httptest.Server) {
	t.Helper()
	e := writeSettings(t, extra)
	s, _, err := Load(context.Background(), e.path, lookup)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	a, err := NewServeApp(context.Background(), s, nil)
	if err != nil {
		t.Fatalf("NewServeApp: %v", err)
	}
	t.Cleanup(a.Close)
	ts := httptest.NewServer(a.Handler)
	t.Cleanup(ts.Close)
	return a, ts
}

func do(t *testing.T, method, url, body string, hdr map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func code(t *testing.T, b []byte) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(b, &env)
	return env.Error.Code
}

// serve and check both build a judge whose entailment client is the judge
// client, not the reader. The reader accepted grades the area definitions
// exclude (T3.11). The entail call still uses JudgeFast, thinking off.
func TestServeAndCheckWireEntail(t *testing.T) {
	a, _ := serveApp(t, "", noEnv)
	e := writeSettings(t, "")
	s, _, err := Load(context.Background(), e.path, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCheckApp(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for name, p := range map[string]*core{"serve": a.core, "check": c} {
		j := p.Pipeline.Judge
		if j.Entail == nil {
			t.Errorf("%s: Judge.Entail is nil", name)
			continue
		}
		if j.Entail != j.Model {
			t.Errorf("%s: Judge.Entail is not the judge client", name)
		}
		if j.Entail == p.Pipeline.Reader.Model {
			t.Errorf("%s: Judge.Entail is the reader's client", name)
		}
		if j.Entail.Settings().Model != "judge-model" {
			t.Errorf("%s: entail model %q, want the judge", name, j.Entail.Settings().Model)
		}
		if p.Pipeline.Reader.Model.Settings().Model != "reader-model" {
			t.Errorf("%s: reader model %q", name, p.Pipeline.Reader.Model.Settings().Model)
		}
	}
}

// serve gives the server and the pipeline the one *spend.Meter that every
// paid call books on, and the cap it carries is the one POST /api/checks
// enforces: with daily_fresh_checks 0 an uncached title is refused 402
// before any call.
func TestServeSharesOneMeter(t *testing.T) {
	a, ts := serveApp(t, "[budget]\ndaily_fresh_checks = 0\n", noEnv)
	if a.Server.Checks != a.Meter || a.Pipeline.Checks != a.Meter {
		t.Fatal("Server.Checks and Pipeline.Checks must both be the app's meter")
	}
	if any(a.Pipeline.Gatherer.Meter) != any(a.Meter) || any(a.Pipeline.Reader.Meter) != any(a.Meter) || any(a.Pipeline.Judge.Meter) != any(a.Meter) {
		t.Error("a paid call goes around the meter")
	}
	data := filepath.Dir(a.DBPath)
	if _, err := os.Stat(filepath.Join(data, spendName)); err != nil {
		t.Errorf("the meter's own file: %v", err)
	}
	if a.Server.Pipeline != a.Pipeline {
		t.Error("the server runs a different pipeline")
	}
	resp, b := do(t, http.MethodPost, ts.URL+"/api/checks", `{"title":"Fortnite","age":11}`, nil)
	if resp.StatusCode != http.StatusPaymentRequired || code(t, b) != "budget_exhausted" {
		t.Errorf("status %d body %s, want 402 budget_exhausted", resp.StatusCode, b)
	}
}

// serve routes POST /api/checks through the gate: with [gate.passcode]
// set, no passcode is 403; the right one gets past the gate. Other routes
// need none.
func TestServeRoutesChecksThroughGate(t *testing.T) {
	lookup := func(k string) (string, bool) {
		if k == "BY_TEST_PASSCODE" {
			return "test-passcode", true
		}
		return "", false
	}
	_, ts := serveApp(t, "[budget]\ndaily_fresh_checks = 0\n[gate.passcode]\nsource = \"env\"\nvar = \"BY_TEST_PASSCODE\"\n", lookup)
	resp, b := do(t, http.MethodPost, ts.URL+"/api/checks", `{"title":"Fortnite","age":11}`, nil)
	if resp.StatusCode != http.StatusForbidden || code(t, b) != "passcode_required" {
		t.Fatalf("no passcode: status %d body %s, want 403 passcode_required", resp.StatusCode, b)
	}
	resp, b = do(t, http.MethodPost, ts.URL+"/api/checks", `{"title":"Fortnite","age":11}`, map[string]string{"X-Passcode": "test-passcode"})
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("with passcode: status %d body %s, want 402 (past the gate)", resp.StatusCode, b)
	}
	if resp, _ := do(t, http.MethodGet, ts.URL+"/api/status", "", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("status route gated: %d", resp.StatusCode)
	}
}

// The live switch on the serve path: the config's live = false is the
// default, and a flag write turns checks back on without a restart; status
// reports the same switch.
func TestServeLiveSwitch(t *testing.T) {
	a, ts := serveApp(t, "live = false\n", noEnv)
	resp, b := do(t, http.MethodPost, ts.URL+"/api/checks", `{"title":"Fortnite","age":11}`, nil)
	if resp.StatusCode != http.StatusServiceUnavailable || code(t, b) != "live_checks_off" {
		t.Fatalf("live off: status %d body %s", resp.StatusCode, b)
	}
	if _, b := do(t, http.MethodGet, ts.URL+"/api/status", "", nil); !strings.Contains(string(b), `"live":false`) {
		t.Errorf("status %s, want live false", b)
	}
	if _, err := a.Flags.SetBool(context.Background(), a.Live, true); err != nil {
		t.Fatal(err)
	}
	if _, b := do(t, http.MethodGet, ts.URL+"/api/status", "", nil); !strings.Contains(string(b), `"live":true`) {
		t.Errorf("status %s, want live true", b)
	}
}

// GET /api/status reports the live switch, the checklist version and the
// budget remaining in the model, Tavily and daily-check pools, in the
// shape of get-status.json plus checklist_version.
func TestStatusRoute(t *testing.T) {
	a, ts := serveApp(t, "[budget]\nmodel_usd = 20\ntavily_credits_monthly = 900\ndaily_fresh_checks = 5\n", noEnv)
	hold, err := a.Meter.BeginCheck(context.Background(), "earlier")
	if err != nil {
		t.Fatal(err)
	}
	if err := hold.Done(); err != nil {
		t.Fatal(err)
	}
	resp, b := do(t, http.MethodGet, ts.URL+"/api/status", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"live":              true,
		"checklist_version": checklist.Version,
		"budget": map[string]any{
			"token_factory_remaining_nano": float64(20_000_000_000),
			"tavily_credits_remaining":     float64(900),
			"fresh_checks_remaining_today": float64(4),
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("status %s\nwant %v", b, want)
	}
	// Every key of the contract example is present.
	var ex struct {
		Response struct {
			Body map[string]any `json:"body"`
		} `json:"response"`
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contracts", "api", "get-status.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &ex); err != nil {
		t.Fatal(err)
	}
	for k := range ex.Response.Body {
		if _, ok := got[k]; !ok {
			t.Errorf("status lacks contract key %q", k)
		}
	}
	for k := range ex.Response.Body["budget"].(map[string]any) {
		if _, ok := got["budget"].(map[string]any)[k]; !ok {
			t.Errorf("status budget lacks contract key %q", k)
		}
	}
	_ = spend.PoolModel
}

// GET /api/checklist is exactly get-checklist.json's body.
func TestChecklistRoute(t *testing.T) {
	_, ts := serveApp(t, "", noEnv)
	resp, b := do(t, http.MethodGet, ts.URL+"/api/checklist", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var ex struct {
		Response struct {
			Body json.RawMessage `json:"body"`
		} `json:"response"`
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contracts", "api", "get-checklist.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &ex); err != nil {
		t.Fatal(err)
	}
	var got, want any
	_ = json.Unmarshal(b, &got)
	_ = json.Unmarshal(ex.Response.Body, &want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("checklist %s\nwant %s", b, ex.Response.Body)
	}
}

// Every other route answers on the serve handler: health, the check and
// card routes, the app shell, its files, and a deep link to a card.
func TestEveryRoute(t *testing.T) {
	_, ts := serveApp(t, "", noEnv)
	cases := []struct {
		method, path, body string
		status             int
		contains           string
	}{
		{"GET", "/healthz", "", 200, "ok"},
		{"GET", "/api/cards", "", 200, `"cards"`},
		{"GET", "/api/cards/c_nope", "", 404, "not_found"},
		{"GET", "/api/checks/j_nope", "", 404, "not_found"},
		{"GET", "/api/checks/j_nope/events", "", 404, "not_found"},
		{"POST", "/api/checks", `{"title":"","age":9}`, 400, "invalid_request"},
		{"GET", "/api/unknown", "", 404, ""},
		{"GET", "/", "", 200, "Before Yes shell"},
		{"GET", "/_app/app.js", "", 200, "console.log"},
		{"GET", "/card/c_0123456789abcdef0123456789abcdef", "", 200, "Before Yes shell"},
		{"GET", "/about/deep/link", "", 200, "Before Yes shell"},
	}
	for _, c := range cases {
		resp, b := do(t, c.method, ts.URL+c.path, c.body, nil)
		if resp.StatusCode != c.status || !bytes.Contains(b, []byte(c.contains)) {
			t.Errorf("%s %s: status %d body %.120s, want %d containing %q", c.method, c.path, resp.StatusCode, b, c.status, c.contains)
		}
	}
	if resp, _ := do(t, "POST", ts.URL+"/card/x", "", nil); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST to the app: %d, want 405", resp.StatusCode)
	}
}

// config prints the settings plan and no secret value; the example file
// parses with its secrets pointed at a stand-in env file of test values.
func TestConfigCommandAndExample(t *testing.T) {
	ex, err := os.ReadFile(filepath.Join("..", "..", "before-yes.toml.example"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	envFile := filepath.Join(dir, "test.env")
	if err := os.WriteFile(envFile, []byte("NEBIUS_KEY=test-value-nebius\nTAVILY_KEY=test-value-tavily\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(ex), `path   = ".env"`, `path   = "`+envFile+`"`)
	text = strings.Replace(text, `data_dir = "data"`, `data_dir = "`+filepath.Join(dir, "data")+`"`, 1)
	path := filepath.Join(dir, "before-yes.toml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"config", "-config", path}, &out, &errb); rc != 0 {
		t.Fatalf("config exit %d: %s", rc, errb.String())
	}
	if out.Len() == 0 {
		t.Error("config printed nothing")
	}
	if strings.Contains(out.String(), "test-value") || strings.Contains(errb.String(), "test-value") {
		t.Error("config printed a secret value")
	}
	s, _, err := Load(context.Background(), path, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if s.Budget.DailyFreshChecks != 5 || s.Reader.Model == "" || s.Judge.Model == "" || s.JudgeThink == nil {
		t.Errorf("example settings %+v", s.Budget)
	}
	if rc := run(context.Background(), []string{"nope"}, &out, &errb); rc != 2 {
		t.Errorf("unknown command exit %d, want 2", rc)
	}
}

// One process owns spend.db: with serve's app open, a check (or a second
// serve) on the same data_dir refuses with ErrMeterBusy and opens no
// meter; once serve closes, it opens. This is the two-process case of
// nrynss/keel#100, closed by refusing the second meter.
func TestMeterLockRefusesSecondProcess(t *testing.T) {
	e := writeSettings(t, "")
	s, _, err := Load(context.Background(), e.path, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewServeApp(context.Background(), s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCheckApp(context.Background(), s); !errors.Is(err, ErrMeterBusy) {
		t.Fatalf("check while serve runs: %v, want ErrMeterBusy", err)
	}
	if _, err := NewServeApp(context.Background(), s, nil); !errors.Is(err, ErrMeterBusy) {
		t.Fatalf("second serve: %v, want ErrMeterBusy", err)
	}
	var out, errb bytes.Buffer
	if rc := run(context.Background(), []string{"check", "-config", e.path, "Roblox"}, &out, &errb); rc != 1 || !strings.Contains(errb.String(), "spend.db") {
		t.Errorf("check command: exit %d stderr %s, want 1 naming spend.db", rc, errb.String())
	}
	a.Close()
	c, err := NewCheckApp(context.Background(), s)
	if err != nil {
		t.Fatalf("check after serve closed: %v", err)
	}
	c.Close()
}

// The reviewer's pin: 400 concurrent BeginCheck+Done. With the lock there
// is only ever one meter on spend.db, and it loses no booking; a second
// meter on the same file cannot be opened through the app.
func TestMeterLosesNoBookings(t *testing.T) {
	for round := range 3 {
		e := writeSettings(t, "[budget]\ndaily_fresh_checks = 400\n")
		s, _, err := Load(context.Background(), e.path, noEnv)
		if err != nil {
			t.Fatal(err)
		}
		a, err := NewServeApp(context.Background(), s, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NewCheckApp(context.Background(), s); !errors.Is(err, ErrMeterBusy) {
			t.Fatalf("second meter opened: %v", err)
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		var failed int
		for i := range 400 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				h, err := a.Meter.BeginCheck(context.Background(), fmt.Sprintf("r%d-%d", round, i))
				if err == nil {
					err = h.Done()
				}
				if err != nil {
					mu.Lock()
					failed++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		st, err := a.Meter.Status(context.Background(), spend.PoolChecksDaily)
		if err != nil {
			t.Fatal(err)
		}
		if failed != 0 || st.Spent != 400 {
			t.Errorf("round %d: failed %d spent %d, want 0 and 400", round, failed, st.Spent)
		}
		a.Close()
	}
}

// check takes its flags before, after or between the title's words.
func TestCheckFlagsAnywhere(t *testing.T) {
	cases := map[string][]string{
		"after":   {"Roblox", "-age", "12"},
		"before":  {"-age", "12", "Roblox"},
		"between": {"Toca", "-age=12", "Boca", "World"},
	}
	want := map[string]string{"after": "Roblox", "before": "Roblox", "between": "Toca Boca World"}
	for name, args := range cases {
		o, ok := parseArgs("check", append(args, "-config", "x.toml"), io.Discard)
		if !ok || o.age != 12 || o.title != want[name] || o.config != "x.toml" {
			t.Errorf("%s: %+v ok %v", name, o, ok)
		}
	}
	if _, ok := parseArgs("check", []string{"Roblox", "-agee", "12"}, io.Discard); ok {
		t.Error("an unknown trailing flag must be refused")
	}
}
