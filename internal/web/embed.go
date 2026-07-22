// Package web holds the embedded M1/M2 client.
//
// This is deliberately a single file with no build step. It will be replaced by
// the React client at M3 — but the tokens and the gold discipline rule it
// establishes carry over. See docs/design.md.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html
var assets embed.FS

// Handler serves the embedded client.
//
// Files in an embed.FS have a zero modification time, so http.FileServer sends
// neither Last-Modified nor ETag. Browsers then cache the page heuristically,
// and a rebuilt binary keeps serving the old UI until someone thinks to hard
// refresh — which looks exactly like the new code not working.
//
// The client is a few KB served over a LAN; revalidating every load costs
// nothing and removes a whole category of phantom bug reports.
func Handler() http.Handler {
	sub, err := fs.Sub(assets, ".")
	if err != nil {
		panic(err) // embedded FS is fixed at compile time; this cannot fail at runtime
	}
	files := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		files.ServeHTTP(w, r)
	})
}
