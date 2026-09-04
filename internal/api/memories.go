package api

import (
	"net/http"
)

/*
 * GET /api/memories — photographs taken on today's date in an earlier year.
 *
 * Its own route rather than a parameter on /api/items, because the day is the
 * server's to decide. A `taken_on=MM-DD` filter would put a calendar date in
 * the client's hands, and a client computing one is a bug this project has met
 * before: `toISOString().slice(0,10)` is UTC, so a US evening resolves to
 * tomorrow, and the shelf would quietly show the wrong day for several hours
 * every night. Here there is one clock and it is the one the photographs were
 * filed under.
 *
 * Not admin-gated. Photographs a viewer may already browse, on a shelf they
 * already see.
 */
func (s *Server) memories(w http.ResponseWriter, r *http.Request) {
	items, on, err := s.st.PhotoMemories(r.Context(), queryInt(r, "limit"))
	if err != nil {
		s.writeInternal(w, err, "photo memories")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		// The date the server resolved, so a client that has been open since
		// yesterday can notice the shelf it is showing is not today's.
		"on": on,
	})
}
