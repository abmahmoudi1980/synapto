package adminapi

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// spaFS holds the built Svelte SPA. The directive is satisfied at compile
// time when frontend/build/ has been copied into this package's directory
// by the Makefile (see `make build`). When the directory is empty in dev,
// the embedded FS is empty and the fallback handler is used instead.
//
//go:embed all:spa
var spaFS embed.FS

// spaHandler serves the SPA's index.html for any non-/api route, with
// correct caching for hashed assets under _app/.
func spaHandler() http.Handler {
	sub, err := fs.Sub(spaFS, "spa")
	if err != nil {
		// The embed dir is missing or empty (dev mode without a built SPA).
		return http.HandlerFunc(devFallbackHandler)
	}
	// Detect the "only .gitkeep" placeholder case (dev build without SPA).
	if _, statErr := fs.Stat(sub, "index.html"); statErr != nil {
		return http.HandlerFunc(devFallbackHandler)
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		// Serve the request file if it exists; otherwise fall back to
		// index.html so the SPA's client-side router can handle the path.
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(sub, path); err != nil {
			r.URL.Path = "/"
		}
		// Aggressive cache for hashed assets; no-cache for index.html.
		if strings.HasPrefix(path, "_app/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else if path == "index.html" || path == "" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

// devFallbackHandler returns a friendly "UI not built" page so that a
// developer who runs `go run ./cmd/assistant` without first building the
// SPA gets a clear explanation instead of a silent 404.
func devFallbackHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`<!DOCTYPE html>
<html><head><title>Synapto Admin — UI not built</title></head>
<body style="font-family: system-ui, sans-serif; max-width: 40em; margin: 2em auto; padding: 0 1em">
<h1>UI not built</h1>
<p>The admin panel's static assets were not embedded into this binary.</p>
<p>Build the SPA first:</p>
<pre style="background:#f6f7f9; padding:1em; border-radius:4px">cd frontend
npm install
npm run build
# then copy or symlink frontend/build into backend/internal/adminapi/spa
# and rebuild the Go binary, OR run via the Makefile target:
make build</pre>
<p>The JSON API at <code>/api/*</code> is still available.</p>
</body></html>`))
}
