// Package web holds the embedded M1 client.
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
func Handler() http.Handler {
	sub, err := fs.Sub(assets, ".")
	if err != nil {
		panic(err) // embedded FS is fixed at compile time; this cannot fail at runtime
	}
	return http.FileServer(http.FS(sub))
}
