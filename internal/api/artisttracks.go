package api

import "net/http"

/*
 * artistTracks lists every track by an artist, in the order the artist page
 * shows them (store.TracksOf).
 *
 * Behind the artist page's track list and its Play all and Shuffle, which are
 * this list handed to the player in order or shuffled. A route of its own for
 * the reason /episodes is one: the tracks hang off albums rather than off the
 * artist, so `?parent_id=<artist>` returns the albums, and every client would
 * otherwise walk them — and get the loose single wrong, the track parented
 * straight to the artist, which has already cost one Play all that did nothing.
 *
 * Not capped or paged. A play queue is the whole discography or it is wrong,
 * and the largest artist in a real library is a few hundred rows.
 */
func (s *Server) artistTracks(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	// GetItem applies the caller's access rules, so an artist the caller may
	// not see is a 404 here exactly as it is on its own page.
	if _, err := s.st.GetItem(r.Context(), id, s.userID(r)); s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	tracks, err := s.st.TracksOf(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "artist tracks")
		return
	}
	// The played flag rides along, so the list can show what has been heard
	// without a request per row.
	if err := s.st.AttachProgress(r.Context(), tracks, s.userID(r)); err != nil {
		s.writeInternal(w, err, "artist tracks progress")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": tracks})
}
