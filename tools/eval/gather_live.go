package main

// A live gather records the App Store and Tavily replies for one title, in
// the same layout the fixture server replays, under
// testdata/eval/gathers/<n>/<slug>/. That directory is a sibling of
// testdata/eval/replies/<slug>/, so a live gather never overwrites the
// frozen fixture recordings. A file that is already there is served and the
// host is not called, so running a saved gather again spends nothing on
// that call.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nrynss/before-yes/internal/pipeline"
)

const (
	itunesHost = "https://itunes.apple.com"
	tavilyHost = "https://api.tavily.com"
)

// searchFile is the fixture name of one planned search, or "" when query
// is not in the plan. The plan is matched with an empty host, as the
// fixture server matches: the host only adds include_domains, not a new query.
func searchFile(title, query string) string {
	for _, q := range pipeline.Plan(title, "") {
		if q.Search.Query == query {
			return "tavily-search-" + q.Name + ".json"
		}
	}
	return ""
}

// extractRecordName names one extract call by its URL list, in request
// order, so a replay of the same call returns the same body.
func extractRecordName(urls []string) string {
	sum := sha256.Sum256([]byte(strings.Join(urls, "\n")))
	return "tavily-extract-" + hex.EncodeToString(sum[:8]) + ".json"
}

// gatherRecorder is an App Store and Tavily stand-in that forwards a miss
// to the real hosts and saves the reply. Authorization is forwarded and
// never written.
type gatherRecorder struct {
	dir, title            string
	storeBase, searchBase string
	http                  *http.Client
	saved                 *fixture

	upstream atomic.Int64

	mu   sync.Mutex
	errs []string
}

func newGatherRecorder(dir, title, storeBase, searchBase string) (*gatherRecorder, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	marker := filepath.Join(dir, "gather.json")
	if _, err := os.Stat(marker); err != nil {
		b, err := json.Marshal(map[string]string{"title": title, "source": "live"})
		if err != nil {
			return nil, err
		}
		if err := writeAtomic(marker, append(b, '\n')); err != nil {
			return nil, err
		}
	}
	return &gatherRecorder{
		dir: dir, title: title, storeBase: storeBase, searchBase: searchBase,
		http:  &http.Client{Timeout: 100 * time.Second},
		saved: &fixture{dir: dir, title: title},
	}, nil
}

func (g *gatherRecorder) errors() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string{}, g.errs...)
}

func (g *gatherRecorder) note(msg string) {
	g.mu.Lock()
	g.errs = append(g.errs, msg)
	g.mu.Unlock()
}

func (g *gatherRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		g.note("read request: " + err.Error())
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	name, kind, err := g.route(r, body)
	if err != nil {
		g.note(err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(filepath.Join(g.dir, name)); err == nil {
		r.Body = io.NopCloser(bytes.NewReader(body))
		g.saved.ServeHTTP(w, r)
		return
	}
	g.record(w, r, name, kind, body)
}

// route names the file a request saves to. kind is "store" or "search".
func (g *gatherRecorder) route(r *http.Request, body []byte) (name, kind string, err error) {
	switch {
	case r.URL.Path == "/search" && r.Method == http.MethodGet:
		return "appstore-search.json", "store", nil
	case feedPath.MatchString(r.URL.Path):
		n, _ := strconv.Atoi(feedPath.FindStringSubmatch(r.URL.Path)[1])
		return "reviews-page-" + pad(n) + ".json", "store", nil
	case r.URL.Path == "/search" && r.Method == http.MethodPost:
		var s struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(body, &s); err != nil {
			return "", "", fmt.Errorf("search body: %w", err)
		}
		name = searchFile(g.title, s.Query)
		if name == "" {
			return "", "", fmt.Errorf("search not in the plan: %s", s.Query)
		}
		return name, "search", nil
	case r.URL.Path == "/extract" && r.Method == http.MethodPost:
		var req struct {
			URLs []string `json:"urls"`
		}
		if err := json.Unmarshal(body, &req); err != nil || len(req.URLs) == 0 {
			return "", "", fmt.Errorf("extract body")
		}
		return extractRecordName(req.URLs), "search", nil
	default:
		return "", "", fmt.Errorf("unexpected %s %s", r.Method, r.URL.Path)
	}
}

func (g *gatherRecorder) record(w http.ResponseWriter, r *http.Request, name, kind string, body []byte) {
	auth := r.Header.Get("Authorization")
	upstream := g.storeBase
	target := upstream + r.URL.RequestURI()
	if kind == "search" {
		upstream = g.searchBase
		target = upstream + r.URL.Path
	}
	status, raw, err := g.forward(r, target, body, auth, kind)
	if err != nil {
		g.note(kind + " " + r.URL.Path + ": upstream failed")
		http.Error(w, "upstream failed", http.StatusBadGateway)
		return
	}
	raw = redact(raw, auth)
	save := status == http.StatusOK || (kind == "store" && status < 500)
	if save {
		if err := writeAtomic(filepath.Join(g.dir, name), raw); err != nil {
			g.note("save " + name + ": " + err.Error())
			http.Error(w, "save failed", http.StatusBadGateway)
			return
		}
		if status >= 400 {
			_ = writeAtomic(filepath.Join(g.dir, name+".status"), []byte(strconv.Itoa(status)+"\n"))
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

func (g *gatherRecorder) forward(r *http.Request, target string, body []byte, auth, kind string) (int, []byte, error) {
	budget := 20 * time.Second
	if kind == "search" {
		budget = 75 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), budget)
	defer cancel()
	var reader io.Reader
	if r.Method != http.MethodGet {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, target, reader)
	if err != nil {
		return 0, nil, err
	}
	if r.Method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	g.upstream.Add(1)
	resp, err := g.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, raw, nil
}

// replyDir is where model recordings for one title live. A gather set keeps
// them beside its capture, not in testdata/eval/replies/<slug>/.
func replyDir(root, slug, subdir string) string {
	if *gatherN > 0 {
		return filepath.Join(root, "testdata", "eval", "gathers", strconv.Itoa(*gatherN), slug, "replies")
	}
	dir := filepath.Join(root, "testdata", "eval", "replies")
	if subdir != "" {
		dir = filepath.Join(dir, subdir)
	}
	return filepath.Join(dir, slug)
}

// captureDir is the App Store and Tavily capture for one title.
func captureDir(root, slug string) string {
	if *gatherN > 0 {
		return filepath.Join(root, "testdata", "eval", "gathers", strconv.Itoa(*gatherN), slug)
	}
	return filepath.Join(root, "testdata", "titles", slug)
}

func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// redact drops an Authorization value out of a saved body. Host replies do
// not echo the key; this is the backstop so a leak cannot land in testdata.
func redact(body []byte, auth string) []byte {
	token := strings.TrimPrefix(auth, "Bearer ")
	token = strings.TrimSpace(token)
	if len(token) < 8 {
		return body
	}
	return bytes.ReplaceAll(body, []byte(token), []byte("redacted"))
}
