package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nrynss/before-yes/internal/appstore"
	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/finding"
	"github.com/nrynss/before-yes/internal/model"
	"github.com/nrynss/before-yes/internal/pipeline"
	"github.com/nrynss/before-yes/internal/spend"
	"github.com/nrynss/before-yes/internal/tavily"
)

// A live gather saved through the recorder replays through the fixture
// server with no further upstream call, and the documents match, including
// a quote that carries characters JSON would escape. The sentinel key is
// forwarded and never written.
// Entailment runs on the judge client. A reply whose system prompt is the
// entailment check must not be stored as a judge pass, or diagnosis reads
// it as a conflict pass.
func TestEntailTraceIsNotAJudgePass(t *testing.T) {
	tr := &callTrace{}
	c := &tracingChatter{inner: keywordReader{}, role: "judge", tr: tr}
	_, err := c.Chat(context.Background(), []model.Message{
		{Role: "system", Content: "Answer meets_definition"},
		{Role: "user", Content: "Area: contact\n"},
	}, model.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.judge) != 0 || len(tr.entail) != 1 {
		t.Fatalf("judge traces %d, entail traces %d", len(tr.judge), len(tr.entail))
	}
}

func TestEntailmentRunsOnTheJudgeModel(t *testing.T) {
	judge := &replayer{role: "judge", model: "recorded-judge"}
	if got := entailChatter(judge); got != judge {
		t.Fatalf("entail chatter = %v, want the judge model", got)
	}
}

