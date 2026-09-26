package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"lancast/internal/auth"
)

/*
 * What a guest may reach, in one place.
 *
 * [ADR 0046](../../docs/adr/0046-remote-guests.md) §3 calls this the
 * load-bearing decision of the whole feature, and the property it buys is
 * worth stating plainly: **the guest's entire power is readable in one list,
 * and the default for all future code is closed.** A route added next year is
 * refused until somebody deliberately adds it here.
 *
 * Expressing this as a third value of `role` beside `admin` and `member` was
 * considered and rejected in the ADR, for a reason this file exists to make
 * structural: with roles, the *absence* of a check is an allow, and there are
 * hundreds of handlers. Here the absence of an entry is a refusal.
 *
 * # This list is deliberately almost empty
 *
 * It grows one entry at a time, each alongside the object-level check that
 * makes it safe. Allow-listing `/api/items/{id}/stream` without the check that
 * the item is one this guest may see is not a smaller version of the feature;
 * it is the library, handed over. The route list and the object checks land
 * together or not at all.
 */
var guestAllowed = []guestRoute{
	// A session reading what it is. No object to check: it discloses only what
	// the caller already proved by presenting a ticket.
	{http.MethodGet, "/api/guest/me"},
}

type guestRoute struct {
	method  string
	pattern string
}

/*
 * guestMayReach matches a request against the list.
 *
 * A segment-wise match rather than a regex, and `{...}` matches exactly one
 * segment. The router's own patterns are not available here — this middleware
 * runs before routing — so the shapes are written out, and a test asserts each
 * one corresponds to a real route so the list cannot rot into permissions for
 * handlers that no longer exist.
 *
 * Trailing-slash and case differences are not normalised away. A path that
 * does not match exactly is refused, which is the safe direction: a matcher
 * that is generous about spelling is a matcher somebody can spell around.
 */
func guestMayReach(method, path string) bool {
	for _, r := range guestAllowed {
		if r.method == method && segmentsMatch(r.pattern, path) {
			return true
		}
	}
	return false
}

func segmentsMatch(pattern, path string) bool {
	p := strings.Split(strings.TrimPrefix(pattern, "/"), "/")
	q := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(p) != len(q) {
		return false
	}
	for i := range p {
		if strings.HasPrefix(p[i], "{") && strings.HasSuffix(p[i], "}") {
			// One segment, and it must be non-empty: "/api/items//stream"
			// is not a request for item "".
			if q[i] == "" {
				return false
			}
			continue
		}
		if p[i] != q[i] {
			return false
		}
	}
	return true
}

/*
 * guestFromRequest resolves a redeemed session from an Authorization header.
 *
 * A bearer token, never a cookie (ADR 0046 §6). A guest is cross-origin by
 * construction, and a cookie that works cross-origin is a cookie with
 * SameSite=None — which is exactly the property the host's own CSRF defence
 * depends on its cookies *not* having.
 *
 * An unknown or expired token returns false rather than an error, so
 * presenting rubbish is exactly as unauthorized as presenting nothing.
 */
func (s *Server) guestFromRequest(r *http.Request) (guestSession, bool) {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return guestSession{}, false
	}
	token := strings.TrimSpace(h[len(prefix):])
	if token == "" {
		return guestSession{}, false
	}
	return s.guests.lookup(auth.HashToken(token), time.Now())
}

// guestFromContext returns the guest session the middleware resolved, if the
// caller is one. Handlers use it to tell a guest from an account.
func guestFromContext(r *http.Request) (guestSession, bool) {
	g, ok := r.Context().Value(guestCtxKey).(guestSession)
	return g, ok
}

func withGuest(ctx context.Context, g guestSession) context.Context {
	return context.WithValue(ctx, guestCtxKey, g)
}

/*
 * guestMe is the one thing a fresh session may do.
 *
 * It answers with what the caller already proved by presenting a ticket — the
 * issuing server and the person that server named — so it discloses nothing
 * and gives a client something to render while the rest of the surface is
 * built.
 *
 * Deliberately not a list of what is shared. That belongs with the scoped
 * listing handlers and their object-level checks, and answering it here would
 * be the first permission granted without one.
 */
func (s *Server) guestMe(w http.ResponseWriter, r *http.Request) {
	g, ok := guestFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not a guest session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"peer":       g.Peer,
		"subject":    g.Subject,
		"expires_at": g.Expires.Unix(),
	})
}
