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

const (
	sessionCtxKey ctxKey = iota
	// apiKeyCtxKey marks a caller that authenticated with an API key rather
	// than a cookie. It is what adminOnly reads to refuse one (ADR 0061), so
	// the distinction survives all the way to authorization instead of being
	// flattened into "authenticated" at the door.
	apiKeyCtxKey
)

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
		// A caller that went away is not a fault, for the reason writeInternal
		// gives. This logged straight to ERROR instead, which made it the last
		// source of that noise — 162 lines, still arriving after the rest had
		// been demoted. Still fails closed either way.
		if errors.Is(err, context.Canceled) {
			s.log.Debug("count users: caller went away", "error", err)
		} else {
			s.log.Error("count users", "error", err)
		}
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

		/*
		 * A key-authenticated caller is resolved before the CSRF check, because
		 * the check does not apply to it and cannot be made to (ADR 0061).
		 *
		 * CSRF exists because **a browser attaches cookies by itself**: the
		 * attack is a third-party page causing the victim's browser to send a
		 * request carrying the victim's ambient credential. Nothing attaches an
		 * Authorization header by itself — a cross-origin page cannot make the
		 * browser add one — so a request authenticated this way cannot be
		 * forged in the way the check defends against, while a legitimate
		 * browser-based client using a key would be refused on every write.
		 *
		 * The shape that would be wrong is skipping whenever an Authorization
		 * header is *present*: any page could then switch the check off by
		 * adding a meaningless header while the cookie still did the
		 * authenticating. This skips only when the key actually resolved, so
		 * the cookie path keeps both of its defences exactly as they were.
		 */
		keySess, keyID, keyed := s.apiKey(r)

		if !keyed {
			// CSRF: a state-changing request must come from this origin. Paired
			// with SameSite=Strict on the cookie — either alone leaves a gap,
			// and this one also covers non-cookie contexts.
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				if !auth.SameOriginRequest(r) {
					writeError(w, http.StatusForbidden, "forbidden", "cross-origin request refused")
					return
				}
			}
		}

		if isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		if keyed {
			/*
			 * No presence.Seen for a key.
			 *
			 * "Online" is meant to be a fact about a person being here, and a
			 * script polling every minute is not that. Letting a key say
			 * otherwise would make the signal permanently wrong for anybody who
			 * has an integration running.
			 */
			s.st.TouchAPIKey(r.Context(), keyID)
			ctx := context.WithValue(r.Context(), sessionCtxKey, keySess)
			ctx = context.WithValue(ctx, apiKeyCtxKey, true)
			next.ServeHTTP(w, r.WithContext(ctx))
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

/*
 * apiKey resolves an `Authorization: Bearer` credential (ADR 0061).
 *
 * A header rather than a query parameter, because a credential in a URL is a
 * credential in the server log, the browser history, the Referer of the next
 * request, and any proxy in between.
 *
 * A malformed or unknown key returns false rather than an error, and the caller
 * then falls through to the cookie path — so presenting rubbish is exactly as
 * unauthorized as presenting nothing, and never a different message that would
 * tell somebody which of their guesses was closer.
 */
func (s *Server) apiKey(r *http.Request) (*store.Session, string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return nil, "", false
	}
	token := strings.TrimSpace(h[len(prefix):])
	if token == "" {
		return nil, "", false
	}
	sess, id, err := s.st.LookupAPIKey(r.Context(), auth.HashToken(token))
	if err != nil {
		return nil, "", false
	}
	return sess, id, true
}

