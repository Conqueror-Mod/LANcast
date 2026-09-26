package api

import (
	"net/http"
	"strings"

	"lancast/internal/store"
)

/*
 * CORS, opened as narrowly as it can be and still work
 * ([ADR 0046](../../docs/adr/0046-remote-guests.md) §7).
 *
 * A guest is cross-origin by construction: the page is served by the friend's
 * own server and talks to this one. Without these headers the browser makes
 * the request and then refuses to let the page read the answer, so the feature
 * does not work at all.
 *
 * This is genuinely new exposure — nothing in this codebase has ever sent an
 * Access-Control-Allow-Origin header — so every dimension is narrowed:
 *
 *   - **Never `*`.** Only an origin belonging to a currently paired peer, and
 *     the header echoes that exact origin.
 *   - **Only guest routes.** The allow-list from ADR 0046 §3, plus redemption
 *     itself. A route a guest cannot reach gets no CORS headers, so opening a
 *     route to guests later is still one deliberate edit rather than two.
 *   - **No credentials.** Access-Control-Allow-Credentials is never sent. The
 *     guest credential is a bearer token precisely so that no cookie is
 *     involved (§6), and sending it would ask browsers to attach this server's
 *     cookies to a foreign page's requests — which is the thing SameSite is
 *     there to prevent.
 *   - **Unpairing closes it.** The answer is computed per request from the
 *     peer table, so there is no cached list to invalidate.
 */

// corsMaxAge is how long a browser may cache a preflight. Short: a pairing
// removed while somebody is watching should stop working in minutes, not at
// the end of the day.
const corsMaxAge = "300"

/*
 * pairedOrigin reports whether an Origin belongs to a currently paired peer.
 *
 * Matched on host and port against the addresses recorded at pairing, ignoring
 * the scheme. An address is a hint about where a peer can be reached
 * (ADR 0044 §5) and is recorded as `host:port`; the origin arrives as
 * `https://host:port`.
 *
 * A peer that has moved and not told anybody will not match, and that is the
 * honest failure: this server knows where it was told the peer lives, and an
 * origin it has never been given is not one to trust because it asked nicely.
 */
func (s *Server) pairedOrigin(r *http.Request, origin string) bool {
	if origin == "" || origin == "null" {
		return false
	}
	host := originHost(origin)
	if host == "" {
		return false
	}
	peers, err := s.st.Peers(r.Context())
	if err != nil {
		// Fail closed: a database error is not a reason to widen access.
		return false
	}
	for _, p := range peers {
		if p.State != store.PeerPaired {
			continue
		}
		for _, a := range p.Addrs {
			if strings.EqualFold(a, host) {
				return true
			}
		}
	}
	return false
}

// originHost pulls host[:port] out of an origin, refusing anything with a
// path, query or fragment — an origin has none of those, and accepting one
// would mean matching on a string a page can partly choose.
func originHost(origin string) string {
	s := origin
	i := strings.Index(s, "://")
	if i < 0 {
		return ""
	}
	s = s[i+3:]
	if s == "" || strings.ContainsAny(s, "/?#@") {
		return ""
	}
	return s
}

/*
 * guestCORS adds the headers when, and only when, all of it lines up: an
 * Origin that is a paired peer, asking for a route a guest may reach.
 *
 * Preflight is answered here rather than by a handler because it arrives
 * without the Authorization header — the browser strips it — so the guest
 * middleware cannot recognise it. It is answered with headers and no body,
 * and it authorises nothing: the real request still has to carry a valid
 * token and still passes the allow-list and the object check.
 */
func (s *Server) guestCORS(w http.ResponseWriter, r *http.Request) (handled bool) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}

	// Which method the eventual request will use: for a preflight it is in the
	// request header, otherwise it is this request's own.
	method := r.Method
	if r.Method == http.MethodOptions {
		method = r.Header.Get("Access-Control-Request-Method")
		if method == "" {
			return false
		}
	}

	if !guestReachablePath(method, r.URL.Path) {
		return false
	}
	if !s.pairedOrigin(r, origin) {
		return false
	}

	// Vary, because the answer depends on who asked. Without it a shared cache
	// could hand one origin's permission to another.
	w.Header().Add("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Origin", origin)

	if r.Method == http.MethodOptions {
		w.Header().Add("Vary", "Access-Control-Request-Method")
		w.Header().Set("Access-Control-Allow-Methods", method)
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Max-Age", corsMaxAge)
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

// guestReachablePath is the allow-list plus redemption, which is the set of
// routes CORS may be opened on. Redemption is included because it is where a
// guest session begins and it has no token to present yet.
func guestReachablePath(method, path string) bool {
	if method == http.MethodPost && path == "/api/guest/session" {
		return true
	}
	_, ok := guestMayReach(method, path)
	return ok
}
