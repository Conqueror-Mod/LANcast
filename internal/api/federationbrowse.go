package api

import (
	"net/http"
	"strconv"

	"lancast/internal/peer"
	"lancast/internal/store"
)

/*
 * Browsing a shared library, asked for by the friend's own server.
 *
 * [ADR 0071's amendment](../../docs/adr/0071-a-shared-library-is-a-standing-grant.md)
 * decided this: a friend's client cannot reach this server at all, because a
 * window pins one server's key and a self-signed certificate from anybody else
 * is refused. So the friend's client asks *its* server, and its server asks
 * this one over the mutual-TLS peer channel.
 *
 * # What authenticates this, and what it does not say
 *
 * The pin, not a session — the caller proved which **server** it is by
 * presenting the identity key recorded at pairing (ADR 0044 §4), exactly as
 * [presence](presence.go) does.
 *
 * **No person is named, and none is needed.** A share is granted to a *server*
 * (ADR 0071 §1), so what may be seen is the same answer for everybody on it.
 * Presence takes a `person` because its grants are per-person; taking one here
 * would be asking a question whose answer could not change anything, and a
 * parameter that cannot matter is one somebody will later assume does.
 *
 * # Why these are GETs
 *
 * State-changing methods go through the CSRF check in requireAuth, which
 * compares an Origin a peer has no reason to send. Reading keeps this out of a
 * defence built for browsers rather than loosening that defence to let a peer
 * through — the same reasoning federationPresence gives.
 */

// federationPeer resolves the calling server from its client certificate, and
// refuses anything this server has not paired with.
//
// An unknown key reaches a lookup and a refusal, never a disclosure, so
// unpairing stops answering immediately — the property ADR 0044 was built for.
func (s *Server) federationPeer(w http.ResponseWriter, r *http.Request) (string, bool) {
	fingerprint, err := peer.FingerprintFromState(r.TLS)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized",
			"peer connections must present their identity")
		return "", false
	}
	if _, err := s.st.PeerByFingerprint(r.Context(), fingerprint); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "not a paired server")
		return "", false
	}
	return fingerprint, true
}

// federationLibraries answers which of this server's libraries the calling
// peer has been granted.
func (s *Server) federationLibraries(w http.ResponseWriter, r *http.Request) {
	fingerprint, ok := s.federationPeer(w, r)
	if !ok {
		return
	}
	s.writeSharedLibraries(w, r, fingerprint)
}

/*
 * federationItems browses or searches one shared library for the calling peer.
 *
 * The same implementation the ticket path uses, deliberately: the two ways in
 * authenticate differently and authorise identically, and a second copy of the
 * scoping would be a second chance to get it wrong.
 */
func (s *Server) federationItems(w http.ResponseWriter, r *http.Request) {
	fingerprint, ok := s.federationPeer(w, r)
	if !ok {
		return
	}
	s.writeSharedItems(w, r, fingerprint)
}

/*
 * federationStream serves one item's file to a paired server.
 *
 * The friend's server fetches it and passes it to their client, which cannot
 * reach this one (ADR 0071's amendment). That hop costs the friend's local
 * network and not the link between the two houses — the correction the
 * amendment records.
 *
 * Range requests pass straight through, which is what makes seeking work at
 * the far end: their server is a pipe, not a buffer, and a viewer dragging the
 * scrubber produces the same partial requests here that a local one would.
 */
func (s *Server) federationStream(w http.ResponseWriter, r *http.Request) {
	fingerprint, ok := s.federationPeer(w, r)
	if !ok {
		return
	}
	s.writeSharedStream(w, r, fingerprint)
}

/*
 * writeSharedStream is the one place a peer's request becomes a file, and it
 * answers the permission question with the same call the browse path uses.
 *
 * store.MayPlay with a Friend principal resolves the share and its ceiling and
 * **fails closed** — a library that is not shared, an item above the limit, or
 * a question that could not be answered are all refusals. That is the opposite
 * of what the account path does with an unknown id, and the whole reason the
 * two were split (ADR 0071 §6).
 *
 * A refusal is 404, indistinguishable from an item that does not exist, so
 * this cannot be used to learn what the host holds.
 */
func (s *Server) writeSharedStream(w http.ResponseWriter, r *http.Request, peerFP string) {
	itemID, err := strconv.ParseInt(r.URL.Query().Get("item"), 10, 64)
	if err != nil || itemID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "which item")
		return
	}

	allowed, err := s.st.MayPlay(r.Context(), store.Friend(peerFP), itemID)
	if err != nil || !allowed {
		writeError(w, http.StatusNotFound, "not_found", "no such item")
		return
	}

	/*
	 * Read with no account. The ceiling that matters was applied above by the
	 * Friend principal; passing an account id here would ask this server to
	 * apply *its own* household's limit to somebody else's, which is the
	 * confusion Principal exists to make impossible.
	 */
	it, err := s.st.GetItem(r.Context(), itemID, "")
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such item")
		return
	}
	s.serveItemFile(w, r, it)
}
