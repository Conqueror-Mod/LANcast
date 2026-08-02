package plugin

import (
	"crypto/ed25519"
	"encoding/hex"
)

// projectPublicKeyHex is the LANcast project's Ed25519 signing key, public half,
// hex-encoded. It is embedded so a first-party bundle verifies against it with no
// external trust root (ADR 0021).
//
// The maintainer generated the project keypair (`lcplugin keygen`) and holds the
// private half outside the repo; only the public half is here. If it were empty,
// no bundle could verify as first_party — a signed bundle would then either match
// an operator-pinned key (pinned) or be rejected, and unsigned bundles are
// unaffected. Nothing about the trust model depends on this value being present,
// only the first_party classification does.
const projectPublicKeyHex = "6f7e685d6dda5eaa300cdc998a55db682c7d7f85bbef39da618ee62276d66d18"

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
