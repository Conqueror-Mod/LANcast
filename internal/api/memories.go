package api

import (
	"net/http"

	"lancast/internal/store"
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
 *
 * `watched` is the other half (ADR 0119 §3a): films, episodes, audiobooks and
 * comics the caller finished on today's date in an earlier year, from their
 * own history, with the same ceiling and hidden libraries applied.
 */
func (s *Server) memories(w http.ResponseWriter, r *http.Request) {
	items, on, err := s.st.PhotoMemories(r.Context(), queryInt(r, "limit"))
	if err != nil {
		s.writeInternal(w, err, "photo memories")
		return
	}
	// Photographs from a library kept from the caller are not their memories
	// (ADR 0084). The ceiling exempts photographs; the hidden libraries do not.
	items, err = s.st.PermittedItems(r.Context(), s.userID(r), items)
	if err != nil {
		s.writeInternal(w, err, "apply account limits")
		return
	}
	// The photographs' own thumbnails. Without them every tile fell back to
	// the filename: the shelf did, and the On this day panel made it plain.
	if err := s.st.AttachArtwork(r.Context(), items); err != nil {
		s.writeInternal(w, err, "photo memories artwork")
		return
	}
	watched, err := s.watchedOnThisDay(r)
	if err != nil {
		s.writeInternal(w, err, "watched on this day")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":   items,
		"watched": watched,
		// The date the server resolved, so a client that has been open since
		// yesterday can notice the shelf it is showing is not today's.
		"on":      on,
	})
}

// watchedOnThisDay is the caller's history half of On this day, filtered as
// any other listing is and given its artwork.
func (s *Server) watchedOnThisDay(r *http.Request) ([]store.FinishedMemory, error) {
	user := s.userID(r)
	found, err := s.st.FinishedOnThisDay(r.Context(), user, queryInt(r, "limit"))
	if err != nil || len(found) == 0 {
		return found, err
	}
	items := make([]store.Item, len(found))
	for i, m := range found {
		items[i] = m.Item
	}
	items, err = s.st.PermittedItems(r.Context(), user, items)
	if err != nil {
		return nil, err
	}
	if err := s.st.AttachArtwork(r.Context(), items); err != nil {
		return nil, err
	}
	kept := make(map[int64]store.Item, len(items))
	for _, it := range items {
		kept[it.ID] = it
	}
	out := []store.FinishedMemory{}
	for _, m := range found {
		if it, ok := kept[m.Item.ID]; ok {
			out = append(out, store.FinishedMemory{Item: it, FinishedAt: m.FinishedAt})
		}
	}
	return out, nil
}
