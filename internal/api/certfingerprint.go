package api

import (
	"os"

	"lancast/internal/certpin"
)

/*
 * The fingerprint of the certificate this server serves.
 *
 * It exists so that trust on first use can actually be performed. A desktop
 * client meeting a server for the first time shows the key it was offered and
 * asks whether it is right ([ADR 0070]); that question is only answerable if
 * the server can be asked the same thing **somewhere else** -- on its own
 * screen, by somebody already on it, read out over the phone. Without that,
 * the prompt is a formality and the honest description of the feature is
 * "accept whatever answers".
 *
 * Which is also why reading it through the connection being verified proves
 * nothing, and why this is not treated as a secret. It is a hash of a public
 * key that the server hands to anybody who opens a TLS connection to it. The
 * protection it offers comes entirely from being compared *out of band*, so
 * hiding it would cost the feature its point and buy nothing.
 *
 * [ADR 0070]: ../../docs/adr/0070-the-desktop-client-can-trust-a-server-it-did-not-install.md
 */
func (s *Server) certFingerprint() string {
	cur := s.settings.Get()

	// A supplied certificate lives where the operator put it; the generated
	// one lives under the data directory. Same question, two places, and the
	// server must answer about the certificate it is actually serving rather
	// than the one it would have generated.
	if cur.CustomTLS() {
		pemBytes, err := os.ReadFile(cur.TLSCertFile)
		if err != nil {
			return ""
		}
		pin, err := certpin.SPKIFromPEM(pemBytes)
		if err != nil {
			return ""
		}
		return pin
	}

	// Empty rather than an error when there is none. A loopback-only server
	// serves plain HTTP and has no certificate at all, which is an ordinary
	// state and not a fault -- and it is exactly the server nobody needs to
	// verify, because nothing can reach it from another machine.
	pin, err := certpin.SPKI(s.dataDir)
	if err != nil {
		return ""
	}
	return pin
}
