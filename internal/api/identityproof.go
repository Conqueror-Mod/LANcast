package api

import (
	"encoding/base64"
	"net/http"

	"lancast/internal/identity"
)

/*
 * GET /api/identity/proof: this server proves it holds its identity key, over
 * the connection the request arrived on (ADR 0124).
 *
 * The desktop client asks this when the serving key at an address it knows
 * has changed. It used to ask a person to compare two copies of the identity
 * it already had, which matched whoever was answering (review finding
 * R11-1). The signature here is over the client's nonce and keying material
 * exported from this TLS session, so it cannot be relayed by somebody in the
 * middle, who necessarily runs a different session each way.
 *
 * No session, because it is asked before the client will send one to the new
 * key -- that is the point. It keeps ADR 0044 section 7's reason for gating
 * GET /api/identity by answering only a caller who names this server's
 * fingerprint already: anybody else gets the same 404 as a route that does
 * not exist, and learns nothing they did not bring.
 */
func (s *Server) identityProof(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	asked := identity.Normalize(q.Get("fingerprint"))
	/*
	 * Over TLS only, and only for our own fingerprint. Both refusals are the
	 * plain 404 so neither says anything: plain HTTP has no connection key to
	 * prove anything about, and a wrong fingerprint is somebody who does not
	 * already know who this is.
	 */
	if r.TLS == nil || asked == "" || asked != s.ident.Fingerprint() {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	nonce, err := base64.RawURLEncoding.DecodeString(q.Get("nonce"))
	if err != nil || len(nonce) < identity.ProofNonceMin || len(nonce) > identity.ProofNonceMax {
		writeError(w, http.StatusBadRequest, "bad_request",
			"nonce must be 16 to 64 bytes, base64url without padding")
		return
	}
	// Refused for a TLS 1.2 session without the extended master secret: the
	// one case where two sessions could share an export. No proof then.
	ekm, err := r.TLS.ExportKeyingMaterial(identity.ProofExporterLabel, nil, identity.ProofEKMSize)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	sig := s.ident.Prove(nonce, ekm)
	if sig == nil {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	// Bound to one connection and one nonce; a cached copy is worthless and
	// a shared cache holding one would be confusing.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"public_key": base64.StdEncoding.EncodeToString(s.ident.Public()),
		"signature":  base64.StdEncoding.EncodeToString(sig),
	})
}
