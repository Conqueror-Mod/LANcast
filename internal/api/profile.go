package api

import (
	"net/http"
	"strconv"

	"lancast/internal/store"
)

/*
 * Who you are, and what you have watched.
 *
 * One request rather than three, for the same reason /api/activity is one
 * request: a page that needs identity, totals and a list in order to render
 * anything should not have to discover that from three round trips and three
 * loading states.
 *
 * Strictly the caller's own. There is no /api/profile/{id}, and adding one is
 * a decision rather than a route — "what has everyone been watching" is the
 * viewer-stats item on the roadmap, and it needs an answer to who may see it
 * before it needs a handler.
 */
type profileResponse struct {
	User    profileUser          `json:"user"`
	Stats   store.ProfileStats   `json:"stats"`
	History []store.HistoryEntry `json:"history"`
	// HasMore says whether the history was cut short, so the client can offer
	// to fetch the next page instead of guessing from a full-looking one.
	HasMore bool `json:"has_more"`
}

type profileUser struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Admin bool   `json:"admin"`
	// Secured is false on an unconfigured loopback server, where there is no
	// account and the history belongs to the migrated 'local' id. The page says
	// so rather than inventing a person.
	Secured bool `json:"secured"`
}

func (s *Server) profile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := s.userID(r)

	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 200 {
		limit = v
	}
	offset := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v > 0 {
		offset = v
	}

	// One more than asked for: the extra row is how HasMore is known without a
	// second COUNT over the same join.
	entries, err := s.st.History(ctx, userID, limit+1, offset)
	if err != nil {
		s.writeInternal(w, err, "history")
		return
	}
	more := len(entries) > limit
	if more {
		entries = entries[:limit]
	}

	stats, err := s.st.ProfileStatistics(ctx, userID)
	if err != nil {
		s.writeInternal(w, err, "profile statistics")
		return
	}

	who := profileUser{ID: userID, Name: "Local", Secured: s.secured(ctx)}
	if sess, ok := sessionFromContext(r); ok {
		who.ID = sess.UserID
		if u, err := s.st.UserByID(ctx, sess.UserID); err == nil {
			who.Name = u.Name
			who.Admin = u.Role == store.RoleAdmin
		}
	}

	writeJSON(w, http.StatusOK, profileResponse{
		User: who, Stats: stats, History: entries, HasMore: more,
	})
}
