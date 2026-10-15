package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/nrynss/before-yes/internal/tavily"
)

// fixture serves one title's captures under testdata/titles as the iTunes
// and Tavily hosts, so the real clients parse the recorded bytes and no
// call leaves the machine. A search is matched to its file by the plan
// query that sent it, and its hits are narrowed by include_domains the way
// Tavily narrows them. An extract answers from tavily-extract.json, and a
// URL the capture did not read fails, as it would offline.
type fixture struct {
	dir   string
	title string

	mu       sync.Mutex
	searches int
	extracts int
	errs     []string
}

func serveFixture(dir, title string) (*fixture, *httptest.Server) {
	f := &fixture{dir: dir, title: title}
	return f, httptest.NewServer(f)
}

var feedPath = regexp.MustCompile(`^/[a-z]{2}/rss/customerreviews/page=(\d+)/id=\d+/sortby=mostrecent/json$`)

func (f *fixture) fail(w http.ResponseWriter, msg string) {
	f.mu.Lock()
	f.errs = append(f.errs, msg)
	f.mu.Unlock()
	http.Error(w, msg, http.StatusBadRequest)
}

func (f *fixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/search" && r.Method == http.MethodGet:
		f.file(w, "appstore-search.json")
	case feedPath.MatchString(r.URL.Path):
		n, _ := strconv.Atoi(feedPath.FindStringSubmatch(r.URL.Path)[1])
		name := "reviews-page-" + pad(n) + ".json"
		if _, err := os.Stat(filepath.Join(f.dir, name)); err != nil {
			_, _ = io.WriteString(w, `{"feed":{}}`)
			return
		}
		f.file(w, name)
	case r.URL.Path == "/search":
		f.search(w, r)
	case r.URL.Path == "/extract":
		f.extract(w, r)
	default:
		f.fail(w, "unexpected "+r.Method+" "+r.URL.Path)
	}
}

func pad(n int) string {
	s := strconv.Itoa(n)
	if len(s) < 2 {
		s = "0" + s
	}
	return s
}

func (f *fixture) file(w http.ResponseWriter, name string) {
	b, err := os.ReadFile(filepath.Join(f.dir, name))
	if err != nil {
		f.fail(w, err.Error())
		return
	}
	if sb, err := os.ReadFile(filepath.Join(f.dir, name+".status")); err == nil {
		code, _ := strconv.Atoi(strings.TrimSpace(string(sb)))
		if code >= 400 {
			w.WriteHeader(code)
			_, _ = w.Write(b)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

// liveCapture reports whether dir is a saved live gather (gather.json),
// whose search and extract bodies are the hosts' own replies.
func (f *fixture) liveCapture() bool {
	_, err := os.Stat(filepath.Join(f.dir, "gather.json"))
	return err == nil
}

func onDomain(raw, dom string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == dom || strings.HasSuffix(h, "."+dom)
}

func (f *fixture) search(w http.ResponseWriter, r *http.Request) {
	var s tavily.Search
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		f.fail(w, "search body: "+err.Error())
		return
	}
	f.mu.Lock()
	f.searches++
	f.mu.Unlock()
	name := searchFile(f.title, s.Query)
	if name == "" {
		f.fail(w, "search not in the plan: "+s.Query)
		return
	}
	b, err := os.ReadFile(filepath.Join(f.dir, name))
	if err != nil {
		f.fail(w, name+": "+err.Error())
		return
	}
	// A live gather stores Tavily's own reply, already narrowed the way the
	// request asked. Serving it unchanged is what the live call returned.
	// The frozen fixtures are wider than any one request, so they are
	// narrowed here, as Tavily would have.
	if f.liveCapture() {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
		return
	}
	var body map[string]any
	if err = json.Unmarshal(b, &body); err != nil {
		f.fail(w, name+": "+err.Error())
		return
	}
	if len(s.IncludeDomains) > 0 {
		keep := []any{}
		hits, _ := body["results"].([]any)
		for _, h := range hits {
			u, _ := h.(map[string]any)["url"].(string)
			for _, d := range s.IncludeDomains {
				if onDomain(u, d) {
					keep = append(keep, h)
					break
				}
			}
		}
		body["results"] = keep
	}
	_ = json.NewEncoder(w).Encode(body)
}

func (f *fixture) extract(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URLs []string `json:"urls"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	f.extracts++
	f.mu.Unlock()
	if f.liveCapture() {
		name := extractRecordName(req.URLs)
		b, err := os.ReadFile(filepath.Join(f.dir, name))
		if err != nil {
			f.fail(w, "extract not recorded: "+name)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
		return
	}
	var rec struct {
		Results []map[string]any `json:"results"`
		Usage   map[string]any   `json:"usage"`
	}
	b, err := os.ReadFile(filepath.Join(f.dir, "tavily-extract.json"))
	if err == nil {
		err = json.Unmarshal(b, &rec)
	}
	if err != nil {
		f.fail(w, "extract: "+err.Error())
		return
	}
	results, failed := []map[string]any{}, []map[string]any{}
	for _, u := range req.URLs {
		hit := false
		for _, p := range rec.Results {
			if p["url"] == u {
				results = append(results, p)
				hit = true
				break
			}
		}
		if !hit {
			failed = append(failed, map[string]any{"url": u, "error": "not in the capture"})
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"results": results, "failed_results": failed, "usage": rec.Usage})
}
