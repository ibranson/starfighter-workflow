// Package web embeds the built SvelteKit SPA bundle and serves it.
//
// During development, before `make web` has been run, the dist/ directory
// contains only a placeholder index.html so the daemon still serves something
// coherent. After `make web`, dist/ contains the real bundle.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// Handler returns an http.Handler that serves the SPA. Unknown paths fall
// through to index.html so SvelteKit's client-side router can take over.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// Should be impossible — embed guarantees the dir exists.
		panic(err)
	}
	files := http.FS(sub)
	fileSrv := http.FileServer(files)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			fileSrv.ServeHTTP(w, r)
			return
		}
		if f, err := files.Open(path); err == nil {
			_ = f.Close()
			fileSrv.ServeHTTP(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileSrv.ServeHTTP(w, r2)
	})
}
