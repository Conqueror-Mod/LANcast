/*
Package guestticket mints and verifies the short-lived credential that admits
somebody from a paired server.

[ADR 0046](../../docs/adr/0046-remote-guests.md) §2 is the specification and
[ADR 0071](../../docs/adr/0071-a-shared-library-is-a-standing-grant.md) §2
reuses it unchanged: Georgia's client asks her own server for a ticket for peer
Chris, her server signs it with its identity key, her client presents it to
Chris's server, and his server verifies it against the key pinned at pairing.
No password crosses and no account is created.

# What a ticket does not say

It names a person and an audience, and nothing about what may be reached. No
library ids, no room id, no permissions.

That is the load-bearing omission. A ticket carrying its own permissions would
be a capability the issuer writes and the host honours, which inverts the trust
direction of everything else here — the host decides what a peer was granted,
from its own rows, at the moment of the request. It is also what makes
revocation work: un-sharing or unpairing changes the host's answer immediately,
while an outstanding ticket that carried permissions would keep asserting them
until it expired.

# Why the encoding is not JSON

The bytes signed must be reconstructible byte-for-byte by the verifier. Two
JSON encoders disagree about key order, escaping and whitespace, so a signature
over "whatever the encoder produced" is a signature over something the other
side cannot re-derive. The encoding here is a length-prefixed concatenation
with one representation per ticket.

It is also domain-separated. The same identity key signs TLS certificates
(ADR 0044 §4), and a signature is only meaningful about the thing it was made
over — without a prefix naming what this is, a signature produced for one
purpose is a signature offered for another.

# Why every refusal looks the same

Verify returns one error for every security-relevant failure. A verifier that
distinguishes a bad signature from an expired ticket from a replayed nonce is
an oracle: it answers questions about a credential the caller should not be
able to ask. The reason is kept for the host's own log, reachable with Reason,
and never travels.
*/
package guestticket

import (
	"crypto"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
	"time"

	"lancast/internal/identity"
)

/*
 * domain separates this signature from every other one the identity key makes.
 *
 * Versioned, because the day the claims change, an old verifier must reject a
 * new ticket rather than read it wrongly — a prefix that never changes turns a
 * format revision into a silent misparse.
 */
const domain = "lancast-guest-ticket/1"

// Field and token bounds. A ticket arrives from the network before anything
// about it is trusted, so every length is checked before it is used to
// allocate or slice.
const (
	maxField = 128
	// A well-formed ticket is a few hundred bytes. The cap is generous enough
	// not to be a limit anybody meets and small enough that a hostile one
	// cannot be a denial of service on its own.
	MaxTokenLen = 2048
)

// ErrInvalid is the only error Verify reports for a ticket it will not accept.
var ErrInvalid = errors.New("invalid ticket")

// invalidError carries why, for the host's log, while presenting the same
// message to everybody. Reason reads it; Error does not.
type invalidError struct{ reason string }

func (e *invalidError) Error() string { return ErrInvalid.Error() }
func (e *invalidError) Is(target error) bool {
	return target == ErrInvalid
}

func refuse(reason string) error { return &invalidError{reason: reason} }

// Reason returns the private detail behind a refusal, for logging on the
// server that refused. Empty for anything that is not a refusal from here.
//
// Never put this in a response. The whole point of the uniform error is that
// the far side learns only that the ticket was not accepted.
func Reason(err error) string {
	var ie *invalidError
	if errors.As(err, &ie) {
		return ie.reason
	}
	return ""
}

/*
 * Claims is what a ticket asserts.
 *
 * Subject is the person's id on the issuing server, which is the same id the
 * host already holds in remote_person — that is what makes it a join key
 * rather than a name to match on. Matching on a display name would break on
 * two people called Sam and would re-point a grant when somebody renamed
 * themselves.
 */
