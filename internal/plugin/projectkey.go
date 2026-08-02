package plugin

import (
	"crypto/ed25519"
	"encoding/hex"
)

// projectPublicKeyHex is the LANcast project's Ed25519 signing key, public half,
// hex-encoded. It is embedded so a first-party bundle verifies against it with no
// external trust root (ADR 0021).
//
// It is EMPTY until the maintainer generates the project keypair
// (`lcplugin keygen`), commits the public half here, and guards the private half
// outside the repo. While empty, no bundle can verify as first_party — a signed
// bundle then either matches an operator-pinned key (pinned) or is rejected;
// unsigned bundles are unaffected. Nothing about the trust model depends on this
// value being present, only the first_party classification does.
const projectPublicKeyHex = ""

// ProjectPublicKey decodes the embedded project key, or nil if none is set. A
// malformed constant returns nil rather than panicking, so a bad paste degrades
// to "no first-party key" instead of crashing the server.
func ProjectPublicKey() ed25519.PublicKey {
	if projectPublicKeyHex == "" {
		return nil
	}
	b, err := hex.DecodeString(projectPublicKeyHex)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil
	}
	return ed25519.PublicKey(b)
}
