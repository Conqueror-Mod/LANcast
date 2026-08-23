package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"lancast/internal/auth"
	"lancast/internal/store"
)

// ctxKey namespaces values this package stores on a request context.
type ctxKey int

const sessionCtxKey ctxKey = iota

// isPublicPath reports paths reachable without a session. Deliberately short:
// the web assets are public because the login form lives in them, and health is
// public so a monitor does not need credentials.
func isPublicPath(p string) bool {
	switch p {
	case "/api/health", "/api/auth/status", "/api/auth/login", "/api/auth/setup":
		return true
	case "/api/federation/presence", "/api/federation/roster":
		// Not public: authenticated by the mutual-TLS pin instead of a session
		// (ADR 0044 §4). It is listed here because the *session* gate is the
		// wrong gate for a caller that is a server, and the handler refuses
		// anything that did not present a peer certificate.
		return true
	}
	return !strings.HasPrefix(p, "/api/")
}

// secured reports whether any account exists. Zero users is the unconfigured
// state that also keeps the server bound to loopback (ADR 0015). On a read
// error it fails closed — treating the instance as configured — so a database
// hiccup never drops the auth gate open.
func (s *Server) secured(ctx context.Context) bool {
	n, err := s.st.CountUsers(ctx)
	if err != nil {
		s.log.Error("count users", "error", err)
		return true
	}
	return n > 0
}

// requireAuth gates the API behind a session, applies the CSRF checks, and
// stashes the resolved session so handlers authorize without re-querying.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// An unconfigured server (no accounts) is loopback-only, so requiring a
		// session before setup exists would lock the owner out of their own
		// setup form.
		if !s.secured(r.Context()) {
			next.ServeHTTP(w, r)
			return
		}

		// CSRF: a state-changing request must come from this origin. Paired with
		// SameSite=Strict on the cookie — either alone leaves a gap, and this one
		// also covers non-cookie contexts.
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if !auth.SameOriginRequest(r) {
				writeError(w, http.StatusForbidden, "forbidden", "cross-origin request refused")
				return
			}
		}

		if isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		sess, ok := s.session(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
			return
		}
		// Any authenticated request means the person is here. This is what
		// makes "online" a fact rather than an assumption, and it costs
		// nothing: the request was going to happen anyway.
		s.presence.Seen(sess.UserID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionCtxKey, sess)))
	})
}

// session resolves the caller's session, refreshing its expiry as it goes.
func (s *Server) session(r *http.Request) (*store.Session, bool) {
	c, err := r.Cookie(auth.CookieName)
	if err != nil || c.Value == "" {
		return nil, false
	}
	hash := auth.HashToken(c.Value)

	sess, err := s.st.LookupSession(r.Context(), hash)
	if err != nil {
		return nil, false
	}
	// Extend on use so an active viewer is never logged out mid-film.
	_ = s.st.TouchSession(r.Context(), hash, auth.SessionTTL)
	return sess, true
}

// sessionFromContext returns the session requireAuth stashed, if any.
func sessionFromContext(r *http.Request) (*store.Session, bool) {
	sess, ok := r.Context().Value(sessionCtxKey).(*store.Session)
	return sess, ok
}

// userID is the caller's account id for per-user data. It falls back to the
// migrated 'local' id in the unconfigured/loopback state, where no session
// exists yet — matching the single-user identity that data was written under.
func (s *Server) userID(r *http.Request) string {
	if sess, ok := sessionFromContext(r); ok {
		return sess.UserID
	}
	return store.LocalUserID
}

// adminOnly wraps a handler so only an admin session may reach it. Adding a
// library is arbitrary filesystem read access, so it — and the other management
// surfaces — are gated here on the server, never merely hidden in the client.
func (s *Server) adminOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, ok := sessionFromContext(r)
		if !ok {
			// No stashed session means the unconfigured/loopback state, where
			// the owner legitimately has full access before the first account
			// exists. Anything else is a bug in the middleware ordering.
			if !s.secured(r.Context()) {
				h(w, r)
				return
			}
			writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
			return
		}
		if sess.Role != store.RoleAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "requires an administrator account")
			return
		}
		h(w, r)
	}
}

// userJSON is the public shape of an account. The password hash is never part
// of it.
func userJSON(id, name, role string) map[string]any {
	return map[string]any{"id": id, "name": name, "role": role}
}

// authStatus tells the client what to render: setup, login, or the library.
func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	configured := s.secured(r.Context())
	resp := map[string]any{
		"configured":    configured,
		"authenticated": !configured,
		"lan_enabled":   s.lanBound,
		// Reported here too, not only from setup: the first-run screen needs it
		// before an account exists, and inferring it from lan_enabled gets the
		// deliberately-loopback case wrong in the opposite direction.
		"restart_required": s.restartWidens,
	}
	if sess, ok := s.session(r); ok {
		resp["authenticated"] = true
		u := userJSON(sess.UserID, sess.Name, sess.Role)
		/*
		 * The caller's own sharing choice (ADR 0035), reported here because
		 * there was nowhere else it could come from.
		 *
		 * `/api/people` deliberately excludes the caller — a row for yourself
		 * in a list of other people is noise — so the settings toggle had no
		 * source for its own value and fell back to off on every mount,
		 * whatever the database held. It rendered unticked for somebody who
		 * had opted in, which for a privacy control is the worst direction to
		 * be wrong in: it invites you to turn on something already on, and
		 * says you are private when you are not.
		 *
		 * On the caller's own record only. Whether anybody else shares is
		 * already public on the People page; this adds nothing about them.
		 */
		if sharing, err := s.st.SharesActivity(r.Context(), sess.UserID); err != nil {
			// Not fatal: the shell reads this route constantly and must not
			// lose its session over one optional field.
			s.log.Error("read share activity", "error", err)
		} else {
			u["sharing"] = sharing
		}
		/*
		 * And whether this account appears in the roster handed to peers
		 * (ADR 0044). Here for exactly the reason `sharing` is, learned the
		 * expensive way: a control that can write a setting and cannot read it
		 * back renders off for somebody who turned it on, which for a privacy
		 * switch is the worst direction to be wrong in. Both settings are the
		 * caller's own, and this is the only route that reports them.
		 */
		if visible, err := s.st.VisibleToPeers(r.Context(), sess.UserID); err != nil {
			s.log.Error("read peer visibility", "error", err)
		} else {
			u["visible_to_peers"] = visible
		}
		resp["user"] = u
	}
	writeJSON(w, http.StatusOK, resp)
}

