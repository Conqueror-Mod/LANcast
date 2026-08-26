package api

import (
	"encoding/json"
	"net/http"
)

/*
 * Choosing which of its films a collection wears.
 *
 * The inherited default — the earliest release — is right for almost every
 * franchise and wrong for some. The Marvel Cinematic Universe wearing Iron Man
 * (2008) is defensible and is not what somebody who has looked at it wants. So
 * the rule stays the default and this is the disagreement, which is the shape
 * every correction in this project takes: a heuristic good enough to ship, and
 * a person allowed to overrule it.
 *
 * Admin-only, matching every other write that changes what a library looks like
 * to everyone — PATCH /api/items/{id}, the match override, the locks. This is
 * shared state, not a per-viewer preference: there is one poster and everybody
 * sees it.
 */
func (s *Server) putCollectionPoster(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	if it.Kind != "collection" {
		writeError(w, http.StatusBadRequest, "bad_request",
			"only a collection takes a poster from one of its members")
		return
	}

	var req struct {
		// FromItemID is the member whose poster to wear. Zero clears the
		// override and returns to the default.
		FromItemID int64 `json:"from_item_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}

	if req.FromItemID == 0 {
		if err := s.st.ClearCollectionPoster(r.Context(), id); err != nil {
			s.writeInternal(w, err, "clear collection poster")
			return
		}
		s.audit(r, "collection.poster", "item", auditID(id),
			"Reset the poster for "+it.Title+" to the default", nil)
		s.respondItem(w, r, id)
		return
	}

	/*
	 * A non-member is a 400, not a 404 and not a silent success.
	 *
	 * The store refuses it -- the id arrives from a client and that is the
	 * boundary where a bad one becomes "any item's poster on any collection" --
	 * and the honest report is that the request was wrong, rather than letting
	 * a caller believe a poster was set that was not.
	 */
	if err := s.st.SetCollectionPoster(r.Context(), id, req.FromItemID); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request",
			"that item is not in this collection, or has no poster of its own")
		return
	}

	s.audit(r, "collection.poster", "item", auditID(id),
		"Set the poster for "+it.Title+" from one of its films",
		map[string]any{"from_item_id": req.FromItemID})
	s.respondItem(w, r, id)
}
