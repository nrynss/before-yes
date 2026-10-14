package server

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// App is the whole HTTP surface: the API (normally Server.GatedHandler),
// the status routes, and the built Chaaya app. Any /api/ path goes to api;
// any other path is a file of web, and an unknown one gets index.html so
// a client route such as /card/{id} loads in a fresh tab. A nil web
// serves no app: non-API paths are 404.
func App(api http.Handler, st *Status, web fs.FS) http.Handler {
	if st == nil {
		st = &Status{}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", st.status)
	mux.HandleFunc("GET /api/checklist", st.checklist)
	mux.HandleFunc("GET /healthz", healthz)
	mux.Handle("/api/", api)
	if web != nil {
		mux.Handle("/", spa(web))
	}
	return mux
}

// spa serves web, falling back to index.html for a path that is not a
// file. Only GET and HEAD are answered.
func spa(web fs.FS) http.Handler {
	files := http.FileServerFS(web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if p != "" {
			if fi, err := fs.Stat(web, p); err != nil || fi.IsDir() {
				w.Header().Set("Cache-Control", "no-cache")
				http.ServeFileFS(w, r, web, "index.html")
				return
			}
		}
		files.ServeHTTP(w, r)
	})
}
