package release

import (
	"crypto/ed25519"
	"encoding/hex"
)

// releasePublicKeyHex is the LANcast project's release-signing key, public half.
//
// Generated 2026-08-09. The private half lives only in the RELEASE_SIGNING_KEY
// repository secret; it was never committed and the local copy was destroyed
// after the secret was set. *.key is gitignored, the same rule the plugin key
// follows (ADR 0021).
//
// If this is ever empty again — a fork, a build from a tree without it — that is
// a working state, not a broken one, and it means precisely that releases
// cannot be verified so nothing may be installed automatically. A build with no
// key refuses rather than waving downloads through: a verifier that trusts
// everything when it cannot check is worse than no verifier at all.
//
// To rotate: go run ./cmd/lcsign keygen -out release.key, put the printed
// public half here and the private half in the secret. Releases signed by the
// old key stop verifying, which is what rotation means.
//
// Separate from internal/plugin's project key on purpose: plugin provenance and
// release provenance are different trust domains, and sharing one key means a
// compromise of either is a compromise of both.
const releasePublicKeyHex = "059f05a5f92f28445c4d0884bf319f968156010a6b5554c310c785f3046228ae"

// activeKeyHex is what PublicKey actually reads. It starts as the compiled-in
// constant and exists as a variable only so tests can supply a keypair — the
// real constant is empty until a maintainer generates one, and a verifier with
// no key to test against cannot be tested at all.
var activeKeyHex = releasePublicKeyHex

// PublicKey decodes the embedded release key, or nil when none is set or it is
// malformed. Nil is handled by every caller as "cannot verify", which fails
// closed.
func PublicKey() ed25519.PublicKey {
	if activeKeyHex == "" {
		return nil
	}
	b, err := hex.DecodeString(activeKeyHex)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil
	}
	return ed25519.PublicKey(b)
}

// Signable reports whether this build can verify a release at all. The updater
// uses it to explain why automatic installation is unavailable rather than
// silently never offering it.
func Signable() bool { return PublicKey() != nil }
