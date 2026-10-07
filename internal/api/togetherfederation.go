package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"lancast/internal/together"
)

/*
 * A room crossing to another server, from the host's side.
 *
 * Phase 5 of the federation plan
 * ([plan](../../docs/phase-5-room-crosses-the-boundary-plan.md)). Somebody on a
 * paired server who may see what you are watching may ask to join it; you
 * answer in the moment; if you say yes they follow your room through their own
 * server, which relays everything over the pinned peer channel
 * ([ADR 0046](../../docs/adr/0046-remote-guests.md), amended).
 *
 * Two families of route live here, and they are authenticated differently on
 * purpose:
 *
 *   - `/api/federation/together/…` is called by the *friend's server*, over
 *     mutual TLS. Which server is asking is proved by the pinned key; which
 *     person is that server's word, carried as `?person=`, exactly as
 *     presence has it.
 *   - `/api/together/requests…` is called by the *host's own client*, with
 *     an ordinary session. It is where the host sees and answers a request.
 *
 * # What the friend's server is never told
 *
 * Why not. A missing grant and a person who does not exist both answer 404;
 * a host who is not watching, a host watching a friend's film, a decline, a
 * timeout and a cooldown all answer "not now". ADR 0045 §7: a decline that
 * explains itself invites a negotiation about why.
 */

// federationPerson resolves the caller of a /api/federation/together route:
// the paired server, by its pinned key, and the person it names.
func (s *Server) federationPerson(w http.ResponseWriter, r *http.Request) (fingerprint, person string, ok bool) {
	fingerprint, ok = s.federationPeer(w, r)
	if !ok {
		return "", "", false
	}
	person = r.URL.Query().Get("person")
	if person == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "which person is asking")
		return "", "", false
	}
	return fingerprint, person, true
}

/*
 * mayAsk answers whether a remote person may ask this host anything at all:
 * the host has granted them presence (ADR 0045 §7 — the grant carries the
 * right to ask). Read from the database on every call, never remembered, so
 * revoking presence revokes the right to ask on the next request.
 */
func (s *Server) mayAsk(r *http.Request, fingerprint, person, hostID string) (bool, error) {
	readers, err := s.st.ReadersOf(r.Context(), fingerprint, person)
	if err != nil {
		return false, err
	}
	return slices.Contains(readers, hostID), nil
}

// notNow is the one answer an asker gets for every kind of no.
func notNow(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{"id": "", "state": "not_now"})
}

/*
 * federationAskTogether is a remote person asking to join a host here.
 *
 * Addressed to a person, not to a room: the asker saw a title on the People
 * page, not a room id, and the host may well be watching alone, which is the
 * ordinary case. The host's client opens a room when it accepts.
 */
