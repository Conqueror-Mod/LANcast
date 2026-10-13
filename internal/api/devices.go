package api

import (
	"fmt"
	"net/http"

	"lancast/internal/auth"
	"lancast/internal/store"
)

/*
 * Authorized devices (docs/settings-plan.md, step 4): every device signed in,
 * what it is, when it was last used and from where, and Sign out.
 *
 * Your own under /api/profile/sessions, for anybody signed in; everyone's
 * under /api/sessions, for an administrator. A session is named by a short
 * public handle (store.SessionHandle) — the token, and even its full hash,
 * never leave the server.
 */

// currentHandle is the handle of the session making this request, if any.
func currentHandle(r *http.Request) string {
	c, err := r.Cookie(auth.CookieName)
	if err != nil || c.Value == "" {
		return ""
	}
	return store.SessionHandle(auth.HashToken(c.Value))
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request, userID string) {
	list, err := s.st.ListSessions(r.Context(), userID)
	if err != nil {
		s.writeInternal(w, err, "list sessions")
		return
	}
	cur := currentHandle(r)
	for i := range list {
		list[i].Current = cur != "" && list[i].ID == cur
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": list})
}

// mySessions lists the caller's own devices.
func (s *Server) mySessions(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": []store.SessionInfo{}})
		return
	}
	s.listSessions(w, r, sess.UserID)
}

// allSessions lists every account's devices, for an administrator.
func (s *Server) allSessions(w http.ResponseWriter, r *http.Request) {
	s.listSessions(w, r, "")
}

// signOutMine signs out one of the caller's own devices. Signing out the
// device asking is signing out: the cookie goes too.
func (s *Server) signOutMine(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
		return
	}
	s.signOut(w, r, sess.UserID)
}

// signOutAny signs out any account's device, for an administrator.
func (s *Server) signOutAny(w http.ResponseWriter, r *http.Request) {
	s.signOut(w, r, "")
}

func (s *Server) signOut(w http.ResponseWriter, r *http.Request, userID string) {
	handle := r.PathValue("id")
	removed, err := s.st.DeleteSessionByHandle(r.Context(), userID, handle)
	if err != nil {
		s.writeInternal(w, err, "sign out session")
		return
	}
	if !removed {
		writeError(w, http.StatusNotFound, "not_found", "no such device")
		return
	}
	if handle == currentHandle(r) {
		http.SetCookie(w, auth.ClearCookie())
	}
	s.audit(r, "session.revoke", "session", handle,
		fmt.Sprintf("signed out a device (%s)", handle), nil)
	w.WriteHeader(http.StatusNoContent)
}

// signOutOthers signs out every one of the caller's devices except the one
// asking — the answer to a list of sign-ins nobody recognises, and to a
// device that has gone missing.
func (s *Server) signOutOthers(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	c, err := r.Cookie(auth.CookieName)
	if !ok || err != nil || c.Value == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
		return
	}
	// Sweep the dead rows first, so the count — and the audit line — is of
	// devices that could still sign in, not of rows that had already expired.
	if err := s.st.PurgeExpiredSessions(r.Context()); err != nil {
		s.log.Warn("purge expired sessions", "err", err)
	}
	n, err := s.st.DeleteOtherSessions(r.Context(), sess.UserID, auth.HashToken(c.Value))
	if err != nil {
		s.writeInternal(w, err, "sign out other sessions")
		return
	}
	if n > 0 {
		s.audit(r, "session.revoke_others", "user", sess.UserID,
			fmt.Sprintf("signed out %d other devices", n), nil)
	}
	writeJSON(w, http.StatusOK, map[string]any{"signed_out": n})
}
