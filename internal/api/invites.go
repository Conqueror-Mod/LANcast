package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"lancast/internal/auth"
	"lancast/internal/rating"
	"lancast/internal/store"
)

/*
 * Invites (ADR 0104): an administrator makes a link, and whoever opens it
 * makes their own member account with it, once.
 *
 * Three administrator routes (make, list, withdraw) and two public ones
 * (check, redeem). The public ones take the secret in a POST body, never in
 * the path or the query: the link carries it in the fragment, which a browser
 * never sends, so it reaches neither the request log nor a Referer.
 */

// inviteLife is how long an invite works for: long enough to reach somebody
// who checks messages once a week, short enough that a forgotten link dies.
const inviteLife = 7 * 24 * time.Hour

type inviteRequest struct {
	Note             string  `json:"note"`
	MaxContentRating string  `json:"max_content_rating"`
	HiddenLibraries  []int64 `json:"hidden_libraries"`
}

// createInvite answers POST /api/invites. Admin-only. The secret is in this
// answer and nowhere else, ever.
func (s *Server) createInvite(w http.ResponseWriter, r *http.Request) {
	var req inviteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	note := strings.TrimSpace(req.Note)
	if len(note) > 200 {
		writeError(w, http.StatusBadRequest, "bad_request", "a note is at most 200 characters")
		return
	}
	// The same check SetMaxContentRating makes, made now rather than when the
	// account is created: a ceiling nothing can place would mean "no limit"
	// and the administrator would believe otherwise.
	if req.MaxContentRating != "" && !rating.Known(req.MaxContentRating) {
		writeError(w, http.StatusBadRequest, "bad_request",
			fmt.Sprintf("%q is not a rating this server can place", req.MaxContentRating))
		return
	}
	for _, id := range req.HiddenLibraries {
		if _, err := s.st.GetLibrary(r.Context(), id); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("no library %d", id))
			return
		}
	}
	token, hash, err := auth.NewToken()
	if err != nil {
		s.writeInternal(w, err, "make invite secret")
		return
	}
	me := s.actorName(r)
	now := time.Now()
	inv, err := s.st.CreateInvite(r.Context(), store.Invite{
		Note: note, MaxContentRating: req.MaxContentRating, HiddenLibraries: req.HiddenLibraries,
		CreatedBy: me, CreatedAt: now.Unix(), ExpiresAt: now.Add(inviteLife).Unix(),
	}, []byte(hash))
	if err != nil {
		s.writeInternal(w, err, "create invite")
		return
	}
	s.audit(r, "invite.create", "invite", inv.ID, describeInvite("Made an invite", inv), nil)
	writeJSON(w, http.StatusCreated, map[string]any{
		"invite": inv,
		"token":  token,
		// Where the link should point, when this server has a name the outside
		// world can reach; otherwise empty, and the client says so.
		"public_name": s.publicName(),
	})
}

func describeInvite(verb string, inv *store.Invite) string {
	if inv.Note != "" {
		return fmt.Sprintf("%s (%s)", verb, inv.Note)
	}
	return verb
}

// listInvites answers GET /api/invites. Admin-only. Expired invites are
// pruned on the way, so what is listed is what still works.
func (s *Server) listInvites(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.Invites(r.Context(), time.Now())
	if err != nil {
		s.writeInternal(w, err, "list invites")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invites": list})
}

// deleteInvite answers DELETE /api/invites/{id}. Admin-only.
func (s *Server) deleteInvite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.st.DeleteInvite(r.Context(), id); s.notFoundOr(w, err, "withdraw invite", "no such invite") {
		return
	}
	s.audit(r, "invite.delete", "invite", id, "Withdrew an invite", nil)
	w.WriteHeader(http.StatusNoContent)
}

// inviteToken reads the secret from a public route's body.
func readInviteToken(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(into); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return false
	}
	return true
}

/*
 * inviteUnusable answers for a secret that is unknown, spent, withdrawn or
 * expired: one answer for all four, and counted per address, so a run of
 * guesses ends in 429. Only a failed lookup counts. Somebody choosing a name
 * that is taken, or a password that is too short, is not guessing.
 */
func (s *Server) inviteUnusable(w http.ResponseWriter, r *http.Request) {
	key := "invite:" + auth.ClientKey(r)
	if !s.inviteThrottle.Allow(key) {
		s.log.Warn("invite refused", "reason", "throttled", "client", auth.ClientKey(r))
		writeError(w, http.StatusTooManyRequests, "too_many_requests",
			"too many attempts; wait a few minutes")
		return
	}
	writeError(w, http.StatusNotFound, "not_found", store.ErrInviteUnusable.Error())
}

// checkInvite answers POST /api/invites/check {token}: whether the link still
// works, and what the page can say about it. Public.
func (s *Server) checkInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !readInviteToken(w, r, &req) {
		return
	}
	inv, err := s.st.InviteByToken(r.Context(), []byte(auth.HashToken(req.Token)), time.Now())
	if errors.Is(err, store.ErrInviteUnusable) || req.Token == "" {
		s.inviteUnusable(w, r)
		return
	}
	if err != nil {
		s.writeInternal(w, err, "check invite")
		return
	}
	// Who made it and when it ends. Not the note: that is the administrator's
	// words about the person, written for the administrator.
	writeJSON(w, http.StatusOK, map[string]any{
		"from":       inv.CreatedBy,
		"expires_at": inv.ExpiresAt,
	})
}

// redeemInvite answers POST /api/invites/redeem {token, username, password}:
// make the account and sign in as it. Public.
func (s *Server) redeemInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readInviteToken(w, r, &req) {
		return
	}
	if req.Token == "" {
		s.inviteUnusable(w, r)
		return
	}
	name := strings.TrimSpace(req.Username)
	if name == "" || len(name) > 64 {
		writeError(w, http.StatusBadRequest, "bad_request", "choose a name of up to 64 characters")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if errors.Is(err, auth.ErrWeakPassword) {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err != nil {
		s.writeInternal(w, err, "hash password")
		return
	}
	u, inv, err := s.st.RedeemInvite(r.Context(), []byte(auth.HashToken(req.Token)), name, hash, time.Now())
	switch {
	case errors.Is(err, store.ErrInviteUnusable):
		s.inviteUnusable(w, r)
		return
	case errors.Is(err, store.ErrDuplicate):
		writeError(w, http.StatusConflict, "conflict", "that name is taken; choose another")
		return
	case err != nil:
		s.writeInternal(w, err, "redeem invite")
		return
	}
	s.auditAs(r, u.ID, u.Name, "invite.redeem", "user", u.ID,
		describeInvite(fmt.Sprintf("%q joined with an invite from %s", u.Name, inv.CreatedBy), inv), nil)
	if err := s.issueSession(w, r, u.ID); err != nil {
		return
	}
	writeJSON(w, http.StatusCreated, userJSON(u.ID, u.Name, u.Role))
}
