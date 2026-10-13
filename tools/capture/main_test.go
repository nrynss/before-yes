package main

import (
	"encoding/json"
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
	"time"
)

const fakeKey = "fake-key-7f3a9c"

func TestOfficialHost(t *testing.T) {
	tests := []struct {
		name  string
		apps  []app
		title string
		want  string
		hit   int64 // trackId of the expected hit; 0 for none
	}{
		{
			name:  "strips www and lowercases",
			apps:  []app{{ID: 1, Name: "Roblox", SellerURL: "https://www.Roblox.com/support"}},
			title: "Roblox",
			want:  "roblox.com",
			hit:   1,
		},
		{
			name:  "match is case-insensitive",
			apps:  []app{{ID: 2, Name: "ROBLOX", SellerURL: "https://roblox.com/"}},
			title: "roblox",
			want:  "roblox.com",
			hit:   2,
		},
		{
			name:  "keeps a subdomain other than www",
			apps:  []app{{ID: 3, Name: "Minecraft", SellerURL: "https://minecraft.net/en-us"}},
			title: "Minecraft",
			want:  "minecraft.net",
			hit:   3,
		},
		{
			name:  "no hit whose name starts with the title gives no host",
			apps:  []app{{ID: 4, Name: "Toca Boca World: Game & Play", SellerURL: "https://tocaboca.com/"}},
			title: "Toca Life World",
			want:  "",
			hit:   0,
		},
		{
			name: "takes the first matching hit, not the first hit",
			apps: []app{
				{ID: 5, Name: "Roblox Mobile Helper", SellerURL: "https://fan.example/"},
				{ID: 6, Name: "Roblox", SellerURL: "https://www.roblox.com/"},
			},
			title: "Roblox",
			want:  "fan.example",
			hit:   5,
		},
		{
			name:  "a matching hit with no sellerUrl gives no host",
			apps:  []app{{ID: 7, Name: "Roblox", SellerURL: ""}},
			title: "Roblox",
			want:  "",
			hit:   7,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, picked := officialHost(tc.apps, tc.title)
			if got != tc.want {
				t.Errorf("host = %q, want %q", got, tc.want)
			}
			switch {
			case tc.hit == 0 && picked != nil:
				t.Errorf("picked %q, want no hit", picked.Name)
			case tc.hit != 0 && (picked == nil || picked.ID != tc.hit):
				t.Errorf("picked %+v, want trackId %d", picked, tc.hit)
			}
		})
	}
}

// tavilyCall is one request the fake Tavily server received.
type tavilyCall struct {
	path     string
	rawQuery string
	auth     string
	body     map[string]any
}

// fakeTavily answers searches and extracts. Each call reports credits. Policy
// and terms return their own URLs so the extract target is checkable.
type fakeTavily struct {
	mu      sync.Mutex
	calls   []tavilyCall
	credits float64
}

func (f *fakeTavily) handler(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	f.mu.Lock()
	f.calls = append(f.calls, tavilyCall{path: r.URL.Path, rawQuery: r.URL.RawQuery, auth: r.Header.Get("Authorization"), body: body})
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/extract" {
		var results []string
		urls, _ := body["urls"].([]any)
		for _, u := range urls {
			results = append(results, fmt.Sprintf(`{"url":%q,"raw_content":"policy text"}`, u))
		}
		fmt.Fprintf(w, `{"results":[%s],"usage":{"credits":%v}}`, strings.Join(results, ","), f.credits)
		return
	}
	q, _ := body["query"].(string)
	u := "https://example.org/other"
	switch {
	case strings.Contains(q, "privacy policy"):
		u = "https://www.roblox.com/privacy"
	case strings.Contains(q, "terms of service"):
		u = "https://www.roblox.com/terms"
	}
	fmt.Fprintf(w, `{"results":[{"title":"t","url":%q,"content":"c","raw_content":"r"}],"usage":{"credits":%v}}`, u, f.credits)
}

func fakeItunes(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("term") != "Roblox" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"resultCount":1,"results":[{"trackId":431946152,"trackName":"Roblox","sellerUrl":"https://www.roblox.com/","trackViewUrl":"https://apps.apple.com/us/app/roblox/id431946152"}]}`)
	})
	mux.HandleFunc("/us/rss/customerreviews/page=1/id=431946152/sortby=mostrecent/json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"feed":{"entry":[
			{"id":{"label":"app"},"title":{"label":"Roblox"}},
			{"id":{"label":"312"},"title":{"label":"Filter failed"},"content":{"label":"The filter did not catch it when someone asked my 7 year old for her number"},"im:rating":{"label":"1"}}
		]}}`)
	})
	mux.HandleFunc("/us/rss/customerreviews/page=2/id=431946152/sortby=mostrecent/json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"feed":{"title":"empty"}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newCapturer(t *testing.T, itunesURL string, f *fakeTavily) *capturer {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	return &capturer{
		itunes: itunesURL,
		tavily: srv.URL,
		key:    fakeKey,
		client: &http.Client{Timeout: 10 * time.Second},
		pause:  0,
		now:    time.Now,
	}
}

