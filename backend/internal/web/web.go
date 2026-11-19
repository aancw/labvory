// Package web serves the built web UI from the server binary.
//
// The SPA lives in frontend/ and is built to frontend/dist by `npm run build`.
// Release builds (`go build -tags embed`, also used by `make release` and the
// release workflow) embed that dist directory via embed.go; plain builds —
// `go build ./...`, `go test ./...` — carry no assets, so Handler serves a
// fallback page pointing at the release/docker builds instead.
package web

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// embeddedFS is populated by the embed build (embed.go, //go:build embed);
// plain builds leave it nil and Handler falls back to the placeholder page.
var embeddedFS fs.FS

// Handler returns the SPA handler: static files with immutable caching for
// hashed assets and an index.html fallback for client-side routes. Cache
// headers set here intentionally override the no-store applied globally by
// httpx.SecurityHeaders — index.html keeps no-store so deploys take effect.
func Handler() http.Handler {
	if embeddedFS == nil {
		return http.HandlerFunc(fallbackPage)
	}
	return spaHandler(embeddedFS)
}

func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Rooted clean path: cannot escape dist regardless of the raw URL.
		name := path.Clean("/" + r.URL.Path)[1:]
		info, err := fs.Stat(fsys, name)
		if err != nil || info.IsDir() {
			// Not a real file: hand the path to the SPA router.
			serveIndex(fsys, w, r)
			return
		}
		if strings.HasPrefix(name, "assets/") {
			// Vite content-hashed bundles never change once written.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	})
}

// serveIndex writes the SPA entrypoint directly: http.FileServer redirects any
// path ending in /index.html back to ./, which would turn the fallback for
// client-side routes into a redirect loop.
func serveIndex(fsys fs.FS, w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		http.Error(w, "web UI assets missing from this build", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// fallbackPage is served when the binary was built without `-tags embed`.
func fallbackPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(fallbackHTML))
}

const fallbackHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>LabVory</title>
<style>body{font-family:system-ui,sans-serif;max-width:36rem;margin:8rem auto;padding:0 1rem;color:#374151;line-height:1.6}code{background:#f3f4f6;padding:.15rem .35rem;border-radius:.25rem}</style>
</head>
<body>
<h1>LabVory API is running</h1>
<p>This binary was built without the web UI embedded. Options:</p>
<ul>
<li>Run the frontend separately: <code>make dev</code> (Vite on :5173)</li>
<li>Build the standalone binary: <code>make release</code></li>
<li>Use the published release or Docker image — those embed the UI</li>
</ul>
<p>The API itself is available under <code>/api/v1</code>.</p>
</body>
</html>
`
