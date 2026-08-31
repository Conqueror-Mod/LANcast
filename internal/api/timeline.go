package api

import "net/http"

/*
 * The photo timeline (see store.PhotoTimeline).
 *
 * Counts by capture month, newest first, so a client learns a picture
 * library's whole shape in one small response and then fetches a month at a
 * time through the ordinary item listing with `taken_month=`.
 *
 * Not admin-only: it discloses nothing a member cannot already see by browsing
 * the library, and browsing is the point.
 */
func (s *Server) photoTimeline(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return
	}
	lib, err := s.st.GetLibrary(r.Context(), id)
	if s.notFoundOr(w, err, "get library", "no such library") {
		return
	}
	/*
	 * A timeline of a film library would be a list of release months, which is
	 * not what this is for and would quietly answer a different question. The
	 * refusal names the kind so the client can say why rather than showing an
	 * empty page.
	 */
	if lib.Kind != "picture" {
		writeError(w, http.StatusBadRequest, "wrong_kind",
			"a timeline is a picture-library view")
		return
	}

	buckets, err := s.st.PhotoTimeline(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "photo timeline")
		return
	}
	total := 0
	for _, b := range buckets {
		total += b.Count
	}
	writeJSON(w, http.StatusOK, map[string]any{"buckets": buckets, "total": total})
}