type Claims struct {
	/*
	 * Key is the issuing server's public key, and the issuer is derived from
	 * it rather than stated beside it.
	 *
	 * A fingerprint is SHA-256 of the key, so a fingerprint pinned at pairing
	 * is already a commitment to exactly one key. Carrying the key therefore
	 * discloses nothing and proves everything: the host hashes it, compares
	 * against what it pinned, and verifies with it. Supplying a different key
	 * that hashed the same would be a SHA-256 collision.
	 *
	 * The alternative was recording the key at pairing, which needs a schema
	 * change and has a bootstrap problem — an invite carries only the
	 * fingerprint, so the key is not known until a TLS connection happens, and
	 * a friend could not redeem a ticket before one had.
	 *
	 * Stating the issuer as a separate field was rejected for a smaller
	 * reason: two fields that must agree are two fields that can disagree, and
	 * the disagreement would have to be checked somewhere.
	 */
	Key ed25519.PublicKey
	// Subject is the person, as their own server's account id.
	Subject string
	// Audience is the fingerprint of the server this ticket is for.
	//
	// Not optional, and the reason is the whole of ADR 0046 §2: without it a
	// ticket minted for one peer is replayable against every other peer the
	// issuer has paired with.
	Audience string
	// Nonce is spent on use and remembered until Expires.
	Nonce    string
	IssuedAt time.Time
	Expires  time.Time
}

// Issuer is the fingerprint of the server that signed this, derived from the
// key it carries.
func (c Claims) Issuer() string { return identity.FingerprintOf(c.Key) }

var enc = base64.RawURLEncoding

/*
 * Mint signs a ticket with this server's identity (ADR 0044) and carries its
 * public key, which is the only key a peer has pinned for it.
 *
 * Every field is required. A ticket missing an audience is the replayable one,
 * a ticket missing a nonce cannot be spent, and a ticket with no expiry does
 * not expire — each is a security property rather than a formatting nicety, so
 * none of them defaults.
 */
func Mint(id identity.Identity, c Claims) (string, error) {
	c.Key = id.Public()
	c.Audience = identity.Normalize(c.Audience)

	if err := c.validate(); err != nil {
		return "", err
	}
	payload := c.encode()
	sig, err := id.Signer().Sign(nil, payload, crypto.Hash(0))
	if err != nil {
		return "", err
	}
	return enc.EncodeToString(payload) + "." + enc.EncodeToString(sig), nil
}

func (c Claims) validate() error {
	for _, f := range []struct{ name, v string }{
		{"subject", c.Subject},
		{"audience", c.Audience}, {"nonce", c.Nonce},
	} {
		if f.v == "" {
			return errors.New("guestticket: " + f.name + " is required")
		}
		if len(f.v) > maxField {
			return errors.New("guestticket: " + f.name + " is too long")
		}
	}
	if len(c.Key) != ed25519.PublicKeySize {
		return errors.New("guestticket: the issuing key is missing or the wrong size")
	}
	if c.IssuedAt.IsZero() || c.Expires.IsZero() {
		return errors.New("guestticket: a ticket without times does not expire")
	}
	if !c.Expires.After(c.IssuedAt) {
		return errors.New("guestticket: expiry is not after issue")
	}
	return nil
}

// encode is the canonical form: the domain prefix, then each field as a
// two-byte length and its bytes, then the two times as eight bytes each.
// One representation per ticket, so the verifier signs over exactly what the
// minter did.
func (c Claims) encode() []byte {
	out := make([]byte, 0, len(domain)+ed25519.PublicKeySize+len(c.Subject)+
		len(c.Audience)+len(c.Nonce)+8+8+8)
	out = append(out, domain...)
	// The key is fixed width, so it needs no length prefix and cannot be
	// confused with the fields that follow.
	out = append(out, c.Key...)
	for _, f := range []string{c.Subject, c.Audience, c.Nonce} {
		out = binary.BigEndian.AppendUint16(out, uint16(len(f)))
		out = append(out, f...)
	}
	out = binary.BigEndian.AppendUint64(out, uint64(c.IssuedAt.Unix()))
	out = binary.BigEndian.AppendUint64(out, uint64(c.Expires.Unix()))
	return out
}

