package api

import (
	"net/http"

	"lancast/internal/store"
)

/*
 * itemAfterwards is what the end of a film offers: its collection's next film,
 * then films like it from outside its franchise (store/afterwards.go).
 *
 * One request for the page a film ends on, so the page draws once rather than
 * filling in. The next film carries continues_from — the same marker the
 * Continue Watching shelf uses for the same fact — so the client tells the two
 * kinds apart without a second shape.
 *
 * no-store: the answer reads watch state, and the page asks at the moment that
 * state is changing.
 */
func (s *Server) itemAfterwards(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	user := s.userID(r)
	film, err := s.st.GetItem(r.Context(), id, user)
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	items := []store.Item{}
	if film.Kind != "movie" {
		s.decorateAndWriteItems(w, r, items)
		return
	}
	next, err := s.st.NextAfterInCollection(r.Context(), id, user)
	if err != nil {
		s.writeInternal(w, err, "next in collection")
		return
	}
	if next != nil {
		items = append(items, *next)
	}
	similar, err := s.st.SimilarFilms(r.Context(), id, user, 3)
	if err != nil {
		s.writeInternal(w, err, "similar films")
		return
	}
	items = append(items, similar...)
	s.decorateAndWriteItems(w, r, items)
}
