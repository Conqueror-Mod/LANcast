package api

import (
	"net/http"

	"lancast"
)

// repository is where the source lives. A link the page shows, never a
// request this server makes.
const repository = "https://github.com/Conqueror-Mod/LANcast"

/*
 * about is Settings › About: what this build is, under what licence, built
 * from what, and what changed in it.
 *
 * Everything in the answer was embedded when the binary was built (the root
 * package's about.go), so the pane describes the running version without
 * asking GitHub or anyone else — no phone-home. Any signed-in account: none
 * of it is administrative, and the licence is everybody's business.
 */
func (s *Server) about(w http.ResponseWriter, r *http.Request) {
	notes, ok := lancast.ReleaseNotes(Version)
	out := map[string]any{
		"version":    Version,
		"repository": repository,
		"licence":    "AGPL-3.0-only",
		"notice":     lancast.Notice,
	}
	if ok {
		out["notes"] = notes
	}
	writeJSON(w, http.StatusOK, out)
}