// decode reverses encode, refusing anything that does not consume exactly the
// bytes it was given. A trailing byte is a different ticket wearing this one's
// signature.
func decode(b []byte) (Claims, error) {
	var c Claims
	if !strings.HasPrefix(string(b), domain) {
		return c, refuse("wrong domain prefix")
	}
	p := b[len(domain):]

	if len(p) < ed25519.PublicKeySize {
		return c, refuse("truncated key")
	}
	c.Key = ed25519.PublicKey(append([]byte(nil), p[:ed25519.PublicKeySize]...))
	p = p[ed25519.PublicKeySize:]

	fields := make([]string, 0, 3)
	for range 3 {
		if len(p) < 2 {
			return c, refuse("truncated field length")
		}
		n := int(binary.BigEndian.Uint16(p))
		p = p[2:]
		if n > maxField || len(p) < n {
			return c, refuse("field length out of range")
		}
		fields = append(fields, string(p[:n]))
		p = p[n:]
	}
	if len(p) != 16 {
		return c, refuse("times are the wrong size")
	}
	c.Subject, c.Audience, c.Nonce = fields[0], fields[1], fields[2]
	c.IssuedAt = time.Unix(int64(binary.BigEndian.Uint64(p[:8])), 0)
	c.Expires = time.Unix(int64(binary.BigEndian.Uint64(p[8:])), 0)
	return c, nil
}

/*
 * Skew is how far ahead of this server's clock a ticket may claim to have been
 * issued.
 *
 * Two domestic machines are not synchronised, and a minute of drift between
 * them is ordinary. Without an allowance, a peer whose clock runs slightly
 * fast would mint tickets this server reads as not yet valid — a failure that
 * looks like a broken pairing and is a clock.
 *
 * It is deliberately small and named. It widens the window in which a
 * compromised ticket is usable, so it is a cost paid for interoperability
 * rather than a number to raise when something does not work.
 */
const Skew = 60 * time.Second

/*
 * Verify checks a ticket for this server and returns what it asserts.
 *
 * isPaired is how the caller answers "have we paired with this fingerprint".
 * Injecting it keeps this package free of the store and makes the pairing
 * check impossible to skip: there is no verification path around it.
 *
 * It takes a fingerprint rather than returning a key because the key travels
 * in the ticket — see Claims.Key. The caller therefore needs to store nothing
 * beyond what pairing already records.
 *
 * The nonce is **not** checked here. Spending it is the caller's, because it
 * needs state and this does not — see the nonce store. A caller that verifies
 * and forgets to spend has a replayable ticket, which is why Claims.Nonce is
 * returned rather than consumed silently.
 *
 * Order matters only for cost: cheap structural checks precede the signature
 * so that rubbish costs nothing to refuse, and the signature precedes the time
 * checks so that a valid-looking expiry on an unsigned ticket is never read.
 */
func Verify(token, audience string, isPaired func(issuer string) bool, now time.Time) (Claims, error) {
	var zero Claims
	if len(token) > MaxTokenLen {
		return zero, refuse("token too long")
	}
	payloadB64, sigB64, ok := strings.Cut(token, ".")
	if !ok {
		return zero, refuse("not two parts")
	}
	payload, err := enc.DecodeString(payloadB64)
	if err != nil {
		return zero, refuse("payload is not base64")
	}
	sig, err := enc.DecodeString(sigB64)
	if err != nil {
		return zero, refuse("signature is not base64")
	}
	if len(sig) != ed25519.SignatureSize {
		return zero, refuse("signature is the wrong size")
	}

	c, err := decode(payload)
	if err != nil {
		return zero, err
	}

	// Audience before signature: a ticket for somebody else is refused without
	// this server ever looking up a key for it.
	if !strings.EqualFold(identity.Normalize(c.Audience), identity.Normalize(audience)) {
		return zero, refuse("audience is another server")
	}

	/*
	 * The pin. The fingerprint recorded at pairing is SHA-256 of a key, so
	 * asking whether *this* key's fingerprint is one we paired with is exactly
	 * the pinning check — and the key is then the right one to verify with by
	 * construction.
	 */
	if !isPaired(c.Issuer()) {
		return zero, refuse("issuer is not a paired server")
	}
	if !ed25519.Verify(c.Key, payload, sig) {
		return zero, refuse("signature does not verify")
	}

	if now.After(c.Expires) {
		return zero, refuse("expired")
	}
	if c.IssuedAt.After(now.Add(Skew)) {
		return zero, refuse("issued too far in the future")
	}
	return c, nil
}
