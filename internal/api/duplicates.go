package api

import (
	"net/http"

	"lancast/internal/store"
)

/*
 * photoDuplicates lists a picture library's groups of identical photos
 * (ADR 0075).
 *
 * The same reach as the timeline: anyone who can browse the library can see
 * which of its photos are the same file, because each copy is already on the
 * grid in front of them. Removing a copy is DELETE /api/items/{id} and keeps
 * that route's rules — admin only, and mode=delete refused when media deletion
 * is off — so this endpoint changes nothing and grants nothing.
 */
func (s *Server) photoDuplicates(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return
	}
	lib, err := s.st.GetLibrary(r.Context(), id)
	if s.notFoundOr(w, err, "get library", "no such library") {
		return
	}
	if lib.Kind != "picture" {
		writeError(w, http.StatusBadRequest, "wrong_kind",
			"duplicates are a picture-library view")
		return
	}
	groups, err := s.st.PhotoDuplicates(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "photo duplicates")
		return
	}
	/*
	 * Thumbnails come with the response, as they do on the grid. One call for
	 * every copy rather than one per group: AttachArtwork works on a slice of
	 * values, so the copies are flattened into one and written back.
	 */
	var all []store.Item
	for _, g := range groups {
		for _, c := range g.Copies {
			all = append(all, c.Item)
		}
	}
	if err := s.st.AttachArtwork(r.Context(), all); err != nil {
		s.writeInternal(w, err, "attach artwork")
		return
	}
	extra, k := 0, 0
	for gi := range groups {
		extra += len(groups[gi].Copies) - 1
		for ci := range groups[gi].Copies {
			groups[gi].Copies[ci].Item = all[k]
			k++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups, "extra_copies": extra})
}

/*
 * photoNearCopies lists a picture library's near copies: the same picture
 * resized or re-saved (ADR 0075, 2026-10-06 amendment).
 *
 * The same reach and the same powers as photoDuplicates: a list, nothing else.
 * Kept a separate endpoint rather than a section of that response, because a
 * duplicate is a fact (the same bytes) and this is a judgement that was
 * measured, and a client should never be able to mistake one for the other.
 */
func (s *Server) photoNearCopies(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return
	}
	lib, err := s.st.GetLibrary(r.Context(), id)
	if s.notFoundOr(w, err, "get library", "no such library") {
		return
	}
	if lib.Kind != "picture" {
		writeError(w, http.StatusBadRequest, "wrong_kind",
			"near copies are a picture-library view")
		return
	}
	near, err := s.st.PhotoNearCopies(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "photo near copies")
		return
	}
	var all []store.Item
	for _, g := range near.Groups {
		for _, c := range g.Copies {
			all = append(all, c.Item)
		}
	}
	if err := s.st.AttachArtwork(r.Context(), all); err != nil {
		s.writeInternal(w, err, "attach artwork")
		return
	}
	k := 0
	for gi := range near.Groups {
		for ci := range near.Groups[gi].Copies {
			near.Groups[gi].Copies[ci].Item = all[k]
			k++
		}
	}
	writeJSON(w, http.StatusOK, near)
}
