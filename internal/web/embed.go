// Package web holds the embedded React client.
//
// The client is built by Vite (see /web) into dist/, which is embedded here and
// served by lancastd. See docs/design.md for the design system it executes.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var assets embed.FS

// Handler serves the embedded client with single-page-app routing.
//
// A real asset is served directly; anything else is a client-side route
// (/library/1, /item/5, …) and gets the app shell so the router can resolve it.
// Without this fallback a deep link or a reload on any non-root path would 404.
//
// Cache policy splits by kind: Vite fingerprints files under assets/, so those
// are immutable and cached for a year, while index.html must revalidate every
// load — otherwise a rebuilt binary keeps serving the old shell until someone
// thinks to hard refresh, which looks exactly like the new code not working.
func Handler() http.Handler {
	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err) // embedded FS is fixed at compile time; this cannot fail at runtime
	}
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		panic(err) // the Vite build output must be present when the binary is built
	}
	files := http.FileServer(http.FS(dist))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if upath == "" {
			upath = "index.html"
		}

		if f, err := dist.Open(upath); err == nil {
			st, _ := f.Stat()
			f.Close()
			if st != nil && !st.IsDir() {
				if strings.HasPrefix(upath, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache, must-revalidate")
				}
				files.ServeHTTP(w, r)
				return
			}
		}

		/*
		 * A missing build file is missing, not a route.
		 *
		 * assets/ holds only fingerprinted build output, and a name that is
		 * not there is one from an older build: a page loaded before an
		 * update, or a phone's home-screen app restored from memory after
		 * one, asking for the code it was built with. Handing it the shell
		 * instead answered a script request with HTML, which a browser
		 * refuses to run, and the page sat blank with nothing in any log
		 * (reported from a phone after a server update). A 404 lets the
		 * page notice and reload itself (index.html).
		 */
		if strings.HasPrefix(upath, "assets/") {
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return
		}

		// Unknown path: hand the client its shell and let the router decide.
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}
