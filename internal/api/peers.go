package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"

	"lancast/internal/identity"
	"lancast/internal/peer"
	"lancast/internal/store"
	"lancast/internal/tlscert"
)

/*
 * Peers: the other servers this one has been introduced to.
 *
 * Phase 2 of the federation plan, ADR 0044. Pairing and nothing else — a peer
 * here is two servers knowing who each other are, which by ADR 0046 permits
 * exactly nothing on its own.
 *
 * # Why these are admin and the People page is not
 *
 * Pairing lives in Settings and granting lives in People, which is a product
 * decision and turns out to settle the authorization cleanly. Adding a peer
 * opens a network relationship **for the whole server** and is the same class
 * of act as adding a library: it is an operational power over the machine, so
 * it is admin-gated here on the server rather than merely hidden in the client
 * (ADR 0015).
 *
 * Granting a named person something is not. That is one account's own decision
 * about its own viewing (ADR 0035), it is self-service, and it never appears in
 * this file. So a member never lists peers — they list *people*, which is a
 * different route reading a different table, and that division is the two-place
 * design expressed as permissions.
 *
 * The one exception is peer visibility, which is a personal decision that
 * happens to be about federation: an account choosing whether to appear in the
 * roster this server hands its peers. Self-service, like sharing, and
 * deliberately not something an administrator can set on somebody's behalf —
 * a switch an admin can flip is not consent.
 */

// peerJSON is the shape a client sees. The fingerprint is sent in both forms
// for the same reason /api/identity does: one is compared, the other is read,
// and a client inventing its own grouping is how two screens end up disagreeing
// about whether a fingerprint matched.
func peerJSON(p store.Peer) map[string]any {
	return map[string]any{
		"fingerprint":         p.Fingerprint,
		"fingerprint_display": identity.Group(p.Fingerprint),
		"name":                p.Name,
		"state":               p.State,
		"addrs":               p.Addrs,
		"added_at":            p.AddedAt,
		"last_seen":           p.LastSeen,
	}
}

func (s *Server) listPeers(w http.ResponseWriter, r *http.Request) {
	peers, err := s.st.Peers(r.Context())
	if err != nil {
		s.writeInternal(w, err, "list peers")
		return
	}
	out := make([]map[string]any, 0, len(peers))
	for _, p := range peers {
		out = append(out, peerJSON(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"peers": out})
}

/*
 * ourInvite builds the string this server's operator hands to somebody else.
 *
 * The addresses are enumerated from the machine's own interfaces, which is a
 * guess and is documented as one: LANcast cannot know which of its addresses a
 * particular peer can reach, because that is a fact about the network the
 * operator built. Sending several and letting the far end try them in order is
 * the honest version of not knowing — and it is why ADR 0044 §5 makes the
 * address a hint rather than part of the identity.
 *
 * A server nothing else can reach cannot introduce itself, and says so rather
 * than producing an invite with no usable address in it. That is the same
 * boundary that gates LAN binding and TLS: loopback-only means nobody else is
 * talking to this machine.
 */
func (s *Server) ourInvite(w http.ResponseWriter, r *http.Request) {
	_, port, err := net.SplitHostPort(s.listenAddr)
	if err != nil || port == "" {
		s.writeInternal(w, err, "read the listen address")
		return
	}

	ips := tlscert.LocalIPs()
	addrs := make([]string, 0, len(ips))
	for _, ip := range ips {
		addrs = append(addrs, net.JoinHostPort(ip, port))
	}

	invite, err := peer.Encode(s.ident, identityName(), addrs)
	if errors.Is(err, peer.ErrNoAddress) {
		writeError(w, http.StatusConflict, "not_reachable",
			"this server has no address another machine could reach it on, so it cannot introduce itself")
		return
	}
	if err != nil {
		s.writeInternal(w, err, "build an invite")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"invite":              invite,
		"fingerprint":         s.ident.Fingerprint(),
		"fingerprint_display": s.ident.Grouped(),
		"name":                identityName(),
		"addrs":               addrs,
	})
}

