package api

import (
	"net"
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
	known, err := s.st.PeerByFingerprint(r.Context(), fingerprint)
	if err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "not a paired server")
		return "", false
	}
	s.notePeerAddress(r, known)
	return fingerprint, true
}

/*
 * notePeerAddress remembers where a peer just connected from
 * ([ADR 0044](../../docs/adr/0044-server-identity-and-peering.md) §5).
 *
 * §5 says the address is a hint and the fingerprint is the identity — *"a peer
 * that moves gets a new address and is still the same peer"* — and then says
 * nothing about how the hint is corrected. It never was: addresses were written
 * once from an invite and never revisited, so a peer that moved became
 * unreachable with no way back except pasting a fresh invite. That is what
 * happened, and it was misdiagnosed for a day as a routing problem.
 *
 * **The connection is the evidence.** It arrived over mutual TLS carrying the
 * identity key recorded at pairing, so this is not somebody claiming an address
 * — it is where an authenticated peer actually is. Nothing weaker would do:
 * an address a caller *asserts* is an address anybody can assert.
 *
 * It only corrects the direction that is already working, and that is the
 * common shape of the fault rather than a limitation: when two servers lose
 * each other it is usually one-way, and the half that still connects is exactly
 * the half that can say where it went.
 *
 * Failures are ignored. Not learning an address costs a pairing nothing it did
 * not already have, and refusing a peer's request because a hint could not be
 * written would be the tail wagging the dog.
 */
func (s *Server) notePeerAddress(r *http.Request, known store.Peer) {
	addr, ok := learnedAddress(r.RemoteAddr, known.Addrs)
	if !ok {
		return
	}
	if err := s.st.LearnPeerAddress(r.Context(), known.Fingerprint, addr); err != nil {
		s.log.Debug("could not record where a peer connected from",
			"peer", known.Fingerprint, "addr", addr, "error", err)
	}
}

/*
 * learnedAddress turns a connection's source into an address worth keeping.
 *
 * Pure, and takes what it needs, because the interesting cases are all about
 * *which* port and they are tedious to arrange over a real socket.
 *
 * # The port is not the one we can see
 *
 * `RemoteAddr` carries the peer's **ephemeral source port**, which is different
 * on every connection and listens for nothing. What is wanted is the port they
 * serve on — so the host comes from the connection and the port comes from what
 * is already recorded for that peer. A peer that changed address but not port,
 * which is every peer that moved network, is then reachable again.
 *
 * With no recorded port there is nothing to guess with, and nothing is learned.
 * Inventing a default would write an address nobody has ever answered on.
 *
 * # Loopback is never learned
 *
 * A connection from this machine says nothing about where another household
 * is, and recording it would have every peer eventually pointing at ourselves.
 */
func learnedAddress(remoteAddr string, known []string) (string, bool) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil || host == "" {
		return "", false
	}
	if ip := net.ParseIP(host); ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
		return "", false
	}
	port := servingPort(known)
	if port == "" {
		return "", false
	}
	addr := net.JoinHostPort(host, port)
	if len(known) > 0 && known[0] == addr {
		// Already the best guess; the store would decline this anyway, and not
		// asking it is one fewer thing happening several times a minute.
		return "", false
	}
	return addr, true
}

// servingPort is the port this peer is believed to serve on, taken from the
// first address recorded for it. They are ordered best-guess first, so this is
// the port most recently known to work.
func servingPort(known []string) string {
	for _, a := range known {
		if _, port, err := net.SplitHostPort(a); err == nil && port != "" {
			return port
		}
	}
	return ""
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

	// The same decision every playback route makes, room membership included.
	allowed, err := s.peerMayPlay(r, peerFP, itemID)
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
