package api

import (
	"errors"
	"net/http"

	"lancast/internal/store"
)

/*
 * The watchlist (ADR 0085): titles the caller means to watch.
 *
 * The caller's own, always — every route acts on the session, like tags and
 * favourites, and none is audited, for the same reason: which films somebody
 * means to get round to is not something anybody looks for in an audit trail.
 */

// watchlist lists the caller's, newest first, decorated like any listing, so
// it takes the account's limits (ADR 0084) and carries progress and artwork.
func (s *Server) watchlist(w http.ResponseWriter, r *http.Request) {
	items, err := s.st.Watchlist(r.Context(), s.userID(r), queryInt(r, "limit"))
	if err != nil {
		s.writeInternal(w, err, "watchlist")
		return
	}
	s.decorateAndWriteItems(w, r, items)
}

// addToWatchlist puts a title on. Through GetItem first, so a title the caller
// cannot see cannot be put on their list — the refusal looks like absence.
func (s *Server) addToWatchlist(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	if _, err := s.st.GetItem(r.Context(), id, s.userID(r)); s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	if err := s.st.AddToWatchlist(r.Context(), s.userID(r), id); err != nil {
		if errors.Is(err, store.ErrNotWatchlistable) {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		s.writeInternal(w, err, "add to watchlist")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// removeFromWatchlist takes a title off. Not gated by GetItem: taking
// something off your own list must work even for a title you can no longer
// see.
func (s *Server) removeFromWatchlist(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	if err := s.st.RemoveFromWatchlist(r.Context(), s.userID(r), id); err != nil {
		s.writeInternal(w, err, "remove from watchlist")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
