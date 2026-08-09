package release

import (
	"crypto/ed25519"
	"encoding/hex"
)

// releasePublicKeyHex is the LANcast project's release-signing key, public half.
//
// EMPTY UNTIL THE MAINTAINER GENERATES ONE. That is a working state, not a
// broken one, and what it means is precise: releases cannot be verified, so
// nothing may be installed automatically. A build with no key refuses rather
// than waving downloads through — an unverifiable update is exactly the one
// worth refusing, and a verifier that trusts everything when it cannot check is
// worse than no verifier at all.
//
// To set it up:
//
//	go run ./cmd/lcsign keygen -out release.key
//
// Put the printed public half here and the private half in the repository
// secret RELEASE_SIGNING_KEY. The private key never enters the repo — *.key is
// gitignored, the same rule the plugin key follows (ADR 0021).
//
// Separate from internal/plugin's project key on purpose: plugin provenance and
// release provenance are different trust domains, and sharing one key means a
// compromise of either is a compromise of both.
const releasePublicKeyHex = ""

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