// roblox is the title the request-body test runs against.
var roblox = demo{Slug: "roblox", Title: "Roblox", ID: 431946152}

func TestRequestBodiesMatchPlan(t *testing.T) {
	itunes := fakeItunes(t)
	f := &fakeTavily{credits: 1}
	c := newCapturer(t, itunes.URL, f)
	out := t.TempDir()
	if err := c.capture(roblox, out); err != nil {
		t.Fatalf("capture: %v", err)
	}

	// Expected bodies, written out from gather.go's plan() and the extract call.
	want := []string{
		`{"query":"Roblox privacy policy","max_results":5,"include_domains":["roblox.com"],"include_usage":true}`,
		`{"query":"Roblox terms of service","max_results":5,"include_domains":["roblox.com"],"include_usage":true}`,
		`{"query":"Roblox parents experience kids chat strangers purchases","max_results":8,"include_domains":["reddit.com","commonsensemedia.org","mumsnet.com","netmums.com"],"include_raw_content":true,"include_usage":true}`,
		`{"query":"Roblox children safety fine lawsuit FTC regulator data breach","topic":"news","max_results":8,"include_raw_content":true,"include_usage":true}`,
		`{"query":"Roblox age rating ESRB PEGI parental controls","max_results":6,"include_raw_content":true,"include_usage":true}`,
		`{"urls":["https://www.roblox.com/privacy","https://www.roblox.com/terms"],"extract_depth":"basic","format":"text","include_usage":true}`,
	}
	if len(f.calls) != len(want) {
		t.Fatalf("got %d Tavily calls, want %d", len(f.calls), len(want))
	}
	paths := []string{"/search", "/search", "/search", "/search", "/search", "/extract"}
	for i, call := range f.calls {
		var wantBody map[string]any
		if err := json.Unmarshal([]byte(want[i]), &wantBody); err != nil {
			t.Fatal(err)
		}
		if call.path != paths[i] {
			t.Errorf("call %d path = %s, want %s", i, call.path, paths[i])
		}
		if !reflect.DeepEqual(call.body, wantBody) {
			t.Errorf("call %d body = %v, want %v", i, call.body, wantBody)
		}
		if call.auth != "Bearer "+fakeKey {
			t.Errorf("call %d Authorization header not the bearer key", i)
		}
		if strings.Contains(call.rawQuery, fakeKey) || strings.Contains(call.path, fakeKey) {
			t.Errorf("call %d carries the key outside the header", i)
		}
	}

	// Review feed stops at the first page with no entries, and that page is not saved.
	if _, err := os.Stat(filepath.Join(out, "reviews-page-01.json")); err != nil {
		t.Errorf("page 1 not saved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "reviews-page-02.json")); !os.IsNotExist(err) {
		t.Errorf("page 2 should not be saved, stat err = %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(out, "MANIFEST.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"Official host used for policy and terms: roblox.com",
		"Reviews captured: 1",
		"Tavily credits for this title: 6",
		"reviews stopped at page 2: no feed.entry",
	} {
		if !strings.Contains(string(manifest), line) {
			t.Errorf("manifest missing %q", line)
		}
	}
}

func TestKeyNeverWritten(t *testing.T) {
	itunes := fakeItunes(t)
	f := &fakeTavily{credits: 1}
	c := newCapturer(t, itunes.URL, f)
	out := t.TempDir()
	if err := c.capture(roblox, out); err != nil {
		t.Fatalf("capture: %v", err)
	}

	files := 0
	err := filepath.Walk(out, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		files++
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), fakeKey) {
			t.Errorf("%s contains the key", filepath.Base(path))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 8 {
		t.Fatalf("only %d files written; the check would be vacuous", files)
	}
}

func TestCreditGuardStopsTheTitle(t *testing.T) {
	itunes := fakeItunes(t)
	f := &fakeTavily{credits: 4}
	c := newCapturer(t, itunes.URL, f)
	err := c.capture(roblox, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "over the guard") {
		t.Fatalf("err = %v, want the credit guard", err)
	}
	if len(f.calls) != 3 {
		t.Errorf("made %d Tavily calls before stopping, want 3 (12 credits passes the guard of 10)", len(f.calls))
	}
}
