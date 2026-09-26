package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"strconv"

	"lancast/internal/auth"
	"lancast/internal/store"
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
	{method: http.MethodGet, pattern: "/api/guest/me"},

	/*
	 * Playing something, and its subtitles, and nothing else about it.
	 *
	 * Each names the segment holding the item, which is what turns an
	 * allow-listed *route* into an allow-listed *object*. ADR 0046 §4 is
	 * explicit that route-level is not enough: /api/stream/{id} streams
	 * whatever id it is handed, so a guest permitted the route is a guest
	 * permitted the library.
	 */
	{method: http.MethodGet, pattern: "/api/stream/{id}", item: "{id}"},
	{method: http.MethodGet, pattern: "/api/stream/{id}/transcode", item: "{id}"},
	{method: http.MethodGet, pattern: "/api/stream/{id}/hls/index.m3u8", item: "{id}"},
	{method: http.MethodGet, pattern: "/api/stream/{id}/hls/{session}/{name}", item: "{id}"},
	{method: http.MethodGet, pattern: "/api/items/{id}/subtitles", item: "{id}"},
	/*
	 * except names sibling literals the router would route elsewhere.
	 *
	 * `{key}` happily matches "search", and /api/items/{id}/subtitles/search
	 * is a different handler that calls OpenSubtitles with the host's own API
	 * key. Go's mux gives a literal segment precedence over a wildcard; this
	 * matcher runs before routing and has to be told.
	 *
	 * Found by the test that enumerates the router rather than by reading, on
	 * its first run after this entry was added.
	 */
	{method: http.MethodGet, pattern: "/api/items/{id}/subtitles/{key}", item: "{id}",
		except: map[string][]string{"{key}": {"search"}}},
}

type guestRoute struct {
	method  string
	pattern string
	/*
	 * item names the pattern segment holding the item id, and a route that
	 * has one **must** declare it.
	 *
	 * Declaring it here rather than checking inside each handler is what makes
	 * the object check impossible to forget. A handler resolves the caller
	 * with userID(), which answers store.LocalUserID for a guest — so a
	 * handler that simply trusted its usual path would apply the *local*
	 * account's ceiling to a stranger from another household and hand the file
	 * over. The check cannot live where it can be omitted.
	 */
	item string
	/*
	 * except lists, per wildcard segment, the values the router would send to
	 * a different handler. A wildcard that matches one of them is not a match
	 * here, because the request will not reach the handler this entry names.
	 *
	 * The enumeration test is what keeps this honest: add a literal sibling
	 * route later and forget to list it here, and the test reports that a
	 * guest can reach a route nobody allow-listed.
	 */
	except map[string][]string
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
func guestMayReach(method, path string) (guestRoute, bool) {
	for _, r := range guestAllowed {
		if r.method == method && r.matches(path) {
			return r, true
		}
	}
	return guestRoute{}, false
}

/*
 * objectID pulls the item id out of a path, using the segment the allow-list
 * entry named. Returns false when the route has no object, or when what is
 * there is not an id.
 */
func (r guestRoute) objectID(path string) (int64, bool) {
	if r.item == "" {
		return 0, false
	}
	p := strings.Split(strings.TrimPrefix(r.pattern, "/"), "/")
	q := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(p) != len(q) {
		return 0, false
	}
	for i := range p {
		if p[i] == r.item {
			id, err := strconv.ParseInt(q[i], 10, 64)
			if err != nil || id <= 0 {
				return 0, false
			}
			return id, true
		}
	}
	return 0, false
}

/*
 * guestMayReachObject answers whether this guest may see one item, now.
 *
 * Resolved per request from the host's own rows rather than from anything the
 * session carries, which is what makes un-sharing and unpairing take effect on
 * the next request with nothing to invalidate (ADR 0071 §6).
 *
 * store.Friend is the principal that fails closed: a peer with no share
 * resolves to a refusal rather than to "no ceiling", which is the opposite of
 * what an account with no row does and the whole reason the two were split.
 */
func (s *Server) guestMayReachObject(r *http.Request, g guestSession, itemID int64) bool {
	ok, err := s.st.MayPlay(r.Context(), store.Friend(g.Peer), itemID)
	return err == nil && ok
}

// matches is segmentsMatch plus this entry's exclusions.
func (r guestRoute) matches(path string) bool {
	if !segmentsMatch(r.pattern, path) {
		return false
	}
	if len(r.except) == 0 {
		return true
	}
	p := strings.Split(strings.TrimPrefix(r.pattern, "/"), "/")
	q := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i := range p {
		for _, bad := range r.except[p[i]] {
			if q[i] == bad {
				return false
			}
		}
	}
	return true
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
