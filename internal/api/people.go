package api

import (
	"encoding/json"
	"net/http"
	"strconv"
)

/*
 * People on this server, and what they have chosen to share (ADR 0035).
 *
 * "Find Friends" on a self-hosted household server means the other accounts on
 * it. There is no directory to search and no second server to federate with, so
 * the honest version is a list of who else is here.
 *
 * Every route reads the sharing flag from the store rather than checking it
 * here, because the check belongs where the rows are: a handler that forgets it
 * would publish somebody's viewing, and nothing about the response would look
 * wrong.
 */

func (s *Server) listPeople(w http.ResponseWriter, r *http.Request) {
	people, err := s.st.People(r.Context(), s.userID(r))
	if err != nil {
		s.writeInternal(w, err, "list people")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": people})
}

/*
 * personActivity returns what one person has published.
 *
 * There is no route that answers "what has this person been watching" for
 * somebody who has not opted in — this one returns an empty list rather than a
 * 403, because the two are the same answer from outside and a 403 would confirm
 * that there *is* something being withheld. What somebody watches is private;
 * so is the size of it.
 */
func (s *Server) personActivity(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid account id")
		return
	}
	limit := 20
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 100 {
		limit = v
	}

	entries, err := s.st.SharedActivity(r.Context(), id, limit)
	if err != nil {
		s.writeInternal(w, err, "shared activity")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"activity": entries})
}

/*
 * putSharing records the caller's own decision, and only ever the caller's.
 *
 * There is deliberately no admin variant. An administrator may run the server;
 * a switch somebody else can flip on your behalf is not consent, and ADR 0035
 * says so in as many words.
 */
func (s *Server) putSharing(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusConflict, "no_account",
			"this server has no accounts yet; create one first")
		return
	}

	var req struct {
		Share bool `json:"share"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}

	if err := s.st.SetShareActivity(r.Context(), sess.UserID, req.Share); err != nil {
		s.writeInternal(w, err, "set sharing")
		return
	}
	// Audited, because it changes who can see something about a person — the
	// same class of act as a role change, and the one somebody would want to
	// find in a log if it were ever flipped without their knowing.
	verb := "stopped sharing"
	if req.Share {
		verb = "started sharing"
	}
	s.audit(r, "profile.sharing", "user", sess.UserID,
		verb+" their watch activity with others on this server", nil)
	writeJSON(w, http.StatusOK, map[string]any{"share": req.Share})
}
