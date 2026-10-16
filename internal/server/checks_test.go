package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/gate"
	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/job"
	"github.com/nrynss/keel/sqlite"
	"github.com/nrynss/keel/stream"
	"github.com/nrynss/keel/wire"

	"github.com/nrynss/before-yes/internal/appstore"
	"github.com/nrynss/before-yes/internal/cards"
	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/finding"
	"github.com/nrynss/before-yes/internal/model"
	"github.com/nrynss/before-yes/internal/pipeline"
	"github.com/nrynss/before-yes/internal/spend"
	"github.com/nrynss/before-yes/internal/tavily"
)

type configurableSearcher struct {
	mu           sync.Mutex
	failErr      error
	emptyDocs    bool
	blockChan    chan struct{}
	searchCalls  int
	extractCalls int
	// pageText, when set, is every extracted page's text, so a test can
	// make the read span several chunks.
	pageText string
	// failQuery, when set, fails every search whose query has this prefix.
	failQuery string
}

func (s *configurableSearcher) Search(ctx context.Context, req tavily.Search) ([]tavily.Result, tavily.Credits, error) {
	s.mu.Lock()
	s.searchCalls++
	block := s.blockChan
	fail := s.failErr
	empty := s.emptyDocs
	s.mu.Unlock()

	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, tavily.Credits{}, ctx.Err()
		}
	}

	if fail != nil {
		return nil, tavily.Credits{}, fail
	}
	s.mu.Lock()
	fq := s.failQuery
	s.mu.Unlock()
	if fq != "" && strings.HasPrefix(req.Query, fq) {
		return nil, tavily.Credits{}, &tavily.Error{Status: 502, Body: "upstream said tvly-dev-SECRETKEY1234567890 is bad"}
	}
	if empty {
		return nil, tavily.Credits{}, nil
	}

	return []tavily.Result{
		{
			URL:     "https://example.com/privacy",
			Title:   "Privacy Policy",
			Content: "We collect basic info. Chat is filtered for safety.",
		},
	}, tavily.Credits{Used: 1, Reported: true}, nil
}

func (s *configurableSearcher) ExtractWithFailures(ctx context.Context, urls []string) (tavily.Extracted, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.extractCalls++
	if s.failErr != nil {
		return tavily.Extracted{}, s.failErr
	}
	if s.emptyDocs {
		return tavily.Extracted{}, nil
	}
	text := s.pageText
	if text == "" {
		text = "Full policy document: chat is moderated and data is retained."
	}
	pages := make([]tavily.Page, len(urls))
	for i, u := range urls {
		pages[i] = tavily.Page{
			URL:        u,
			RawContent: text,
		}
	}
	return tavily.Extracted{Pages: pages, Credits: tavily.Credits{Used: 1, Reported: true}}, nil
}

func okGradesJSON() string {
	var gs []checklist.Grade
	for _, a := range checklist.Areas() {
		gs = append(gs, checklist.Grade{
			Area:   a.ID,
			Level:  checklist.OK,
			Reason: "All ok.",
		})
	}
	b, _ := json.Marshal(map[string]any{"grades": gs})
	return string(b)
}

type configurableStore struct {
	mu      sync.Mutex
	empty   bool
	country string
}

func (s *configurableStore) Find(ctx context.Context, term string, limit int) ([]appstore.App, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.empty {
		return nil, nil
	}
	return []appstore.App{
		{
			ID:        123456,
			Name:      term,
			SellerURL: "https://example.com",
		},
	}, nil
}

func (s *configurableStore) Reviews(ctx context.Context, appID int64, pages int) ([]appstore.Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.empty {
		return nil, nil
	}
	return []appstore.Review{
		{
			ID:    "rev1",
			Title: "Great",
			Body:  "Chat is safe.",
			Index: 1,
		},
	}, nil
}

func (s *configurableStore) FeedURL(appID int64, page int) string {
	return ""
}

func (s *configurableStore) Storefront() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.country == "" {
		return "us"
	}
	return s.country
}

type testServerEnv struct {
	db        *sqlite.DB
	cardStore *cards.Store
	broker    *stream.Broker
	runner    *job.Runner
	store     *configurableStore
	searcher  *configurableSearcher
	server    *Server
	srv       *httptest.Server
}