func (s *Server) federationAskTogether(w http.ResponseWriter, r *http.Request) {
	fingerprint, person, ok := s.federationPerson(w, r)
	if !ok {
		return
	}
	var req struct {
		Host string `json:"host"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Host == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "which host")
		return
	}

	granted, err := s.mayAsk(r, fingerprint, person, req.Host)
	if err != nil {
		s.writeInternal(w, err, "presence readers")
		return
	}
	if !granted {
		// The same answer as for a person who does not exist: the asker may
		// not learn who is here beyond what they were granted.
		writeError(w, http.StatusNotFound, "not_found", "no such person")
		return
	}

	/*
	 * Only somebody watching a film on this server can be joined. Not
	 * watching at all, or watching a friend's film (ADR 0045 §10 lets
	 * presence name one), both read as "not now" — a room can only be built
	 * around a film this server holds, and saying which case it was would
	 * tell the asker where the host's film lives.
	 */
	st, online := s.presence.Snapshot(req.Host)
	if !online || st.Watching == "" || !st.Here {
		notNow(w)
		return
	}

	// The asker's name comes from the roster this server already holds, not
	// from the request: a peer vouches for which person is asking, and the
	// roster is what that person published about themselves.
	name, server := s.remoteNames(r, fingerprint, person)

	asked := s.together.Ask(req.Host, fingerprint, person, name, server)
	answer := s.together.Status(asked.ID, fingerprint, person)
	if answer.State == together.RequestPending {
		s.log.Info("watch together: asked to join", "request", asked.ID,
			"host", s.displayName(r.Context(), req.Host), "asker", name, "server", server)
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": asked.ID, "state": answer.State, "room_id": answer.RoomID})
}

// remoteNames is how a remote person is shown: their roster name and their
// server's name, both as this server last learned them.
func (s *Server) remoteNames(r *http.Request, fingerprint, person string) (name, server string) {
	name = "Someone"
	if people, err := s.st.RemotePeople(r.Context(), fingerprint); err == nil {
		for _, p := range people {
			if p.ID == person && p.Name != "" {
				name = p.Name
			}
		}
	}
	if p, err := s.st.PeerByFingerprint(r.Context(), fingerprint); err == nil {
		server = p.Name
	}
	return name, server
}

// federationTogetherRequest is the asker's server polling for the answer.
func (s *Server) federationTogetherRequest(w http.ResponseWriter, r *http.Request) {
	fingerprint, person, ok := s.federationPerson(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	answer := s.together.Status(id, fingerprint, person)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "state": answer.State, "room_id": answer.RoomID})
}

/*
 * remoteRoom runs one room call for a remote member, and is where membership
 * is re-checked against the host's grant.
 *
 * The request is not the only consent that matters: a host who revokes
 * presence mid-film has said this person should not see what they are
 * watching, and the room shows exactly that. So every join and poll checks
 * the grant still stands, and a member whose grant is gone is removed and
 * told the room has ended.
 *
 * Every refusal is 404. A remote caller cannot distinguish "no such room",
 * "not admitted" and "no longer granted", so the routes cannot be used to
 * learn which rooms exist.
 */
func (s *Server) remoteRoom(w http.ResponseWriter, r *http.Request,
	call func(id, fingerprint, person string) (together.Session, error)) {
	fingerprint, person, ok := s.federationPerson(w, r)
	if !ok {
		return
	}
	id := r.PathValue("room")
	sess, err := call(id, fingerprint, person)
	if err != nil {
		if errors.Is(err, together.ErrNotFound) || errors.Is(err, together.ErrNotMember) {
			writeError(w, http.StatusNotFound, "not_found", "that session has ended")
			return
		}
		s.writeInternal(w, err, "watch together")
		return
	}
	granted, err := s.mayAsk(r, fingerprint, person, sess.HostID)
	if err != nil {
		s.writeInternal(w, err, "presence readers")
		return
	}
	if !granted {
		_, _ = s.together.LeaveRoom(id, together.RemoteID(fingerprint, person))
		s.log.Info("watch together: remote member removed, presence revoked",
			"room", id, "server", fingerprint)
		writeError(w, http.StatusNotFound, "not_found", "that session has ended")
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) federationJoinTogether(w http.ResponseWriter, r *http.Request) {
	s.remoteRoom(w, r, s.together.JoinRemote)
}

func (s *Server) federationPollTogether(w http.ResponseWriter, r *http.Request) {
	s.remoteRoom(w, r, func(id, fingerprint, person string) (together.Session, error) {
		return s.together.Poll(id, together.RemoteID(fingerprint, person))
	})
}

// federationLeaveTogether removes a remote member. Leaving a room one is not
// in is success: afterwards they are not in it.
func (s *Server) federationLeaveTogether(w http.ResponseWriter, r *http.Request) {
	fingerprint, person, ok := s.federationPerson(w, r)
	if !ok {
		return
	}
	id := r.PathValue("room")
	if _, err := s.together.LeaveRoom(id, together.RemoteID(fingerprint, person)); err != nil &&
		!errors.Is(err, together.ErrNotFound) {
		s.writeInternal(w, err, "watch together")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- the host's own client ---------------------------------------------------

// listTogetherRequests is the host's client asking whether anybody wants in.
// Polled while something is playing, so a prompt appears whatever screen the
// host is on.
func (s *Server) listTogetherRequests(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"requests": s.together.Pending(s.userID(r))})
}

/*
 * acceptTogetherRequest says yes, into a room the caller hosts.
 *
 * The client opens the room first when the host was watching alone. Accepting
 * into a room somebody else drives is refused: the host's answer is consent
 * to share *their* room.
 */
func (s *Server) acceptTogetherRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RoomID string `json:"room_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RoomID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "which room")
		return
	}
	id := r.PathValue("id")
	sess, err := s.together.Accept(id, s.userID(r), req.RoomID)
	if err != nil {
		s.requestError(w, err)
		return
	}
	s.log.Info("watch together: request accepted", "request", id, "room", sess.ID)
	writeJSON(w, http.StatusOK, sess)
}

// declineTogetherRequest says not now. The asker learns nothing more.
func (s *Server) declineTogetherRequest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.together.Decline(id, s.userID(r)); err != nil {
		s.requestError(w, err)
		return
	}
	s.log.Info("watch together: request declined", "request", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) requestError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, together.ErrRequestClosed):
		writeError(w, http.StatusConflict, "conflict", "that request has already been answered or has timed out")
	case errors.Is(err, together.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "no such request or room")
	default:
		s.togetherError(w, err)
	}
}