/*
 * addPeer accepts an invite somebody pasted.
 *
 * This records an introduction and does not complete a pairing: ADR 0044 §3
 * makes pairing mutual, and the peer stays in `added` until the other side is
 * confirmed to hold us too — which only the transport can find out. So this
 * route cannot, by construction, create a relationship on its own.
 */
func (s *Server) addPeer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Invite string `json:"invite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}

	in, err := peer.Parse(req.Invite)
	if err != nil {
		// The parser's messages are written for the person holding the paste,
		// so they are passed through rather than flattened into "bad request".
		// "That invite was made by a newer version of LANcast" is actionable;
		// "invalid" is not.
		writeError(w, http.StatusBadRequest, "bad_invite", err.Error())
		return
	}

	// Pasting your own invite is a mistake somebody will make while testing,
	// and it would otherwise produce a peer that is this server: a row that
	// looks reachable, answers its own reachability check, and appears in its
	// own peer list.
	if in.Fingerprint == s.ident.Fingerprint() {
		writeError(w, http.StatusBadRequest, "self",
			"that is this server's own invite")
		return
	}

	if err := s.st.AddPeer(r.Context(), store.Peer{
		Fingerprint: in.Fingerprint, Name: in.Name, Addrs: in.Addrs,
	}); err != nil {
		s.writeInternal(w, err, "add peer")
		return
	}

	// Pairing is an administrative act on the server, which is exactly what the
	// audit log is for (ADR 0026). The fingerprint is recorded rather than the
	// invite: the invite is a transport for it, and one of them is the identity.
	s.audit(r, "peer.add", "peer", in.Fingerprint,
		"Added peer "+in.Name, map[string]any{"name": in.Name, "addrs": in.Addrs})

	p, err := s.st.PeerByFingerprint(r.Context(), in.Fingerprint)
	if err != nil {
		s.writeInternal(w, err, "add peer")
		return
	}
	writeJSON(w, http.StatusCreated, peerJSON(p))
}

/*
 * removePeer un-pairs.
 *
 * This is the revocation mechanism the whole feature rests on (ADR 0046): the
 * pinned key goes, no ticket verifies, and everything the pairing carried —
 * addresses, the roster, and in later phases every grant naming one of those
 * people — goes with it through the schema's cascade. One act, complete, with
 * nothing per-person left to forget.
 */
func (s *Server) removePeer(w http.ResponseWriter, r *http.Request) {
	// Normalized, so a fingerprint copied from a screen in its grouped form
	// removes the peer somebody is looking at rather than reporting that no
	// such peer exists.
	fp := identity.Normalize(r.PathValue("fingerprint"))

	p, err := s.st.PeerByFingerprint(r.Context(), fp)
	if s.notFoundOr(w, err, "get peer", "no such peer") {
		return
	}
	if err := s.st.RemovePeer(r.Context(), fp); err != nil {
		s.writeInternal(w, err, "remove peer")
		return
	}
	s.audit(r, "peer.remove", "peer", fp, "Removed peer "+p.Name, nil)
	w.WriteHeader(http.StatusNoContent)
}

/*
 * putPeerVisibility records one account's own decision to appear in the roster
 * this server hands its peers.
 *
 * Session-gated rather than admin-gated, and it always writes the *caller's*
 * row — there is no user id in the request, so there is no shape of this call
 * that sets somebody else's. That is the same construction share_activity uses,
 * for the reason ADR 0035 gives: a switch an administrator can flip is not
 * consent.
 */
func (s *Server) putPeerVisibility(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
		return
	}
	var req struct {
		Visible bool `json:"visible"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	if err := s.st.SetVisibleToPeers(r.Context(), sess.UserID, req.Visible); err != nil {
		s.writeInternal(w, err, "set peer visibility")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"visible": req.Visible})
}