func TestLiveGatherReplaysOffline(t *testing.T) {
	const key = "SENTINEL-KEY-t3-11"
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method == http.MethodPost && r.Header.Get("Authorization") != "Bearer "+key {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/search" && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"resultCount":1,"results":[{"trackId":42,"trackName":"Widget","sellerName":"Widget Co","sellerUrl":"https://widget.example.com/","trackViewUrl":"https://apps.apple.com/us/app/widget/id42","description":"A block building game. Chat is part of play & so are purchases."}]}`)
		case strings.Contains(r.URL.Path, "customerreviews"):
			if strings.Contains(r.URL.Path, "page=1/") {
				_, _ = io.WriteString(w, `{"feed":{"entry":{"id":{"label":"r1"},"title":{"label":"Careful"},"content":{"label":"Strangers can message a child in open chat."},"im:rating":{"label":"1"},"im:version":{"label":"1.0"},"updated":{"label":"2026-01-02T03:04:05-07:00"},"author":{"name":{"label":"Pat"}}}}}`)
				return
			}
			_, _ = io.WriteString(w, `{"feed":{}}`)
		case r.URL.Path == "/search":
			var s tavily.Search
			if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
				http.Error(w, "search body", http.StatusBadRequest)
				return
			}
			url := "https://example.com/" + strings.ReplaceAll(s.Query, " ", "-")
			raw := "Parents discuss " + s.Query + ". A child met someone & shared a photo <here>."
			if strings.Contains(s.Query, "privacy") {
				url = "https://widget.example.com/privacy"
				raw = ""
			}
			if strings.Contains(s.Query, "terms") {
				url = "https://widget.example.com/terms"
				raw = ""
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{
					"url": url, "title": s.Query, "content": raw, "raw_content": raw, "score": 0.5,
				}},
				"usage": map[string]any{"credits": 1},
			})
		case r.URL.Path == "/extract":
			var req struct {
				URLs []string `json:"urls"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			var pages []map[string]any
			for _, u := range req.URLs {
				text := "Privacy Policy\nWe collect an email when a parent signs up."
				title := "Privacy Policy"
				if strings.Contains(u, "terms") {
					text = "Terms of Service\nPurchases need the account holder."
					title = "Terms of Service"
				}
				pages = append(pages, map[string]any{"url": u, "title": title, "raw_content": text})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": pages, "failed_results": []any{}, "usage": map[string]any{"credits": 1},
			})
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusBadRequest)
		}
	}))
	defer upstream.Close()

	dir := t.TempDir()
	rec, err := newGatherRecorder(dir, "Widget", upstream.URL, upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(rec)
	defer proxy.Close()
	now := func() time.Time { return time.Date(2026, 10, 13, 19, 0, 0, 0, time.UTC) }
	first := gatherThrough(t, proxy.URL, dir, now)
	paid := hits.Load()
	if paid < 8 {
		t.Fatalf("upstream calls %d, want the search plan plus the store", paid)
	}
	again := gatherThrough(t, proxy.URL, dir, now)
	if hits.Load() != paid {
		t.Fatalf("a second gather called upstream %d more times", hits.Load()-paid)
	}
	if strings.Join(docKeys(again), "\n") != strings.Join(docKeys(first), "\n") {
		t.Fatalf("cached gather docs differ:\n%s\n---\n%s", docKeys(again), docKeys(first))
	}

	fix, fixSrv := serveFixture(dir, "Widget")
	defer fixSrv.Close()
	replayed := gatherThrough(t, fixSrv.URL, dir, now)
	if strings.Join(docKeys(replayed), "\n") != strings.Join(docKeys(first), "\n") {
		t.Fatalf("fixture replay docs differ:\n%s\n---\n%s", docKeys(replayed), docKeys(first))
	}
	if hits.Load() != paid {
		t.Fatalf("fixture replay called upstream")
	}
	walk := func(path string, _ os.FileInfo, err error) error {
		if err != nil || path == dir {
			return err
		}
		if info, e := os.Stat(path); e != nil || info.IsDir() {
			return e
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), key) {
			t.Errorf("key written to %s", path)
		}
		return nil
	}
	if err := filepath.Walk(dir, walk); err != nil {
		t.Fatal(err)
	}
	_ = fix
}

func gatherThrough(t *testing.T, base, _ string, now func() time.Time) []pipeline.Doc {
	t.Helper()
	m, err := spend.Open(context.Background(), spend.Config{Path: filepath.Join(t.TempDir(), "s.db"), Pools: map[spend.Pool]spend.PoolConfig{
		spend.PoolTavily: {Limit: 1_000_000},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	tc := tavily.New(base, func() (string, error) { return "SENTINEL-KEY-t3-11", nil })
	tc.Retry.PerMinute, tc.Retry.Name = 0, ""
	tc.Retry.Sleep = func(context.Context, time.Duration) error { return nil }
	g := &pipeline.Gatherer{Store: publicFeed{appstore.New(base, "us"), appstore.New("", "us")}, Search: tc, Meter: m, Now: now}
	got, err := g.Gather(context.Background(), "Widget", "t")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Docs) == 0 {
		t.Fatal("no documents")
	}
	return got.Docs
}

func docKeys(docs []pipeline.Doc) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.Source.Kind + "\t" + d.Source.URL + "\t" + d.Text
	}
	return out
}

func TestCheckActedNamesTheReason(t *testing.T) {
	byID := map[string]finding.Finding{
		"r1": {ID: "r1", Area: checklist.Contact, Stance: finding.Risk, Quote: "A stranger messaged her."},
		"m1": {ID: "m1", Area: checklist.Money, Stance: finding.Mitigation, Quote: "Ask to Buy is in the settings."},
		"c1": {ID: "c1", Area: checklist.Contact, Stance: finding.Mitigation, Quote: "Chat can be switched off."},
	}
	if got := citationFault(rawGrade{Area: "contact", Level: "flag", Findings: []string{"nope"}}, byID); !strings.Contains(got, "no citation") {
		t.Errorf("missing id: %q", got)
	}
	if got := citationFault(rawGrade{Area: "contact", Level: "flag", Findings: []string{"m1"}}, byID); !strings.Contains(got, "outside the area") {
		t.Errorf("wrong area: %q", got)
	}
	if got := citationFault(rawGrade{Area: "contact", Level: "flag", Findings: []string{"r1", "m1"}}, byID); !strings.Contains(got, "outside the area") {
		t.Errorf("bad id among good ones: %q", got)
	}
	if got := citationFault(rawGrade{Area: "contact", Level: "flag", Findings: []string{"c1"}}, byID); !strings.Contains(got, "wrong stance") {
		t.Errorf("wrong stance: %q", got)
	}
	if got := citationFault(rawGrade{Area: "contact", Level: "flag"}, byID); got != "no citation" {
		t.Errorf("empty list: %q", got)
	}
	if got := citationFault(rawGrade{Area: "contact", Level: "flag", Findings: []string{"r1"}}, byID); got != "" {
		t.Errorf("fitting citation faulted: %q", got)
	}
	cases := []struct {
		reason string
		want   string
	}{
		{pipeline.ThinReason, "checkCitations: no citation"},
		{pipeline.SettingThinReason, "ground: setting_without_quote"},
		{pipeline.NotEarnedReason, "entail: flag_not_earned"},
		{pipeline.UnsupportedReason, "entail: reason_unsupported"},
		{checklist.Reasons[checklist.RuleOKNoProof], "ok_without_finding"},
		{"Strangers can reach this child.", "stood"},
	}
	for _, c := range cases {
		if got := checkActed(checklist.Grade{Reason: c.reason}, "no citation"); got != c.want {
			t.Errorf("reason %q: check %q, want %q", c.reason, got, c.want)
		}
	}
}
