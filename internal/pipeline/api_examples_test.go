package pipeline

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nrynss/keel/wire"

	"github.com/nrynss/before-yes/internal/checklist"
)

// Tests for the T1.4 API examples in testdata/contracts/api/. They keep the
// examples tied to the types and to Keel's wire package, so a regenerated
// card or a checklist change cannot leave the API examples behind.

const apiDir = "../../testdata/contracts/api"

type exchange struct {
	Request struct {
		Method string          `json:"method"`
		Path   string          `json:"path"`
		Body   json.RawMessage `json:"body,omitempty"`
	} `json:"request"`
	Response struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	} `json:"response"`
}

func readExchange(t *testing.T, name string) exchange {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(apiDir, name))
	if err != nil {
		t.Fatal(err)
	}
	var x exchange
	if err := decodeStrict(b, &x); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return x
}

func sameJSON(a, b []byte) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}

// get-card.json serves card-no.json, unchanged.
func TestAPICardMatchesContract(t *testing.T) {
	x := readExchange(t, "get-card.json")
	card, err := os.ReadFile(filepath.Join(contracts, "card-no.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !sameJSON(x.Response.Body, card) {
		t.Error("get-card.json body differs from card-no.json; regenerate with -update")
	}
	var c Card
	json.Unmarshal(card, &c)
	if x.Request.Path != "/api/cards/"+c.ID {
		t.Errorf("path %s does not name card %s", x.Request.Path, c.ID)
	}
	for _, name := range []string{"post-checks-200.json", "get-check-done.json"} {
		var body struct {
			CardID string `json:"card_id"`
		}
		json.Unmarshal(readExchange(t, name).Response.Body, &body)
		if body.CardID != c.ID {
			t.Errorf("%s: card_id %q, want %q", name, body.CardID, c.ID)
		}
	}
}

// get-cards.json lists the card examples as they are, under the current
// checklist version (T1.4 round 2 M1).
func TestAPICardsMatchContracts(t *testing.T) {
	var body struct {
		Cards []struct {
			ID               string            `json:"id"`
			Title            string            `json:"title"`
			Age              int               `json:"age"`
			Verdict          checklist.Verdict `json:"verdict"`
			ChecklistVersion string            `json:"checklist_version"`
			CheckedAt        string            `json:"checked_at"`
		} `json:"cards"`
	}
	if err := decodeStrict(readExchange(t, "get-cards.json").Response.Body, &body); err != nil {
		t.Fatal(err)
	}
	want := map[string]Card{}
	for _, name := range []string{"card-no.json", "card-settings.json", "card-yes.json", "card-unclear.json"} {
		b, _ := os.ReadFile(filepath.Join(contracts, name))
		var c Card
		json.Unmarshal(b, &c)
		want[c.ID] = c
	}
	if len(body.Cards) != len(want) {
		t.Fatalf("%d cards listed, want %d", len(body.Cards), len(want))
	}
	for _, s := range body.Cards {
		c, ok := want[s.ID]
		if !ok || s.Title != c.Title || s.Age != c.Age || s.Verdict != c.Verdict ||
			s.ChecklistVersion != checklist.Version || s.ChecklistVersion != c.ChecklistVersion || s.CheckedAt != c.CheckedAt.Format("2006-01-02T15:04:05Z07:00") {
			t.Errorf("get-cards.json entry %+v does not match its card", s)
		}
	}
}

// get-checklist.json is what the checklist package says.
func TestAPIChecklistMatches(t *testing.T) {
	var body struct {
		Version  string               `json:"version"`
		Areas    []checklist.AreaSpec `json:"areas"`
		Levels   []checklist.Level    `json:"levels"`
		Verdicts []checklist.Verdict  `json:"verdicts"`
	}
	if err := decodeStrict(readExchange(t, "get-checklist.json").Response.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body.Version != checklist.Version || !reflect.DeepEqual(body.Areas, checklist.Areas()) {
		t.Errorf("get-checklist.json is stale: %s", body.Version)
	}
	if !reflect.DeepEqual(body.Levels, []checklist.Level{checklist.OK, checklist.OKWithSetting, checklist.Flag, checklist.Unknown}) ||
		!reflect.DeepEqual(body.Verdicts, []checklist.Verdict{checklist.Yes, checklist.YesWithSettings, checklist.No, checklist.Unclear}) {
		t.Error("levels or verdicts stale")
	}
}

// Every non-2xx example is Keel's wire envelope with a documented code.
func TestAPIErrorEnvelopes(t *testing.T) {
	codes := map[string]bool{}
	doc, _ := os.ReadFile("../../docs/api.md")
	files, _ := filepath.Glob(filepath.Join(apiDir, "*.json"))
	if len(files) == 0 {
		t.Fatal("no API examples")
	}
	for _, f := range files {
		x := readExchange(t, filepath.Base(f))
		if x.Response.Status < 400 {
			continue
		}
		var env struct {
			Error struct {
				Code    string          `json:"code"`
				Message string          `json:"message"`
				Detail  json.RawMessage `json:"detail,omitempty"`
			} `json:"error"`
		}
		if err := decodeStrict(x.Response.Body, &env); err != nil || env.Error.Code == "" || env.Error.Message == "" {
			t.Errorf("%s: not a wire envelope: %v", f, err)
		}
		if !bytes.Contains(doc, []byte("`"+env.Error.Code+"`")) {
			t.Errorf("%s: code %s not in docs/api.md", f, env.Error.Code)
		}
		codes[env.Error.Code] = true
	}
	if len(codes) < 5 {
		t.Errorf("only %d error codes have examples", len(codes))
	}
}

func i64(n int64) *int64 { return &n }

func progress(job, stage, msg string, cur, total *int64) wire.ProgressEvent {
	d, _ := json.Marshal(map[string]string{"message": msg})
	return wire.ProgressEvent{JobID: job, Stage: stage, Current: cur, Total: total, Detail: d}
}

// streams builds the example event streams with Keel's own writer.
func streams(t *testing.T) map[string]string {
	head := func(job string) string {
		return "GET /api/checks/" + job + "/events\nAccept: text/event-stream\n\n200 text/event-stream\n\n"
	}
	write := func(frames ...func(*httptest.ResponseRecorder)) string {
		rec := httptest.NewRecorder()
		for _, f := range frames {
			f(rec)
		}
		return rec.Body.String()
	}
	ev := func(name string, id uint64, data any) func(*httptest.ResponseRecorder) {
		return func(r *httptest.ResponseRecorder) {
			if err := wire.WriteEvent(r, name, id, data); err != nil {
				t.Fatal(err)
			}
		}
	}
	j1, j2 := "j_3Fh8sQ2mVx7Kp1Lz9RtWc", "j_9Qm2Wd5Hn8Bx4Kv7Ts1Ra"
	errEv, err := wire.NewErrorEvent(j2, "budget_exhausted", "the Token Factory budget is spent", map[string]string{"pool": "token_factory"})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"events-done.txt": head(j1) + write(
			ev(wire.EventProgress, 1, progress(j1, "gather", "searching for the privacy policy", nil, nil)),
			ev(wire.EventProgress, 2, progress(j1, "read", "reading 500 reviews", i64(120), i64(500))),
			ev(wire.EventProgress, 3, progress(j1, "judge", "grading 6 areas", nil, nil)),
			ev(wire.EventDone, 4, wire.StatusEvent{JobID: j1, Status: wire.EventDone}),
		),
		"events-error.txt": head(j2) + write(
			ev(wire.EventProgress, 1, progress(j2, "gather", "searching for the privacy policy", nil, nil)),
			ev(wire.EventError, 2, errEv),
		),
	}
}

// TestAPIGenerate rewrites the derived API examples with -update and
// otherwise fails when they are stale: the event streams (written by
// wire.WriteEvent), get-card.json and get-checklist.json.
func TestAPIGenerate(t *testing.T) {
	files := streams(t)
	card, _ := os.ReadFile(filepath.Join(contracts, "card-no.json"))
	var c Card
	json.Unmarshal(card, &c)
	cardX := map[string]any{
		"request":  map[string]any{"method": "GET", "path": "/api/cards/" + c.ID},
		"response": map[string]any{"status": 200, "body": json.RawMessage(card)},
	}
	cl := map[string]any{
		"request": map[string]any{"method": "GET", "path": "/api/checklist"},
		"response": map[string]any{"status": 200, "body": map[string]any{
			"version": checklist.Version, "areas": checklist.Areas(),
			"levels":   []checklist.Level{checklist.OK, checklist.OKWithSetting, checklist.Flag, checklist.Unknown},
			"verdicts": []checklist.Verdict{checklist.Yes, checklist.YesWithSettings, checklist.No, checklist.Unclear},
		}},
	}
	type summary struct {
		ID               string            `json:"id"`
		Title            string            `json:"title"`
		Age              int               `json:"age"`
		Verdict          checklist.Verdict `json:"verdict"`
		ChecklistVersion string            `json:"checklist_version"`
		CheckedAt        string            `json:"checked_at"`
	}
	var recent []summary
	for _, name := range []string{"card-no.json", "card-settings.json", "card-yes.json", "card-unclear.json"} {
		b, err := os.ReadFile(filepath.Join(contracts, name))
		if err != nil {
			t.Fatal(err)
		}
		var k Card
		json.Unmarshal(b, &k)
		recent = append(recent, summary{k.ID, k.Title, k.Age, k.Verdict, k.ChecklistVersion, k.CheckedAt.Format("2006-01-02T15:04:05Z07:00")})
	}
	cards := map[string]any{
		"request":  map[string]any{"method": "GET", "path": "/api/cards"},
		"response": map[string]any{"status": 200, "body": map[string]any{"cards": recent}},
	}
	for name, v := range map[string]any{"get-card.json": cardX, "get-checklist.json": cl, "get-cards.json": cards} {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(v); err != nil {
			t.Fatal(err)
		}
		files[name] = buf.String()
	}
	for name, want := range files {
		path := filepath.Join(apiDir, name)
		if *update {
			if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, _ := os.ReadFile(path)
		if strings.HasSuffix(name, ".txt") && string(got) != want {
			t.Errorf("%s is stale; run go test ./internal/pipeline -update", name)
		}
		if strings.HasSuffix(name, ".json") && !sameJSON(got, []byte(want)) {
			t.Errorf("%s is stale; run go test ./internal/pipeline -update", name)
		}
	}
}
