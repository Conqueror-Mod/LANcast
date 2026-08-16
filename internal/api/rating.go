package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"lancast/internal/store"
)

/*
 * Your rating, and nobody else's.
 *
 * The roadmap holds "ratings/reviews" back alongside viewer stats because both
 * wait on a decision about who may see whose viewing. This makes the smaller
 * half of that decision and states it in the contract rather than leaving it to
 * be inferred: **a rating is private to the account that wrote it.** There is no
 * household average, no count of how many people rated something, and no route
 * that returns somebody else's score.
 *
 * That is why these routes carry no id but the item's. "Whose rating" is always
 * the caller's, which makes it impossible to add a leak by forgetting a filter
 * — the only user id in the query comes from the session.
 */

const maxReviewLength = 4000

func (s *Server) getRating(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	rating, err := s.st.GetRating(r.Context(), id, s.userID(r))
	if err != nil {
		s.writeInternal(w, err, "get rating")
		return
	}
	// A null rating is the answer, not an absence: "you have not rated this" is
	// a different statement from "this item does not exist", and a 404 here
	// would conflate them.
	writeJSON(w, http.StatusOK, map[string]any{"rating": rating})
}

func (s *Server) putRating(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	var req struct {
		Score  int    `json:"score"`
		Review string `json:"review"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	if req.Score < store.MinScore || req.Score > store.MaxScore {
		writeError(w, http.StatusBadRequest, "bad_request",
			"score must be between 1 and 10")
		return
	}
	// Bounded, because it is stored and returned: a note to yourself has no
	// business being a megabyte, and refusing at the edge beats discovering the
	// limit when a listing gets slow.
	review := strings.TrimSpace(req.Review)
	if len(review) > maxReviewLength {
		writeError(w, http.StatusBadRequest, "bad_request",
			"review is too long")
		return
	}
	if _, err := s.st.GetItem(r.Context(), id, s.userID(r)); s.notFoundOr(w, err, "get item", "no such item") {
		return
	}

	if err := s.st.SetRating(r.Context(), id, s.userID(r), req.Score, review); err != nil {
		s.writeInternal(w, err, "set rating")
		return
	}
	rating, err := s.st.GetRating(r.Context(), id, s.userID(r))
	if err != nil {
		s.writeInternal(w, err, "get rating")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rating": rating})
}

// deleteRating withdraws a verdict, which is not the same as scoring something
// one — "I have not rated this" and "I rated this badly" are different
// statements, and an interface that cannot say the first is one people stop
// trusting with the second.
func (s *Server) deleteRating(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	if err := s.st.ClearRating(r.Context(), id, s.userID(r)); err != nil {
		s.writeInternal(w, err, "clear rating")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listMyRatings is the profile's "what you thought" list. Under /api/profile
// rather than /api/ratings for the same reason the item routes carry no user
// id: there is one person whose ratings are readable, and the path says so.
func (s *Server) listMyRatings(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 200 {
		limit = v
	}
	items, err := s.st.ListRatings(r.Context(), s.userID(r), limit)
	if err != nil {
		s.writeInternal(w, err, "list ratings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ratings": items})
}
