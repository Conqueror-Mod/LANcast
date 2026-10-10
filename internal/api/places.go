package api

import (
	"net/http"
	"strconv"
)

/*
 * Photo places (ADR 0078) — a picture library grouped by the town each
 * photograph was taken in.
 *
 * Shaped like the timeline: one small response with every place and its
 * count, then one place's photographs when it is opened. What crosses the
 * wire is a name and a count. No coordinate is ever sent, because nothing in
 * the client needs one without a map.
 *
 * Readable by anyone who can see the library, as the timeline is. Turning the
 * reading on or off is the `photo_places` setting, which is an administrator's.
 */

// placesLibrary resolves the library and refuses anything but a picture
// library, the way the timeline does: places of a film library would be a
// question about filming locations, which this is not.
func (s *Server) placesLibrary(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return 0, false
	}
	lib, err := s.st.GetLibrary(r.Context(), id)
	if s.notFoundOr(w, err, "get library", "no such library") {
		return 0, false
	}
	if lib.Kind != "picture" {
		writeError(w, http.StatusBadRequest, "wrong_kind", "places are a picture-library view")
		return 0, false
	}
	return id, true
}

func (s *Server) photoPlaces(w http.ResponseWriter, r *http.Request) {
	id, ok := s.placesLibrary(w, r)
	if !ok {
		return
	}
	sum, err := s.st.PhotoPlaces(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "photo places")
		return
	}
	/*
	 * `enabled` travels with the list. An empty list means "nothing here
	 * carries a position", "nothing has been read yet" or "reading is off",
	 * and a screen that cannot tell those apart offers the wrong next step.
	 * `reading` says a pass is running now, so the client knows to look again.
	 */
	reading := false
	if s.locations != nil {
		reading = s.locations.Stats().Running
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":   s.settings.Get().PhotoPlaces,
		"reading":   reading,
		"places":    sum.Places,
		"elsewhere": sum.Elsewhere,
		"unlocated": sum.Unlocated,
		"unread":    sum.Unread,
	})
}

// placePhotos lists one place's photographs, newest first. `{place}` is a
// place id from the list, or `elsewhere` for photographs with a position and
// no town near it.
func (s *Server) placePhotos(w http.ResponseWriter, r *http.Request) {
	id, ok := s.placesLibrary(w, r)
	if !ok {
		return
	}
	var placeID int64
	if raw := r.PathValue("place"); raw != "elsewhere" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "place must be a place id or \"elsewhere\"")
			return
		}
		placeID = n
	}
	items, total, err := s.st.PlacePhotos(r.Context(), id, placeID, queryInt(r, "limit"), queryInt(r, "offset"))
	if err != nil {
		s.writeInternal(w, err, "place photos")
		return
	}
	// Through the same funnel as every other listing, so a tile gets its
	// poster and the ceiling is applied where every list becomes a response.
	s.decorateAndWriteItems(w, r, items, total)
}
