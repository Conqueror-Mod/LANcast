package api

import (
	"net/http"
	"strconv"
	"time"

	"lancast/internal/store"
)

/*
 * GET /api/libraries/{id}/trending
 *
 * What this library's people have been playing in the last thirty days.
 *
 * The response says how many accounts contributed, and that is not a
 * decoration. With one account every count is 1 and the list is honestly just
 * "recently played" — so the client is given what it needs to say the true
 * thing rather than being handed a list that calls itself trending regardless.
 * A number that means different things at different scales should carry its
 * scale with it.
 *
 * Not admin-gated. It reports what has been played in a shared library, which
 * is what a shared library is for — and it names no accounts. Which *titles*
 * are popular is a fact about the library; who watched them is a fact about a
 * person, and this endpoint deliberately cannot answer the second.
 */
type trendingResponse struct {
	Items []store.TrendingItem `json:"items"`
	// Contributors is the number of accounts with any play in the window.
	Contributors int `json:"contributors"`
	// WindowDays says what "lately" meant, so a client need not hard-code a
	// number that lives on the server.
	WindowDays int `json:"window_days"`
}

func (s *Server) libraryTrending(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return
	}
	if _, err := s.st.GetLibrary(r.Context(), id); s.notFoundOr(w, err, "get library", "no such library") {
		return
	}

	limit := 12
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 50 {
		limit = v
	}

	now := time.Now()
	items, err := s.st.Trending(r.Context(), id, limit, now)
	if err != nil {
		s.writeInternal(w, err, "trending")
		return
	}
	contributors, err := s.st.TrendingContributors(r.Context(), id, now)
	if err != nil {
		s.writeInternal(w, err, "trending contributors")
		return
	}

	writeJSON(w, http.StatusOK, trendingResponse{
		Items:        items,
		Contributors: contributors,
		WindowDays:   int(store.TrendingWindow / (24 * time.Hour)),
	})
}
