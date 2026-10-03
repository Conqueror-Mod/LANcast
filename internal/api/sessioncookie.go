package api

import (
	"net/http"
	"time"

	"lancast/internal/auth"
)

/*
 * The session cookie keeps pace with the session.
 *
 * A session stays valid for SessionTTL after its last use: every
 * authenticated request extends it server-side (session, TouchSession), so an
 * active viewer is never logged out mid-film. The cookie carrying it was set
 * once, at login, with MaxAge of the same 30 days, and never again. The
 * browser kept counting down from that one moment however active the person
 * was, so somebody using LANcast every day was signed out exactly thirty days
 * after they last typed their password, while the server still held a
 * session valid for another month.
 *
 * Found on a real install: a session created 2026-09-02 11:50 and in use every
 * day was last seen 2026-10-02 11:50, to the minute, with its server-side
 * expiry already pushed out to November.
 *
 * So the cookie is re-sent, with a fresh MaxAge, as the session is used. Not
 * on every response: once per cookieRefresh per session is enough to keep it
 * well clear of expiring, and it keeps a header off the hundreds of artwork
 * and progress requests a page makes. When it was last sent lives in memory;
 * after a restart the first authenticated request sends it once more, which
 * costs one header.
 */
const cookieRefresh = 6 * time.Hour

// keepCookie re-sends the caller's session cookie if it has not been sent in
// the last cookieRefresh. The caller has already established that the
// session is valid.
func (s *Server) keepCookie(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(auth.CookieName)
	if err != nil || c.Value == "" {
		return
	}
	key := auth.HashToken(c.Value)
	now := time.Now()

	s.cookieMu.Lock()
	if last, ok := s.cookieSent[key]; ok && now.Sub(last) < cookieRefresh {
		s.cookieMu.Unlock()
		return
	}
	s.cookieSent[key] = now
	// Bounded by the sessions in use; entries for sessions long gone are
	// dropped whenever the map is walked for a new one.
	if len(s.cookieSent) > 256 {
		for k, t := range s.cookieSent {
			if now.Sub(t) > auth.SessionTTL {
				delete(s.cookieSent, k)
			}
		}
	}
	s.cookieMu.Unlock()

	http.SetCookie(w, auth.Cookie(c.Value, auth.SessionTTL, r.TLS != nil))
}

// cookieIssued records that a session's cookie was just set, at login or
// setup, so the next request does not send it again straight away.
func (s *Server) cookieIssued(token string) {
	s.cookieMu.Lock()
	s.cookieSent[auth.HashToken(token)] = time.Now()
	s.cookieMu.Unlock()
}
