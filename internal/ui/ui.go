// Package ui serves the embedded React UI bundle over HTTP, with an
// index.html fallback so client-side routes resolve cleanly when reloaded.
package ui

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/sirrobot01/mnemo/web"
)

// Handler returns an http.Handler that serves the embedded UI assets. Paths
// that don't map to a file in the bundle fall through to index.html so a
// future client-side router can take over.
func Handler() (http.Handler, error) {
	dist, err := web.DistFS()
	if err != nil {
		return nil, err
	}

	files := http.FileServer(http.FS(dist))
	indexBytes, indexErr := fs.ReadFile(dist, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			files.ServeHTTP(w, r)
			return
		}
		if info, err := fs.Stat(dist, path); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		if indexErr != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexBytes)
	}), nil
}