// authedByKey reports whether the caller authenticated with an API key.
func authedByKey(r *http.Request) bool {
	keyed, _ := r.Context().Value(apiKeyCtxKey).(bool)
	return keyed
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
		/*
		 * An API key never reaches here, whoever owns it (ADR 0061).
		 *
		 * Adding a library is arbitrary filesystem read access at a path the
		 * request chooses — the same sentence that justifies the loopback rule.
		 * A session is bounded by somebody sitting in front of the app: it
		 * expires, and it dies when the password changes. A key is long-lived,
		 * kept in a config file on another machine, used unattended, and
		 * committed by accident. Those are different risk classes.
		 *
		 * Checked after the role rather than before it, so the message a
		 * non-admin gets does not depend on how they authenticated.
		 */
		if sess.Role != store.RoleAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "requires an administrator account")
			return
		}
		if authedByKey(r) {
			writeError(w, http.StatusForbidden, "forbidden",
				"an API key cannot perform administration; sign in instead")
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
		/*
		 * Whether this server can convert anything at all (ADR 0048).
		 *
		 * Reported here because this is the one response every screen already
		 * fetches, and because the absence has to be visible *before* playback
		 * is attempted. A <video> element handed a failed request reports a
		 * bare error with no status, so a client that waits to be told cannot
		 * be told — which is how a household concluded the software could not
		 * play their library rather than that a tool was missing.
		 *
		 * Deliberately not admin-only. The install button is, correctly, but
		 * the *fact* is not a secret and the person most affected is a member
		 * who cannot see Settings at all. Telling them what is wrong costs
		 * nothing and is the difference between "this is broken" and "ask
		 * whoever runs this to install ffmpeg".
		 */
		"can_convert": s.trans.Available(),
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
		/*
		 * InstallMediaTools is the ticked option on the setup form (ADR 0048).
		 *
		 * A pointer so "not sent" and "sent as false" stay different. An older
		 * client, or a scripted setup that predates this, must not be read as
		 * having declined -- it never saw the question. Nil falls back to the
		 * configured default, which is what a headless install sets.
		 *
		 * The fetch is not admin-gated because at this moment there are no
		 * accounts. What stands in for that gate is that this field only arrives
		 * from a form stating what will be downloaded, from where, how large and
		 * under which licence, with the option to untick it. That disclosure is
		 * the consent, which is why this must never default to true when the
		 * field is absent.
		 */
		InstallMediaTools *bool `json:"install_media_tools"`
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

	/*
	 * The media tools, if the box was ticked (ADR 0048).
	 *
	 * After the account exists rather than before: a download that fails must
	 * never cost somebody their setup, and the server is entirely usable
	 * without ffmpeg -- it simply cannot convert.
	 *
	 * Setup does not wait for it. The fetch runs alongside with progress in
	 * Settings, so a playback attempted meanwhile can still fail for want of
	 * tools -- but explainably, which is the difference between "this software
	 * cannot play my files" and "wait a moment".
	 *
	 * Skipped silently when ffmpeg is already there: most Linux and macOS
	 * installs, and every machine where somebody solved this by hand.
	 */
	/*
	 * Absent means no, and that is the whole safety property.
	 *
	 * The proposal needed an environment variable so a scripted or air-gapped
	 * install could refuse a fetch that would otherwise start on its own. A
	 * ticked box on a form removes the need for one: a script that POSTs to
	 * this endpoint simply does not send the field, and nothing is fetched.
	 * The escape hatch stopped being necessary when the trigger stopped being
	 * automatic.
	 */
	toolsStarted := false
	if req.InstallMediaTools != nil && *req.InstallMediaTools && !s.trans.Available() {
		_, toolsStarted = s.beginToolsInstall()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"configured":             true,
		"authenticated":          true,
		"media_tools_installing": toolsStarted,
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

	/*
	 * Throttled with the same counter the login path uses, because this is the
	 * same question asked a different way.
	 *
	 * It verifies `current_password` with bcrypt at cost 12 before it does
	 * anything else. Two things follow from that being unbounded. It is a
	 * **password oracle** for anybody holding a stolen session — they already
	 * have access, but the password is worth more, because it survives having
	 * every session revoked and people reuse it elsewhere. And each attempt is
	 * about a hundred milliseconds of deliberate work, so an unbounded stream
	 * of wrong guesses is a way to spend the machine's cores rather than merely
	 * a way to guess.
	 *
	 * Sharing the login counter rather than keeping a second one: an attacker
	 * who has a session and wants to learn the password should not get a fresh
	 * budget by asking on a different route. A correct answer clears it, below,
	 * exactly as a successful login does.
	 */
	key := auth.ClientKey(r)
	if !s.throttle.Allow(key) {
		writeError(w, http.StatusTooManyRequests, "too_many_requests",
			"too many attempts; wait a few minutes")
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
	// Knowing the current password is the same proof a login gives, so it earns
	// the same clean slate.
	s.throttle.Reset(key)

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