func newTestServerEnv(t *testing.T, limit int) *testServerEnv {
	t.Helper()
	db := openTestDB(t, filepath.Join(t.TempDir(), "test.db"))
	cardStore, err := cards.Open(context.Background(), db)
	if err != nil {
		t.Fatalf("cards.Open: %v", err)
	}
	evStore := openEvidenceStore(t, db, "evidence")

	broker := stream.New(stream.Config{})
	runner, err := NewRunner(db, broker, limit, nil, nil)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	evCache, err := NewEvidenceCache(EvidenceCacheConfig{Store: evStore})
	if err != nil {
		t.Fatalf("NewEvidenceCache: %v", err)
	}
	cardCache, err := NewCardCache(CardCacheConfig{Cards: cardStore})
	if err != nil {
		t.Fatalf("NewCardCache: %v", err)
	}

	appStore := &configurableStore{}
	searcher := &configurableSearcher{}
	readerChatter := &fakeChatter{replyText: "[]"}
	judgeChatter := &fakeChatter{replyText: okGradesJSON()}

	p := &pipeline.Pipeline{
		Gatherer: &pipeline.Gatherer{
			Store:  appStore,
			Search: searcher,
			Meter:  fakeMeter{},
		},
		Reader: &pipeline.Reader{
			Model: readerChatter,
			Meter: fakeMeter{},
		},
		Judge: &pipeline.Judge{
			Model: judgeChatter,
			Meter: fakeMeter{},
		},
		Cache: evCache,
	}

	srv := &Server{
		Pipeline:      p,
		Runner:        runner,
		Broker:        broker,
		Cards:         cardStore,
		EvidenceCache: evCache,
		CardCache:     cardCache,
	}

	h, err := srv.Handler()
	if err != nil {
		t.Fatalf("srv.Handler: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	return &testServerEnv{
		db:        db,
		cardStore: cardStore,
		broker:    broker,
		runner:    runner,
		store:     appStore,
		searcher:  searcher,
		server:    srv,
		srv:       ts,
	}
}

// TestCheckDrivesToDoneThroughHTTPAPI drives a check to done through POST /api/checks -> 202,
// polls GET /api/checks/{id} -> 200 done with card_id, and GET /api/cards/{id} -> card.
// Then repeats POST /api/checks and asserts 200 with card_id immediately from cache!
func TestCheckDrivesToDoneThroughHTTPAPI(t *testing.T) {
	env := newTestServerEnv(t, 4)

	// 1. POST /api/checks -> 202 with check_id
	reqBody := `{"title": "Roblox", "age": 9}`
	resp, err := http.Post(env.srv.URL+"/api/checks", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST /api/checks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /api/checks status = %d, want 202; body = %s", resp.StatusCode, b)
	}

	var startResp struct {
		CheckID string `json:"check_id"`
		CardID  string `json:"card_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&startResp); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	if !strings.HasPrefix(startResp.CheckID, "j_") || !id.Valid(strings.TrimPrefix(startResp.CheckID, "j_")) {
		t.Fatalf("check_id = %q; want j_ + 32-hex", startResp.CheckID)
	}
	if startResp.CardID != "" {
		t.Fatalf("card_id = %q; want empty on check start", startResp.CardID)
	}

	// 2. Poll GET /api/checks/{id} -> done with card_id
	var cardID string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get(env.srv.URL + "/api/checks/" + startResp.CheckID)
		if err != nil {
			t.Fatalf("GET check: %v", err)
		}
		var checkResp struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			CardID string `json:"card_id"`
		}
		_ = json.NewDecoder(res.Body).Decode(&checkResp)
		res.Body.Close()

		if checkResp.Status == "done" {
			if checkResp.ID != startResp.CheckID {
				t.Errorf("check response ID = %q, want %q", checkResp.ID, startResp.CheckID)
			}
			cardID = checkResp.CardID
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if cardID == "" {
		t.Fatal("check did not finish with status done within deadline")
	}
	if !strings.HasPrefix(cardID, "c_") || !id.Valid(strings.TrimPrefix(cardID, "c_")) {
		t.Fatalf("card_id = %q; want c_ + 32-hex", cardID)
	}

	// 3. GET /api/cards/{id} -> 200 with card JSON
	cResp, err := http.Get(env.srv.URL + "/api/cards/" + cardID)
	if err != nil {
		t.Fatalf("GET card: %v", err)
	}
	defer cResp.Body.Close()
	if cResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(cResp.Body)
		t.Fatalf("GET card status = %d, want 200; body = %s", cResp.StatusCode, b)
	}
	var card pipeline.Card
	if err := json.NewDecoder(cResp.Body).Decode(&card); err != nil {
		t.Fatalf("decode card: %v", err)
	}
	if card.ID != cardID {
		t.Errorf("card.ID = %q; want %q", card.ID, cardID)
	}
	if card.Title != "Roblox" {
		t.Errorf("card.Title = %q; want Roblox", card.Title)
	}
	if card.Age != 9 {
		t.Errorf("card.Age = %d; want 9", card.Age)
	}
	if card.Verdict != checklist.Unclear {
		t.Errorf("card.Verdict = %s; want unclear", card.Verdict)
	}

	// 4. Repeat POST /api/checks -> 200 immediately with card_id from cache
	resp2, err := http.Post(env.srv.URL+"/api/checks", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST /api/checks repeat: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp2.Body)
		t.Fatalf("POST repeat status = %d, want 200 (cache hit); body = %s", resp2.StatusCode, b)
	}
	var cachedResp struct {
		CheckID string `json:"check_id"`
		CardID  string `json:"card_id"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&cachedResp); err != nil {
		t.Fatalf("decode cached response: %v", err)
	}
	if cachedResp.CardID != cardID {
		t.Errorf("cached card_id = %q; want %q", cachedResp.CardID, cardID)
	}
	if cachedResp.CheckID != "" {
		t.Errorf("cached check_id = %q; want empty", cachedResp.CheckID)
	}
}

// TestCheckEventsStream connects to GET /api/checks/{id}/events and reads SSE stream;
// verifies progress events (stage, detail) and the terminal done event.
func TestCheckEventsStream(t *testing.T) {
	env := newTestServerEnv(t, 4)

	// Hold the searcher until the event stream connects.
	gate := make(chan struct{})
	env.searcher.mu.Lock()
	env.searcher.blockChan = gate
	env.searcher.mu.Unlock()

	// 1. Start check
	reqBody := `{"title": "Minecraft", "age": 9}`
	resp, err := http.Post(env.srv.URL+"/api/checks", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	var startResp struct {
		CheckID string `json:"check_id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&startResp)

	// 2. Connect to GET /api/checks/{id}/events
	req, err := http.NewRequest(http.MethodGet, env.srv.URL+"/api/checks/"+startResp.CheckID+"/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	streamResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET events: %v", err)
	}
	defer streamResp.Body.Close()

	if streamResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(streamResp.Body)
		t.Fatalf("status = %d; body = %s", streamResp.StatusCode, b)
	}
	if ct := streamResp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q; want text/event-stream", ct)
	}

	// 3. Release gate
	close(gate)

	// 4. Read frames
	br := bufio.NewReader(streamResp.Body)
	var stages []string
	var sawDone bool

	for {
		frame, err := wire.ReadEvent(br)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("ReadEvent: %v", err)
		}
		if frame.Comment != "" {
			continue
		}
		switch frame.Event {
		case wire.EventProgress:
			var p wire.ProgressEvent
			if err := json.Unmarshal(frame.Data, &p); err != nil {
				t.Fatalf("unmarshal progress: %v", err)
			}
			stages = append(stages, p.Stage)
			if len(p.Detail) == 0 {
				t.Errorf("progress frame %s has empty detail", p.Stage)
			}
		case wire.EventDone:
			sawDone = true
			var st wire.StatusEvent
			if err := json.Unmarshal(frame.Data, &st); err != nil {
				t.Fatalf("unmarshal status: %v", err)
			}
			if st.Status != "done" {
				t.Errorf("status = %q, want done", st.Status)
			}
			return
		case wire.EventError:
			t.Fatalf("unexpected error event: %s", frame.Data)
		}
	}

	if !sawDone {
		t.Error("did not see done event on stream")
	}
	expectedStages := []string{"gather", "read", "judge"}
	for _, exp := range expectedStages {
		found := false
		for _, s := range stages {
			if s == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("did not observe stage %q in progress events: %v", exp, stages)
		}
	}
}

// TestCheckFailureSurfacesBudgetExhausted simulates cost.ErrOverBudget in pipeline;
// verifies GET /api/checks/{id} returns status error with code: "budget_exhausted",
// detail: {"pool": "token_factory"}; verifies error event on the stream carries the matching envelope.
func TestCheckFailureSurfacesBudgetExhausted(t *testing.T) {
	env := newTestServerEnv(t, 4)

	// Simulate Token Factory budget exhausted.
	env.searcher.mu.Lock()
	env.searcher.failErr = fmt.Errorf("%w: token_factory budget exhausted", cost.ErrOverBudget)
	env.searcher.mu.Unlock()

	reqBody := `{"title": "Fortnite", "age": 11}`
	resp, err := http.Post(env.srv.URL+"/api/checks", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	var startResp struct {
		CheckID string `json:"check_id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&startResp)

	// Read stream until error terminal
	req, _ := http.NewRequest(http.MethodGet, env.srv.URL+"/api/checks/"+startResp.CheckID+"/events", nil)
	req.Header.Set("Accept", "text/event-stream")
	sResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer sResp.Body.Close()

	br := bufio.NewReader(sResp.Body)
	var sawErrorEvent bool
	for {
		frame, err := wire.ReadEvent(br)
		if err != nil {
			break
		}
		if frame.Event == wire.EventError {
			sawErrorEvent = true
			var ee wire.ErrorEvent
			if err := json.Unmarshal(frame.Data, &ee); err != nil {
				t.Fatalf("unmarshal error event: %v", err)
			}
			var envelope struct {
				Error struct {
					Code    string            `json:"code"`
					Message string            `json:"message"`
					Detail  map[string]string `json:"detail"`
				} `json:"error"`
			}
			if err := json.Unmarshal(ee.Error, &envelope); err != nil {
				t.Fatalf("unmarshal envelope: %v", err)
			}
			if envelope.Error.Code != "budget_exhausted" {
				t.Errorf("error event code = %q; want budget_exhausted", envelope.Error.Code)
			}
			if envelope.Error.Detail["pool"] != "token_factory" {
				t.Errorf("error event pool = %q; want token_factory", envelope.Error.Detail["pool"])
			}
			if envelope.Error.Message != "the Token Factory budget is spent" {
				t.Errorf("error event message = %q; want 'the Token Factory budget is spent'", envelope.Error.Message)
			}
			break
		}
	}
	if !sawErrorEvent {
		t.Fatal("did not see error event on stream")
	}

	// Query GET /api/checks/{id}
	cRes, err := http.Get(env.srv.URL + "/api/checks/" + startResp.CheckID)
	if err != nil {
		t.Fatalf("GET check: %v", err)
	}
	defer cRes.Body.Close()
	if cRes.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", cRes.StatusCode)
	}
	var checkResp struct {
		ID      string            `json:"id"`
		Status  string            `json:"status"`
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Detail  map[string]string `json:"detail"`
	}
	if err := json.NewDecoder(cRes.Body).Decode(&checkResp); err != nil {
		t.Fatalf("decode check: %v", err)
	}
	if checkResp.Status != "error" {
		t.Errorf("checkResp.Status = %q, want error", checkResp.Status)
	}
	if checkResp.Code != "budget_exhausted" {
		t.Errorf("checkResp.Code = %q, want budget_exhausted", checkResp.Code)
	}
	if checkResp.Message != "the Token Factory budget is spent" {
		t.Errorf("checkResp.Message = %q, want 'the Token Factory budget is spent'", checkResp.Message)
	}
	if checkResp.Detail["pool"] != "token_factory" {
		t.Errorf("checkResp.Detail.pool = %q, want token_factory", checkResp.Detail["pool"])
	}
}

// TestCheckFailureSurfacesNoSources simulates pipeline.ErrNoSources;
// verifies GET /api/checks/{id} returns code: "no_sources".
func TestCheckFailureSurfacesNoSources(t *testing.T) {
	env := newTestServerEnv(t, 4)

	// Simulate empty docs to trigger pipeline.ErrNoSources.
	env.store.mu.Lock()
	env.store.empty = true
	env.store.mu.Unlock()
	env.searcher.mu.Lock()
	env.searcher.emptyDocs = true
	env.searcher.mu.Unlock()

	reqBody := `{"title": "ObscureGame", "age": 7}`
	resp, err := http.Post(env.srv.URL+"/api/checks", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	var startResp struct {
		CheckID string `json:"check_id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&startResp)

	// Poll GET /api/checks/{id}
	deadline := time.Now().Add(5 * time.Second)
	var checkResp struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	for time.Now().Before(deadline) {
		res, err := http.Get(env.srv.URL + "/api/checks/" + startResp.CheckID)
		if err != nil {
			t.Fatalf("GET check: %v", err)
		}
		_ = json.NewDecoder(res.Body).Decode(&checkResp)
		res.Body.Close()
		if checkResp.Status == "error" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if checkResp.Status != "error" {
		t.Fatalf("check status = %q; want error", checkResp.Status)
	}
	if checkResp.Code != "no_sources" {
		t.Errorf("check code = %q; want no_sources", checkResp.Code)
	}
	if checkResp.Message != "nothing could be read about that title; check the spelling, or try its full name" {
		t.Errorf("check message = %q", checkResp.Message)
	}
}

// TestCheckConcurrencyLimitBusy: when kind is at its concurrency limit,
// POST /api/checks returns 503 code: "busy", detail.retry_after_seconds: 20, and Retry-After: 20 header.
func TestCheckConcurrencyLimitBusy(t *testing.T) {
	env := newTestServerEnv(t, 1) // concurrency limit = 1

	gate := make(chan struct{})
	env.searcher.mu.Lock()
	env.searcher.blockChan = gate
	env.searcher.mu.Unlock()

	// First check starts and claims the single slot.
	resp1, err := http.Post(env.srv.URL+"/api/checks", "application/json", strings.NewReader(`{"title": "Check1", "age": 8}`))
	if err != nil {
		t.Fatalf("first POST: %v", err)
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusAccepted {
		t.Fatalf("first status = %d, want 202", resp1.StatusCode)
	}
	b1, _ := io.ReadAll(resp1.Body)
	first := checkIDOf(t, b1)

	// Second check arrives while slot is occupied.
	resp2, err := http.Post(env.srv.URL+"/api/checks", "application/json", strings.NewReader(`{"title": "Check2", "age": 8}`))
	if err != nil {
		t.Fatalf("second POST: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("second status = %d; want 503", resp2.StatusCode)
	}
	if ra := resp2.Header.Get("Retry-After"); ra != "20" {
		t.Errorf("Retry-After header = %q; want 20", ra)
	}

	var errResp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Detail  struct {
				RetryAfterSeconds int `json:"retry_after_seconds"`
			} `json:"detail"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if errResp.Error.Code != "busy" {
		t.Errorf("code = %q; want busy", errResp.Error.Code)
	}
	if errResp.Error.Message != "too many checks are running; try again shortly" {
		t.Errorf("message = %q", errResp.Error.Message)
	}
	if errResp.Error.Detail.RetryAfterSeconds != 20 {
		t.Errorf("retry_after_seconds = %d; want 20", errResp.Error.Detail.RetryAfterSeconds)
	}

	// Release the first check and let it finish, so its progress and
	// terminal writes land before the temp dir is removed.
	close(gate)
	waitCheckDone(t, env, first)
}

// TestCheckInterruptedOnRestart: non-idempotent check job interrupted across runner restarts,
// marks status interrupted with code check_failed and does NOT restart.
func TestCheckInterruptedOnRestart(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "restart.db"))
	cardStore, err := cards.Open(context.Background(), db)
	if err != nil {
		t.Fatalf("cards.Open: %v", err)
	}
	broker1 := stream.New(stream.Config{})
	runner1, err := NewRunner(db, broker1, 4, nil, nil)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	// Start a job that blocks until cancelled.
	started := make(chan struct{})
	jid, err := runner1.StartKind(context.Background(), JobKind, func(ctx context.Context, report func(job.Progress)) ([]byte, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatalf("StartKind: %v", err)
	}

	<-started

	// Verify it is running in runner1.
	res, err := runner1.Result(context.Background(), jid)
	if err != nil || res.Status != job.StatusRunning {
		t.Fatalf("res = (%v, %v); want StatusRunning", res, err)
	}

	// Now simulate restart: open runner 2 on the same database.
	broker2 := stream.New(stream.Config{})
	runner2, err := NewRunner(db, broker2, 4, nil, nil)
	if err != nil {
		t.Fatalf("NewRunner2: %v", err)
	}

	// Query via Server handler.
	srv := &Server{
		Runner: runner2,
		Broker: broker2,
		Cards:  cardStore,
	}
	h, err := srv.Handler()
	if err != nil {
		t.Fatalf("srv.Handler: %v", err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/checks/j_" + jid)
	if err != nil {
		t.Fatalf("GET check: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var checkResp struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&checkResp); err != nil {
		t.Fatalf("decode check: %v", err)
	}

	if checkResp.ID != "j_"+jid {
		t.Errorf("ID = %q; want j_%s", checkResp.ID, jid)
	}
	if checkResp.Status != "interrupted" {
		t.Errorf("Status = %q; want interrupted", checkResp.Status)
	}
	if checkResp.Code != "check_failed" {
		t.Errorf("Code = %q; want check_failed", checkResp.Code)
	}
	wantMsg := "The server restarted during this check. Ask again; sources already read are reused."
	if checkResp.Message != wantMsg {
		t.Errorf("Message = %q; want %q", checkResp.Message, wantMsg)
	}

	// Verify with Attempts: length must be 1 (never resumed).
	attempts, err := runner2.Attempts(context.Background(), jid)
	if err != nil {
		t.Fatalf("Attempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Errorf("len(attempts) = %d; want 1 (non-idempotent job must not restart)", len(attempts))
	}
}

// TestCheckInvalidRequest: 400 validation for empty title and invalid age.
func TestCheckInvalidRequest(t *testing.T) {
	env := newTestServerEnv(t, 4)

	cases := []struct {
		name string
		body string
	}{
		{"empty title", `{"title": "", "age": 9}`},
		{"age too old", `{"title": "Roblox", "age": 40}`},
		{"age too young", `{"title": "Roblox", "age": 1}`},
		{"invalid json", `not-json`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Post(env.srv.URL+"/api/checks", "application/json", strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("POST: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d; want 400", resp.StatusCode)
			}
			var errResp struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if errResp.Error.Code != "invalid_request" {
				t.Errorf("code = %q; want invalid_request", errResp.Error.Code)
			}
			if errResp.Error.Message != "title is required and age must be between 3 and 17" {
				t.Errorf("message = %q; want 'title is required and age must be between 3 and 17'", errResp.Error.Message)
			}
		})
	}
}

// TestUnknownCheckAndCard404: 404 for unknown or malformed check/card IDs.
func TestUnknownCheckAndCard404(t *testing.T) {
	env := newTestServerEnv(t, 4)

	unknownKeelID, _ := id.New()

	cases := []struct {
		name        string
		path        string
		wantMessage string
	}{
		{"malformed check id", "/api/checks/j_unknown", "no such check"},
		{"bare check id without prefix", "/api/checks/" + unknownKeelID, "no such check"},
		{"unknown valid check id", "/api/checks/j_" + unknownKeelID, "no such check"},
		{"malformed card id", "/api/cards/c_unknown", "no such card"},
		{"bare card id without prefix", "/api/cards/" + unknownKeelID, "no such card"},
		{"unknown valid card id", "/api/cards/c_" + unknownKeelID, "no such card"},
		{"malformed check events id", "/api/checks/j_unknown/events", "no such check"},
		{"bare check events id", "/api/checks/" + unknownKeelID + "/events", "no such check"},
		{"unknown valid check events id", "/api/checks/j_" + unknownKeelID + "/events", "no such check"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(env.srv.URL + tc.path)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNotFound {
				b, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d; want 404; body = %s", resp.StatusCode, b)
			}
			var errResp struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if errResp.Error.Code != "not_found" {
				t.Errorf("code = %q; want not_found", errResp.Error.Code)
			}
			if errResp.Error.Message != tc.wantMessage {
				t.Errorf("message = %q; want %q", errResp.Error.Message, tc.wantMessage)
			}
		})
	}
}

// TestRecentCardsEndpoint: matches contract {"cards": [...]}.
func TestRecentCardsEndpoint(t *testing.T) {
	env := newTestServerEnv(t, 4)

	// 1. When empty: returns {"cards": []}
	resp, err := http.Get(env.srv.URL + "/api/cards")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var emptyResp struct {
		Cards []cards.Summary `json:"cards"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&emptyResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if emptyResp.Cards == nil || len(emptyResp.Cards) != 0 {
		t.Fatalf("cards = %+v; want empty slice", emptyResp.Cards)
	}

	// 2. Put 3 cards in store
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	c1 := pipeline.Card{
		Title:            "Roblox",
		Age:              9,
		ChecklistVersion: checklist.Version,
		CheckedAt:        now,
		Verdict:          checklist.No,
	}
	c2 := pipeline.Card{
		Title:            "Minecraft",
		Age:              9,
		ChecklistVersion: checklist.Version,
		CheckedAt:        now.Add(time.Minute),
		Verdict:          checklist.YesWithSettings,
	}
	c3 := pipeline.Card{
		Title:            "Khan Academy Kids",
		Age:              5,
		ChecklistVersion: checklist.Version,
		CheckedAt:        now.Add(2 * time.Minute),
		Verdict:          checklist.Yes,
	}

	_, err = env.cardStore.Put(context.Background(), c1)
	if err != nil {
		t.Fatalf("put c1: %v", err)
	}
	_, err = env.cardStore.Put(context.Background(), c2)
	if err != nil {
		t.Fatalf("put c2: %v", err)
	}
	_, err = env.cardStore.Put(context.Background(), c3)
	if err != nil {
		t.Fatalf("put c3: %v", err)
	}

	// 3. Query GET /api/cards
	resp2, err := http.Get(env.srv.URL + "/api/cards")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp2.StatusCode)
	}
	var cardsResp struct {
		Cards []struct {
			ID               string            `json:"id"`
			Title            string            `json:"title"`
			Age              int               `json:"age"`
			Verdict          checklist.Verdict `json:"verdict"`
			ChecklistVersion string            `json:"checklist_version"`
			CheckedAt        time.Time         `json:"checked_at"`
		} `json:"cards"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&cardsResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(cardsResp.Cards) != 3 {
		t.Fatalf("len(cards) = %d; want 3", len(cardsResp.Cards))
	}
	for _, c := range cardsResp.Cards {
		if !strings.HasPrefix(c.ID, "c_") {
			t.Errorf("card ID %q does not start with c_", c.ID)
		}
		if c.ChecklistVersion != checklist.Version {
			t.Errorf("card checklist_version = %q; want %q", c.ChecklistVersion, checklist.Version)
		}
	}
	if cardsResp.Cards[0].Title != "Khan Academy Kids" {
		t.Errorf("cards[0] = %q; want newest 'Khan Academy Kids'", cardsResp.Cards[0].Title)
	}
}

// TestStartResponseJSON verifies that the response shapes match testdata contract JSONs.
func TestStartResponseJSON(t *testing.T) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	_ = enc.Encode(map[string]string{"card_id": "c_00000000000000000000000000000001"})
	if !strings.Contains(buf.String(), `"card_id":"c_00000000000000000000000000000001"`) {
		t.Errorf("card_id json: %s", buf.String())
	}

	buf.Reset()
	_ = enc.Encode(map[string]string{"check_id": "j_00000000000000000000000000000001"})
	if !strings.Contains(buf.String(), `"check_id":"j_00000000000000000000000000000001"`) {
		t.Errorf("check_id json: %s", buf.String())
	}
}

// blockTavily puts a gate in front of the env's fake Tavily host: every
// Tavily call, on our keys or a visitor's, waits until release is closed.
// It returns release.
func blockTavily(t *testing.T, e *byokEnv) chan struct{} {
	t.Helper()
	ownMeter(t, e)
	orig := e.srv.Pipeline.Gatherer.Search.(*tavily.Client)
	target, err := url.Parse(orig.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	proxy := httputil.NewSingleHostReverseProxy(target)
	ps := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(ps.Close)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	gated := tavily.New(ps.URL, func() (string, error) { return ourTavilyKey, nil })
	gated.Retry = orig.Retry
	e.srv.Pipeline.Gatherer.Search = gated
	return release
}

// ownMeter moves the env's daily cap onto a meter with its own database
// file, the layout serve uses (see feedback.md: a meter sharing the app's
// file loses bookings to SQLITE_BUSY under concurrent checks).
func ownMeter(t *testing.T, e *byokEnv) {
	t.Helper()
	st, err := e.meter.Status(context.Background(), spend.PoolChecksDaily)
	if err != nil {
		t.Fatal(err)
	}
	m, err := spend.Open(context.Background(), spend.Config{
		Path:  filepath.Join(t.TempDir(), "spend.db"),
		Pools: map[spend.Pool]spend.PoolConfig{spend.PoolChecksDaily: {Limit: st.Limit}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	e.meter, e.srv.Checks, e.srv.Pipeline.Checks = m, m, m
}

func checkIDOf(t *testing.T, b []byte) string {
	t.Helper()
	var s struct {
		CheckID string `json:"check_id"`
	}
	_ = json.Unmarshal(b, &s)
	return s.CheckID
}

func remainingToday(t *testing.T, e *byokEnv) cost.Price {
	t.Helper()
	st, err := e.meter.Status(context.Background(), spend.PoolChecksDaily)
	if err != nil {
		t.Fatal(err)
	}
	return st.Remaining
}

// Concurrent identical requests (same normalized title and age) get one
// check_id, run one job and hold one daily-cap reservation; once the check
// is done, the next identical request gets the card.
func TestDoubleClickJoinsRunningCheck(t *testing.T) {
	e := newBYOKEnv(t, 5)
	release := blockTavily(t, e)

	titles := []string{"Roblox", "roblox", "  Roblox ", "ROBLOX", "Roblox"}
	ids := make([]string, len(titles))
	codes := make([]int, len(titles))
	var wg sync.WaitGroup
	for i, title := range titles {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, b := byokPost(t, e.url, fmt.Sprintf(`{"title":%q,"age":9}`, title), nil)
			codes[i], ids[i] = resp.StatusCode, checkIDOf(t, b)
		}()
	}
	wg.Wait()
	for i := range titles {
		if codes[i] != http.StatusAccepted || ids[i] == "" || ids[i] != ids[0] {
			t.Fatalf("responses %v ids %v, want all 202 with one check_id", codes, ids)
		}
	}
	if got := remainingToday(t, e); got != 4 {
		t.Errorf("remaining today %d while running, want 4 (one reservation)", got)
	}
	// A different age is a different check.
	resp, b := byokPost(t, e.url, `{"title":"Roblox","age":12}`, nil)
	if resp.StatusCode != http.StatusAccepted || checkIDOf(t, b) == ids[0] {
		t.Errorf("other age: status %d id %s, want a new check", resp.StatusCode, checkIDOf(t, b))
	}

	close(release)
	if st, body := waitCheck(t, e.url, ids[0]); st != "done" {
		t.Fatalf("joined check: %s %s", st, body)
	}
	resp, b = byokPost(t, e.url, `{"title":"roblox","age":9}`, nil)
	var hit struct {
		CardID string `json:"card_id"`
	}
	_ = json.Unmarshal(b, &hit)
	if resp.StatusCode != http.StatusOK || hit.CardID == "" {
		t.Errorf("after done: status %d body %s, want 200 with the card", resp.StatusCode, b)
	}
	// Only the 9 and the 12 were booked: the joined requests reserved nothing.
	waitCheck(t, e.url, checkIDOf(t, func() []byte { _, b := byokPost(t, e.url, `{"title":"Roblox","age":12}`, nil); return b }()))
	if got := checksSpentToday(t, e.meter); got != 2 {
		st, _ := e.meter.Status(context.Background(), spend.PoolChecksDaily)
		t.Errorf("checks booked %d, want 2 (%+v)", got, st)
	}
}

// A visitor-key request never joins a check on our keys: it starts its
// own job and reserves nothing.
func TestVisitorRequestNeverJoinsOurCheck(t *testing.T) {
	e := newBYOKEnv(t, 5)
	release := blockTavily(t, e)

	_, b := byokPost(t, e.url, `{"title":"Roblox","age":9}`, nil)
	ours := checkIDOf(t, b)
	_, b = byokPost(t, e.url, `{"title":"Roblox","age":9}`, visitorHeaders())
	visitor := checkIDOf(t, b)
	if ours == "" || visitor == "" || ours == visitor {
		t.Fatalf("ours %q visitor %q, want two separate checks", ours, visitor)
	}
	if got := remainingToday(t, e); got != 4 {
		t.Errorf("remaining %d, want 4 (the visitor reserves nothing)", got)
	}
	close(release)
	for _, id := range []string{ours, visitor} {
		if st, body := waitCheck(t, e.url, id); st != "done" {
			t.Errorf("%s: %s %s", id, st, body)
		}
	}
	// Each check judged on its own keys. The evidence collect for the
	// same title is one single flight across both (T4.2), so only one of
	// them paid Tavily; see the T4.6 notes.
	if e.tf.saw(visitorTF) == 0 || e.tf.saw(ourTFKey) == 0 {
		t.Error("each check should have judged on its own Token Factory key")
	}
	if got := checksSpentToday(t, e.meter); got != 1 {
		t.Errorf("checks booked %d, want 1", got)
	}
}

// A check on a visitor's keys is never joined, by our-key requests or by
// another visitor's.
func TestVisitorCheckIsNeverJoined(t *testing.T) {
	e := newBYOKEnv(t, 5)
	release := blockTavily(t, e)

	_, b := byokPost(t, e.url, `{"title":"Fortnite","age":11}`, visitorHeaders())
	v1 := checkIDOf(t, b)
	_, b = byokPost(t, e.url, `{"title":"Fortnite","age":11}`, visitorHeaders())
	v2 := checkIDOf(t, b)
	_, b = byokPost(t, e.url, `{"title":"Fortnite","age":11}`, nil)
	ours := checkIDOf(t, b)
	if v1 == "" || v2 == "" || ours == "" || v1 == v2 || v1 == ours || v2 == ours {
		t.Fatalf("visitor %q, visitor %q, ours %q: want three separate checks", v1, v2, ours)
	}
	if got := remainingToday(t, e); got != 4 {
		t.Errorf("remaining %d, want 4 (only our check holds one)", got)
	}
	close(release)
	for _, id := range []string{v1, v2, ours} {
		if st, body := waitCheck(t, e.url, id); st != "done" {
			t.Errorf("%s: %s %s", id, st, body)
		}
	}
	if got := checksSpentToday(t, e.meter); got != 1 {
		t.Errorf("checks booked %d, want 1", got)
	}
}

// Joining comes before live_checks_off (docs/api.md step 4 before step 5):
// with the switch turned off while a check runs, the identical request
// still joins it, and a new title is refused.
func TestJoinBeforeLiveSwitch(t *testing.T) {
	e := newGatedEnv(t, GateConfig{Rule: gate.Rule{
		Name:      "checks",
		PerClient: gate.Limit{Burst: 100, Every: time.Second},
		Global:    gate.Limit{Burst: 100, Every: time.Second},
	}})
	block := make(chan struct{})
	e.searcher.mu.Lock()
	e.searcher.blockChan = block
	e.searcher.mu.Unlock()
	defer close(block)

	resp, b := postCheck(t, e.url, `{"title":"Roblox","age":9}`, nil)
	first := checkIDOf(t, b)
	if resp.StatusCode != http.StatusAccepted || first == "" {
		t.Fatalf("start: %d %s", resp.StatusCode, b)
	}
	if _, err := e.flags.SetBool(context.Background(), e.live, false); err != nil {
		t.Fatal(err)
	}
	resp, b = postCheck(t, e.url, `{"title":"roblox","age":9}`, nil)
	if resp.StatusCode != http.StatusAccepted || checkIDOf(t, b) != first {
		t.Errorf("join with live off: %d %s, want 202 with %s", resp.StatusCode, b, first)
	}
	resp, b = postCheck(t, e.url, `{"title":"Fortnite","age":11}`, nil)
	if c, _ := errCode(t, b); resp.StatusCode != http.StatusServiceUnavailable || c != CodeLiveChecksOff {
		t.Errorf("new title with live off: %d %s, want 503 live_checks_off", resp.StatusCode, b)
	}
}

// Round-1 M1, the reviewer's reproduction: with live off, the gate lets a
// request through to join a running check; the check then fails and
// leaves the table before the handler takes its lock. The handler must
// refuse live_checks_off and start nothing.
func TestJoinOnlyRequestNeverStartsCheckWhenLiveOff(t *testing.T) {
	e := newGatedEnv(t, GateConfig{Rule: gate.Rule{
		Name:      "checks",
		PerClient: gate.Limit{Burst: 100, Every: time.Second},
		Global:    gate.Limit{Burst: 100, Every: time.Second},
	}})
	block := make(chan struct{})
	e.searcher.mu.Lock()
	e.searcher.blockChan = block
	e.searcher.failErr = errors.New("search failed")
	e.searcher.mu.Unlock()

	resp, b := postCheck(t, e.url, `{"title":"Roblox","age":9}`, nil)
	first := checkIDOf(t, b)
	if resp.StatusCode != http.StatusAccepted || first == "" {
		t.Fatalf("start: %d %s", resp.StatusCode, b)
	}
	if _, err := e.flags.SetBool(context.Background(), e.live, false); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"title":"Roblox","age":9}`)
	probe := httptest.NewRequest(http.MethodPost, "/api/checks", bytes.NewReader(body))
	if !e.server.joinsRunning(probe, body) {
		t.Fatal("the gate would not pass the request as a joiner")
	}
	// The running check fails and leaves the table.
	close(block)
	if st, _ := waitCheck(t, e.url, first); st != "error" {
		t.Fatalf("first check ended %s, want error", st)
	}
	inner, err := e.server.Handler()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/checks", bytes.NewReader(body))
	req = req.WithContext(withJoinOnly(req.Context()))
	rec := httptest.NewRecorder()
	inner.ServeHTTP(rec, req)
	if c, _ := errCode(t, rec.Body.Bytes()); rec.Code != http.StatusServiceUnavailable || c != CodeLiveChecksOff {
		t.Fatalf("join-only after the target vanished: %d %s, want 503 live_checks_off", rec.Code, rec.Body)
	}
	// Through the whole gated path too.
	resp, b = postCheck(t, e.url, `{"title":"Roblox","age":9}`, nil)
	if c, _ := errCode(t, b); resp.StatusCode != http.StatusServiceUnavailable || c != CodeLiveChecksOff {
		t.Errorf("gated: %d %s, want 503 live_checks_off", resp.StatusCode, b)
	}
	e.searcher.mu.Lock()
	calls := e.searcher.searchCalls
	e.searcher.mu.Unlock()
	if calls != 1 {
		t.Errorf("search calls %d, want 1 (only the first check ran)", calls)
	}
}

// Round-1 L1: a check that finishes (card stored, entry gone) between the
// handler's cache read and its lock is not started again: the handler
// reads the card cache again under the lock.
func TestCacheRecheckedUnderJoinLock(t *testing.T) {
	e := newBYOKEnv(t, 5)
	ownMeter(t, e)
	e.srv.joins.mu.Lock()
	type result struct {
		code int
		body []byte
	}
	done := make(chan result, 1)
	go func() {
		resp, b := byokPost(t, e.url, `{"title":"Roblox","age":9}`, nil)
		done <- result{resp.StatusCode, b}
	}()
	// Let the request read the cache (miss) and wait on the lock.
	time.Sleep(200 * time.Millisecond)
	cid, err := e.srv.Cards.Put(context.Background(), pipeline.Card{
		Title: "Roblox", Age: 9, ChecklistVersion: checklist.Version,
		CheckedAt: time.Now().UTC(), Verdict: checklist.Unclear,
	})
	if err != nil {
		e.srv.joins.mu.Unlock()
		t.Fatal(err)
	}
	e.srv.joins.mu.Unlock()
	r := <-done
	var hit struct {
		CardID string `json:"card_id"`
	}
	_ = json.Unmarshal(r.body, &hit)
	if r.code != http.StatusOK || strings.TrimPrefix(hit.CardID, "c_") != strings.TrimPrefix(cid, "c_") {
		t.Fatalf("status %d body %s, want 200 with the new card", r.code, r.body)
	}
	if got := remainingToday(t, e); got != 5 {
		t.Errorf("remaining %d, want 5 (nothing reserved)", got)
	}
	if e.tv.saw(ourTavilyKey) != 0 {
		t.Error("a check started")
	}
}

// longPolicy is a policy page long enough to span several reader chunks.
func longPolicy() string {
	return strings.Repeat("Chat is moderated by staff and data is retained for thirty days. ", 900)
}

// slowChatter answers like fakeChatter after a pause, or holds every call
// after the first hold calls until release closes, so a test sees a read
// in flight.
type slowChatter struct {
	fakeChatter
	delay   time.Duration
	hold    int64
	release chan struct{}
	n       atomic.Int64
}

func (c *slowChatter) Chat(ctx context.Context, msgs []model.Message, opts model.Options) (model.Reply, error) {
	if c.release != nil && c.n.Add(1) > c.hold {
		select {
		case <-c.release:
		case <-ctx.Done():
			return model.Reply{}, ctx.Err()
		}
	}
	if c.delay > 0 {
		time.Sleep(c.delay)
	}
	return c.fakeChatter.Chat(ctx, msgs, opts)
}

// TestProgressGatherThenRisingReadThenJudge is the T4.7 Done-when: a check
// on a slow fake pipeline reports gather, then read with a count that
// rises chunk by chunk to its total, then judge, in that order.
func TestProgressGatherThenRisingReadThenJudge(t *testing.T) {
	env := newTestServerEnv(t, 4)
	env.searcher.pageText = longPolicy()
	p := env.server.Pipeline
	p.Reader.Model = &slowChatter{fakeChatter: fakeChatter{replyText: "[]"}, delay: 15 * time.Millisecond}

	type seen struct {
		stage, msg string
		cur, tot   int64
		counted    bool
	}
	var mu sync.Mutex
	var got []seen
	report := func(pr job.Progress) {
		var d struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(pr.Detail, &d)
		s := seen{stage: pr.Stage, msg: d.Message}
		if pr.Current != nil && pr.Total != nil {
			s.cur, s.tot, s.counted = *pr.Current, *pr.Total, true
		}
		mu.Lock()
		got = append(got, s)
		mu.Unlock()
	}
	if _, err := env.server.work(p, pipeline.Request{Title: "Minecraft", Age: 9}, "t47")(context.Background(), report); err != nil {
		t.Fatalf("work: %v", err)
	}

	order := map[string]int{"gather": 0, "read": 1, "judge": 2}
	last := -1
	var gathers []string
	var counts []int64
	var total int64
	for _, s := range got {
		o, ok := order[s.stage]
		if !ok {
			continue
		}
		if o < last {
			t.Fatalf("stage %q after a later stage: %+v", s.stage, got)
		}
		last = o
		if s.msg == "" {
			t.Errorf("stage %q with no message", s.stage)
		}
		switch s.stage {
		case "gather":
			gathers = append(gathers, s.msg)
		case "read":
			if s.counted {
				counts = append(counts, s.cur)
				total = s.tot
			}
		}
	}
	if last != 2 {
		t.Fatalf("never reached judge: %+v", got)
	}
	if !slices.Contains(gathers, "searching for the privacy policy") || !slices.Contains(gathers, "looking up the App Store listing") {
		t.Errorf("gather did not say what it was fetching: %q", gathers)
	}
	if total < 2 {
		t.Fatalf("read total = %d, want several chunks (counts %v)", total, counts)
	}
	if len(counts) != int(total)+1 || counts[0] != 0 || counts[len(counts)-1] != total {
		t.Fatalf("read counts = %v, want 0..%d", counts, total)
	}
	for i := 1; i < len(counts); i++ {
		if counts[i] <= counts[i-1] {
			t.Fatalf("read counts not rising: %v", counts)
		}
	}
}

// TestGetCheckMidReadShowsStageAndCount: while the read is under way, GET
// /api/checks/{id} answers with the latest stage, message and counts, so a
// page opened mid-read shows them at once.
func TestGetCheckMidReadShowsStageAndCount(t *testing.T) {
	env := newTestServerEnv(t, 4)
	env.searcher.pageText = longPolicy()
	release := make(chan struct{})
	p := env.server.Pipeline
	p.Reader.Concurrency = 1
	p.Reader.Model = &slowChatter{fakeChatter: fakeChatter{replyText: "[]"}, hold: 1, release: release}
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()

	resp, err := http.Post(env.srv.URL+"/api/checks", "application/json", strings.NewReader(`{"title":"Minecraft","age":9}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	cid := checkIDOf(t, b)

	type running struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Stage   string `json:"stage"`
		Message string `json:"message"`
		Current *int64 `json:"current"`
		Total   *int64 `json:"total"`
	}
	var r running
	deadline := time.Now().Add(10 * time.Second)
	for {
		res, err := http.Get(env.srv.URL + "/api/checks/" + cid)
		if err != nil {
			t.Fatal(err)
		}
		r = running{}
		_ = json.NewDecoder(res.Body).Decode(&r)
		res.Body.Close()
		if r.Stage == "read" && r.Current != nil && *r.Current >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never saw a counted read: %+v", r)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r.Status != "running" || r.Message == "" || r.Total == nil || *r.Current != 1 || *r.Total <= *r.Current {
		t.Fatalf("mid-read GET = %+v, want running, a message, 1 of several", r)
	}
	close(release)
	waitCheckDone(t, env, cid)
}

func waitCheckDone(t *testing.T, env *testServerEnv, cid string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get(env.srv.URL + "/api/checks/" + cid)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		if out["status"] != "running" && out["status"] != "queued" {
			if _, ok := out["stage"]; ok {
				t.Errorf("finished check still carries a stage: %v", out)
			}
			return out
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("check never finished")
	return nil
}

var segRefLine = regexp.MustCompile(`(?m)^\[([RD]\d+)\] \(`)
var findingID = regexp.MustCompile(`id=(\S+) area=contact`)

// flagReader reports one contact risk per segment, quoting the policy text
// the test sets (the rule gate needs a quote about strangers reaching a child).
type flagReader struct{ fakeChatter }

func (c *flagReader) Chat(ctx context.Context, msgs []model.Message, opts model.Options) (model.Reply, error) {
	user := msgs[len(msgs)-1].Content
	var out []finding.Raw
	for _, m := range segRefLine.FindAllStringSubmatch(user, -1) {
		out = append(out, finding.Raw{Ref: m[1], Area: "contact", Stance: "risk", Claim: "Chat is open", Quote: "strangers can chat with children"})
	}
	if out == nil {
		out = []finding.Raw{}
	}
	b, _ := json.Marshal(map[string]any{"findings": out})
	return model.Reply{Text: string(b), Measured: true}, nil
}

// flagJudge flags contact on the first contact finding and grades the rest ok.
type flagJudge struct{ fakeChatter }

func (c *flagJudge) Chat(ctx context.Context, msgs []model.Message, opts model.Options) (model.Reply, error) {
	user := msgs[len(msgs)-1].Content
	var gs []checklist.Grade
	for _, a := range checklist.Areas() {
		g := checklist.Grade{Area: a.ID, Level: checklist.OK, Reason: "All ok.", Findings: []string{}}
		if m := findingID.FindStringSubmatch(user); a.ID == checklist.Contact && m != nil {
			g = checklist.Grade{Area: a.ID, Level: checklist.Flag, Reason: "Chat is open.", Findings: []string{m[1]}}
		}
		gs = append(gs, g)
	}
	b, _ := json.Marshal(map[string]any{"grades": gs})
	return model.Reply{Text: string(b), Measured: true}, nil
}

// lockedBuf is a log sink a job goroutine and the test share.
type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// TestAlternativesFailureLoggedCardStored: a "no" card whose alternatives
// search fails on something other than the budget is still stored, and
// the failure is logged with its upstream and status, never the body.
func TestAlternativesFailureLoggedCardStored(t *testing.T) {
	env := newTestServerEnv(t, 4)
	var logs lockedBuf
	env.server.Log = slog.New(slog.NewTextHandler(&logs, nil))
	env.searcher.failQuery = "safer "
	env.searcher.pageText = "Full policy document: strangers can chat with children and data is retained."
	p := env.server.Pipeline
	p.Reader.Model = &flagReader{}
	p.Judge.Model = &flagJudge{}

	resp, err := http.Post(env.srv.URL+"/api/checks", "application/json", strings.NewReader(`{"title":"Minecraft","age":9}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	out := waitCheckDone(t, env, checkIDOf(t, b))
	if out["status"] != "done" {
		t.Fatalf("check = %v, want done with the card stored", out)
	}
	cardID, _ := out["card_id"].(string)
	res, err := http.Get(env.srv.URL + "/api/cards/" + cardID)
	if err != nil {
		t.Fatal(err)
	}
	var card struct {
		Verdict checklist.Verdict `json:"verdict"`
	}
	_ = json.NewDecoder(res.Body).Decode(&card)
	res.Body.Close()
	if card.Verdict != checklist.No {
		t.Fatalf("card verdict = %s, want no (the alternatives path must run)", card.Verdict)
	}
	got := logs.String()
	if !strings.Contains(got, "alternatives failed") || !strings.Contains(got, "tavily HTTP 502") {
		t.Fatalf("alternatives failure not logged: %q", got)
	}
	if strings.Contains(got, "SECRETKEY") || strings.Contains(got, "upstream said") {
		t.Fatalf("log carries the upstream body: %q", got)
	}
}
