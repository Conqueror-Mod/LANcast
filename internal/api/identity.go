package api

import (
	"net/http"
	"os"
)

/*
 * Who this server is (ADR 0044).
 *
 * Phase 1 of the federation plan, and deliberately the whole of it: this route
 * reports an identity and grants nothing. There is no peer, no pairing and no
 * access decision behind it yet. Pairing (Phase 2) is what turns a fingerprint
 * into a relationship, and even then a pairing on its own permits nothing —
 * ADR 0046 is where a remote party gets to do anything at all.
 *
 * Session-gated, like everything else that is not the login form. It says a
 * LANcast is here and what it is called, which is not information worth handing
 * to an unauthenticated caller: the fingerprint travels to the people who need
 * it out of band, in an invite, not by being fetched from the network. That is
 * the same reasoning that makes the whole introduction out-of-band in the first
 * place — a route anybody could read would be a directory of one.
 */

// identityName is what a peer will see this server called.
//
// The machine's hostname, because there is no server name setting and inventing
// one here would be a schema decision made by a route. It becomes editable in
// Phase 2, where the name actually travels — inside an invite, next to the
// fingerprint — and where "which of these two machines is Georgia's" is a
// question somebody is really asking. Until then the hostname is both accurate
// and the only answer available.
//
// A machine that will not say its own name is not an error worth failing a
// request over; the fingerprint is the part that identifies anything.
func identityName() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "LANcast"
}

func (s *Server) identity(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		// Both forms, and both are the same value.
		//
		// `fingerprint` is canonical: 52 base32 characters, and what any
		// comparison is made against. `fingerprint_display` is the grouped
		// rendering, so a client shows the readable form without inventing its
		// own grouping — a second opinion about where the separators go is how
		// two screens end up disagreeing about whether a fingerprint matched.
		"fingerprint":         s.ident.Fingerprint(),
		"fingerprint_display": s.ident.Grouped(),
		"name":                identityName(),
	})
}
