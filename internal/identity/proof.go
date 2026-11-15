package identity

import (
	"crypto/ed25519"
	"crypto/subtle"
	"errors"
)

/*
 * A proof that this server holds its identity key, bound to one TLS
 * connection (ADR 0124).
 *
 * The message is fixed here, in one place, because both ends build it: the
 * server signs it in the API and the desktop client verifies it in
 * knownserver. Two copies of a byte layout are how a proof that should pass
 * quietly fails, or one that should fail quietly passes.
 *
 * What makes it a proof about *this connection* is the exported keying
 * material (RFC 5705, RFC 8446 section 7.5). Both ends of one TLS session
 * derive the same bytes and nobody else can, so somebody in the middle -- who
 * necessarily runs two sessions -- cannot pass the real server's signature on.
 */

const (
	// ProofExporterLabel is the TLS exporter label. RFC 5705 reserves the
	// "EXPORTER" prefix for exactly this kind of use.
	ProofExporterLabel = "EXPORTER-LANcast-identity-proof"
	// ProofEKMSize is how many bytes of keying material are exported.
	ProofEKMSize = 32
	// ProofNonceMin and ProofNonceMax bound the client's nonce. Below the
	// minimum a nonce could repeat; above the maximum is somebody using the
	// endpoint for something else.
	ProofNonceMin = 16
	ProofNonceMax = 64

	// proofDomain separates this signature from everything else this key
	// signs: guest tickets start "lancast-guest-ticket/1", a certificate is
	// DER and starts 0x30, and a TLS 1.3 handshake signature starts with 64
	// spaces. The zero byte ends the prefix, so no nonce can extend it.
	proofDomain = "LANcast identity proof v1\x00"
)

// ErrProof is a proof that does not hold: the key is not the identity asked
// about, or the signature is not over this connection.
var ErrProof = errors.New("the server could not prove its identity")

// ProofMessage is the exact bytes signed and verified.
func ProofMessage(nonce, ekm []byte) []byte {
	out := make([]byte, 0, len(proofDomain)+len(nonce)+len(ekm))
	out = append(out, proofDomain...)
	out = append(out, nonce...)
	return append(out, ekm...)
}

// Prove signs a challenge for one connection. Nil from an identity with no
// key, so a server that somehow has none answers nothing rather than panics.
func (i Identity) Prove(nonce, ekm []byte) []byte {
	if len(i.priv) != ed25519.PrivateKeySize {
		return nil
	}
	return ed25519.Sign(i.priv, ProofMessage(nonce, ekm))
}

/*
 * VerifyProof checks a proof against the identity the caller already holds.
 *
 * Two questions, and both must be yes: is this public key the one that
 * fingerprint names, and did it sign this nonce on this connection. The first
 * is what stops a server proving it holds *a* key; the second is what stops it
 * replaying somebody else's proof.
 */
func VerifyProof(fingerprint string, pub, nonce, ekm, sig []byte) error {
	if len(pub) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize {
		return ErrProof
	}
	want := Normalize(fingerprint)
	got := FingerprintOf(ed25519.PublicKey(pub))
	if want == "" || subtle.ConstantTimeCompare([]byte(want), []byte(got)) != 1 {
		return ErrProof
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), ProofMessage(nonce, ekm), sig) {
		return ErrProof
	}
	return nil
}
