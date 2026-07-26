package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"lancast/internal/auth"
	"lancast/internal/store"
)

// publicPaths are reachable without a session. Everything else requires one.
//
// Deliberately short. The web assets are public because the login form lives
// in them; health is public so a monitor does not need credentials.
func isPublicPath(p string) bool {
	switch p {
	case "/api/health", "/api/auth/status", "/api/auth/login", "/api/auth/setup":
		return true
	}
	return !strings.HasPrefix(p, "/api/")
}

// requireAuth gates the API behind a session and applies the CSRF checks.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// An unconfigured server is loopback-only (enforced at bind time), so
		// requiring a session before setup exists would lock the owner out of
		// their own setup form.
		if !s.settings.Get().Secured() {
			next.ServeHTTP(w, r)
			return
		}

		// CSRF: a state-changing request must come from this origin. Paired
		// with SameSite=Strict on the cookie — either alone leaves a gap, and
		// this one also covers non-cookie contexts.
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

		if _, ok := s.session(r); !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
			return
		}
		next.ServeHTTP(w, r)
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

// authStatus tells the client what to render: setup, login, or the library.
func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	cur := s.settings.Get()
	_, signedIn := s.session(r)

	writeJSON(w, http.StatusOK, map[string]any{
		"configured":    cur.Secured(),
		"authenticated": !cur.Secured() || signedIn,
		"lan_enabled":   s.lanBound,
	})
}

// authSetup sets the first password.
//
// Only available while unconfigured, and the server is loopback-only until
// that happens — so this cannot be raced from the network to claim someone
// else's instance.
func (s *Server) authSetup(w http.ResponseWriter, r *http.Request) {
	if s.settings.Get().Secured() {
		writeError(w, http.StatusConflict, "conflict", "a password is already set")
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
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

	next := s.settings.Get()
	next.PasswordHash = hash
	if err := s.settings.Set(next); err != nil {
		s.writeInternal(w, err, "save password")
		return
	}

	s.issueSession(w, r)
	writeJSON(w, http.StatusOK, map[string]any{
		"configured":       true,
		"authenticated":    true,
		"restart_required": !s.lanBound,
	})
}

// authLogin exchanges a password for a session.
func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	cur := s.settings.Get()
	if !cur.Secured() {
		writeError(w, http.StatusConflict, "conflict", "no password is set")
		return
	}

	key := auth.ClientKey(r)
	if !s.throttle.Allow(key) {
		// A single shared password is one guessable secret, so unlimited
		// attempts against it is the entire attack.
		writeError(w, http.StatusTooManyRequests, "too_many_requests",
			"too many attempts; wait a few minutes")
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}

	if !auth.CheckPassword(cur.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "incorrect password")
		return
	}
	s.throttle.Reset(key)

	s.issueSession(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true})
}

// authLogout ends this session only.
func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.CookieName); err == nil && c.Value != "" {
		_ = s.st.DeleteSession(r.Context(), auth.HashToken(c.Value))
	}
	http.SetCookie(w, auth.ClearCookie())
	w.WriteHeader(http.StatusNoContent)
}

// authChangePassword replaces the password and logs every session out,
// including this one. Revoking everything is the point of server-side
// sessions: a changed password that leaves old sessions alive has not
// actually locked anyone out.
func (s *Server) authChangePassword(w http.ResponseWriter, r *http.Request) {
	cur := s.settings.Get()
	if !cur.Secured() {
		writeError(w, http.StatusConflict, "conflict", "no password is set")
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
	if !auth.CheckPassword(cur.PasswordHash, req.Current) {
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

	next := cur
	next.PasswordHash = hash
	if err := s.settings.Set(next); err != nil {
		s.writeInternal(w, err, "save password")
		return
	}
	if err := s.st.DeleteAllSessions(r.Context()); err != nil {
		s.writeInternal(w, err, "revoke sessions")
		return
	}

	http.SetCookie(w, auth.ClearCookie())
	w.WriteHeader(http.StatusNoContent)
}

// issueSession mints a token and sets the cookie.
func (s *Server) issueSession(w http.ResponseWriter, r *http.Request) {
	token, hash, err := auth.NewToken()
	if err != nil {
		s.writeInternal(w, err, "generate session")
		return
	}
	if err := s.st.CreateSession(r.Context(), hash, localUser, auth.SessionTTL); err != nil {
		s.writeInternal(w, err, "create session")
		return
	}
	http.SetCookie(w, auth.Cookie(token, auth.SessionTTL, r.TLS != nil))
}
