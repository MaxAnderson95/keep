package serve

import (
	"io/fs"
	"net/http"
	"strings"
)

// staticHandler serves the embedded SPA: real files as-is (hashed /assets
// immutable), everything else falling back to index.html for client-side
// routing. When the frontend was not built into this binary, it answers with
// a plain explanation instead — the API remains fully usable.
func (s *Server) staticHandler() http.Handler {
	if s.opts.StaticFS == nil {
		return http.HandlerFunc(notBuilt)
	}
	if _, err := fs.Stat(s.opts.StaticFS, "index.html"); err != nil {
		return http.HandlerFunc(notBuilt)
	}
	fileServer := http.FileServerFS(s.opts.StaticFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(s.opts.StaticFS, path); err == nil {
				if strings.HasPrefix(path, "assets/") {
					// Vite emits content-hashed filenames under assets/.
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// SPA fallback: serve the app shell and let the router take over.
		w.Header().Set("Cache-Control", "no-cache")
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

func notBuilt(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "keep serve: the web UI was not built into this binary (build web/ first); the JSON API at /api/v1 is available", http.StatusNotImplemented)
}
