package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"lancast/internal/together"
)

/*
 * Watch together.
 *
 * The server owns the truth — what is playing, where it is, whether it is
 * paused — and clients converge on it. That is the first principle applied to a
 * feature that could easily have been built the other way, with each client
 * broadcasting its position and the last writer winning; on a lossy connection
 * the winner is whoever lagged worst, and everybody else gets dragged backwards.
 *
 * Every route needs a session but no particular role. Watching something with
 * the people you live with is not an administrative act, and gating it on admin
 * would make the feature useless on exactly the servers it is for.
 */

// togetherError maps the session manager's errors onto the API envelope. The
// distinction matters to a client: "the room is gone" ends the experience,
// "you are not the host" is a control it should not have offered.
func (s *Server) togetherError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, together.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "that session has ended")
	case errors.Is(err, together.ErrNotHost):
		writeError(w, http.StatusForbidden, "forbidden", "only the host controls playback in a session")
	case errors.Is(err, together.ErrNotMember):
		writeError(w, http.StatusForbidden, "forbidden", "you are not in that session")
	default:
		s.writeInternal(w, err, "watch together")
	}
}

// whoami resolves the caller's id and display name once. The name is frozen
// into the room at join time, the same way the audit log freezes an actor:
// a member list should stay readable while somebody is being renamed.
func (s *Server) whoami(r *http.Request) (id, name string) {
	id = s.userID(r)
	name = "Local"
	if sess, ok := sessionFromContext(r); ok {
		if u, err := s.st.UserByID(r.Context(), sess.UserID); err == nil {
			name = u.Name
		}
	}
	return id, name
}

func (s *Server) listTogether(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sessions": s.together.List()})
}

func (s *Server) createTogether(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ItemID     int64 `json:"item_id"`
		PositionMS int64 `json:"position_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	if req.ItemID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "item_id is required")
		return
	}
	// The item is checked here so a room can never be opened around something
	// that does not exist — everyone who joined would sit looking at a player
	// that could not load, with nothing to say why.
	if _, err := s.st.GetItem(r.Context(), req.ItemID, s.userID(r)); s.notFoundOr(w, err, "get item", "no such item") {
		return
	}

	id, name := s.whoami(r)
	writeJSON(w, http.StatusCreated, s.together.Create(req.ItemID, id, name, req.PositionMS))
}

func (s *Server) joinTogether(w http.ResponseWriter, r *http.Request) {
	id, name := s.whoami(r)
	sess, err := s.together.Join(r.PathValue("id"), id, name)
	if err != nil {
		s.togetherError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

// pollTogether is the follower's whole synchronisation input, and it doubles as
// the signal that this member is still here — nobody presses "leave", they
// close the laptop.
func (s *Server) pollTogether(w http.ResponseWriter, r *http.Request) {
	sess, err := s.together.Poll(r.PathValue("id"), s.userID(r))
	if err != nil {
		s.togetherError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) reportTogether(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PositionMS int64 `json:"position_ms"`
		Paused     bool  `json:"paused"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	sess, err := s.together.Report(r.PathValue("id"), s.userID(r), req.PositionMS, req.Paused)
	if err != nil {
		s.togetherError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) leaveTogether(w http.ResponseWriter, r *http.Request) {
	if err := s.together.Leave(r.PathValue("id"), s.userID(r)); err != nil {
		s.togetherError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