// authSetup creates the first account, an admin. Only available while
// unconfigured, and the server is loopback-only until that happens — so it
// cannot be raced from the network to claim someone else's instance.
//
// The first admin takes the 'local' id so it lines up with any playback rows
// written under the pre-multi-user default (ADR 0006).
func (s *Server) authSetup(w http.ResponseWriter, r *http.Request) {
	if s.secured(r.Context()) {
		writeError(w, http.StatusConflict, "conflict", "already configured")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	name := strings.TrimSpace(req.Username)
	if name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "username is required")
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

	u, err := s.st.CreateUser(r.Context(), store.LocalUserID, name, hash, store.RoleAdmin)
	if err != nil {
		s.writeInternal(w, err, "create first user")
		return
	}

	if err := s.issueSession(w, r, u.ID); err != nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured":    true,
		"authenticated": true,
		// Not !lanBound: a server the operator deliberately bound to loopback is
		// not LAN-bound either, and a restart would not change that. Promising
		// otherwise sends them to do something that cannot work.
		"restart_required": s.restartWidens,
		"user":             userJSON(u.ID, u.Name, u.Role),
	})
}

// authLogin exchanges a username and password for a session.
func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	if !s.secured(r.Context()) {
		writeError(w, http.StatusConflict, "conflict", "no accounts exist")
		return
	}

	key := auth.ClientKey(r)
	if !s.throttle.Allow(key) {
		writeError(w, http.StatusTooManyRequests, "too_many_requests",
			"too many attempts; wait a few minutes")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}

	u, err := s.st.UserByName(r.Context(), strings.TrimSpace(req.Username))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.writeInternal(w, err, "lookup user")
		return
	}
	// An unknown user and a wrong password are never distinguished in the
	// response. err != nil short-circuits before the nil-user compare.
	if err != nil || !auth.CheckPassword(u.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "incorrect username or password")
		return
	}
	s.throttle.Reset(key)

	if err := s.issueSession(w, r, u.ID); err != nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"user":          userJSON(u.ID, u.Name, u.Role),
	})
}

// authLogout ends this session only.
func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.CookieName); err == nil && c.Value != "" {
		_ = s.st.DeleteSession(r.Context(), auth.HashToken(c.Value))
	}
	http.SetCookie(w, auth.ClearCookie())
	w.WriteHeader(http.StatusNoContent)
}

// authChangePassword changes the calling user's own password and revokes only
// that user's sessions. Under one shared password a change revoked every
// session; with accounts, doing that would let one person log everyone else out
// (ADR 0015).
func (s *Server) authChangePassword(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
		return
	}

	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}

	u, err := s.st.UserByID(r.Context(), sess.UserID)
	if err != nil {
		s.writeInternal(w, err, "load user")
		return
	}
	if !auth.CheckPassword(u.PasswordHash, req.Current) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "current password is incorrect")
		return
	}

	hash, err := auth.HashPassword(req.New)
	if errors.Is(err, auth.ErrWeakPassword) {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err != nil {
		s.writeInternal(w, err, "hash password")
		return
	}

	if err := s.st.SetUserPassword(r.Context(), u.ID, hash); err != nil {
		s.writeInternal(w, err, "save password")
		return
	}
	if err := s.st.DeleteUserSessions(r.Context(), u.ID); err != nil {
		s.writeInternal(w, err, "revoke sessions")
		return
	}

	s.audit(r, "auth.password_change", "user", u.ID,
		fmt.Sprintf("%q changed their own password; all their sessions were revoked", u.Name), nil)

	http.SetCookie(w, auth.ClearCookie())
	w.WriteHeader(http.StatusNoContent)
}

// issueSession mints a token and sets the cookie for userID. It returns an error
// (after writing the response) so callers stop rather than emit a success body
// over a failed session.
func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, userID string) error {
	token, hash, err := auth.NewToken()
	if err != nil {
		s.writeInternal(w, err, "generate session")
		return err
	}
	if err := s.st.CreateSession(r.Context(), hash, userID, auth.SessionTTL); err != nil {
		s.writeInternal(w, err, "create session")
		return err
	}
	// Secure follows the connection (from the TLS work): a LAN-bound server
	// serves HTTPS, and marking the cookie Secure there stops it downgrading.
	http.SetCookie(w, auth.Cookie(token, auth.SessionTTL, r.TLS != nil))
	return nil
}
