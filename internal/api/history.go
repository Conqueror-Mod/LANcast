package api

import (
	"fmt"
	"net/http"

	"lancast/internal/store"
)

/*
 * Forgetting what you watched.
 *
 * Two endpoints and one rule: the account is the session's, never the
 * request's. Playback state is keyed by user (ADR 0006) so that one person's
 * viewing is their own, and an endpoint that accepted a user id would hand an
 * administrator a way to erase somebody else's history — which is not an
 * administrative power this server grants, because there is nothing to
 * administer about a record only its owner can see.
 *
 * GET answers how much a reset would remove, so the confirmation can name a
 * number. DELETE performs it. Separating them means the client can show the
 * cost *before* asking, and a person who expected to clear one show and is
 * told four hundred things has learned something while it is still free.
 */

// historyScope reads the scope parameter, defaulting to everything.
func historyScope(r *http.Request) (store.HistoryScope, bool) {
	switch r.URL.Query().Get("scope") {
	case "", "all":
		return store.HistoryAll, true
	case "finished":
		return store.HistoryFinished, true
	case "unfinished":
		return store.HistoryUnfinished, true
	}
	return "", false
}

// historyPreview answers how many playback records a reset would remove.
func (s *Server) historyPreview(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusConflict, "no_account",
			"this server has no accounts yet, so there is no history to forget")
		return
	}
	scope, valid := historyScope(r)
	if !valid {
		writeError(w, http.StatusBadRequest, "bad_request",
			"that is not a scope this server knows")
		return
	}
	n, err := s.st.HistoryCount(r.Context(), sess.UserID, scope, int64(queryInt(r, "under")))
	if err != nil {
		s.writeInternal(w, err, "history count")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": n, "scope": string(scope)})
}

/*
 * resetHistory deletes it.
 *
 * Audited (ADR 0026), because this is destructive and irreversible and belongs
 * in the same class as removing a library. The entry records the scope and the
 * count rather than the rows, since "forgot 412 finished items" is what a
 * person reading the log a month later needs, and a list of item ids for things
 * that may since have been deleted is not.
 */
func (s *Server) resetHistory(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusConflict, "no_account",
			"this server has no accounts yet, so there is no history to forget")
		return
	}
	scope, valid := historyScope(r)
	if !valid {
		writeError(w, http.StatusBadRequest, "bad_request",
			"that is not a scope this server knows")
		return
	}
	under := int64(queryInt(r, "under"))

	n, err := s.st.ResetHistory(r.Context(), sess.UserID, scope, under)
	if err != nil {
		s.writeInternal(w, err, "reset history")
		return
	}

	what := map[store.HistoryScope]string{
		store.HistoryAll:        "everything",
		store.HistoryFinished:   "finished items",
		store.HistoryUnfinished: "unfinished items",
	}[scope]
	where := ""
	if under > 0 {
		where = fmt.Sprintf(" under item %d", under)
	}
	s.audit(r, "history.reset", "user", sess.UserID,
		fmt.Sprintf("forgot %d %s%s from their viewing history", n, what, where), nil)

	writeJSON(w, http.StatusOK, map[string]any{"removed": n, "scope": string(scope)})
}
