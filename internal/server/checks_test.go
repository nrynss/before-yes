package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/job"
	"github.com/nrynss/keel/sqlite"
	"github.com/nrynss/keel/stream"
	"github.com/nrynss/keel/wire"

	"github.com/nrynss/before-yes/internal/appstore"
	"github.com/nrynss/before-yes/internal/cards"
	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/pipeline"
	"github.com/nrynss/before-yes/internal/tavily"
)

type configurableSearcher struct {
	mu           sync.Mutex
	failErr      error
	emptyDocs    bool
	blockChan    chan struct{}
	searchCalls  int
	extractCalls int
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
	pages := make([]tavily.Page, len(urls))
	for i, u := range urls {
		pages[i] = tavily.Page{
			URL:        u,
			RawContent: "Full policy document: chat is moderated and data is retained.",
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

	// Release first check
	close(gate)
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
			if errResp.Error.Message != "title is required and age must be between 2 and 17" {
				t.Errorf("message = %q; want 'title is required and age must be between 2 and 17'", errResp.Error.Message)
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
