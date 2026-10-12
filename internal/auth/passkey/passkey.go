// Package passkey verifies WebAuthn registrations and sign-ins (ADR 0082).
//
// Only what LANcast asks for is implemented, and that is deliberately little:
// attestation "none", so nothing here judges which make of authenticator
// somebody owns; user verification required; and three signature algorithms,
// ES256, EdDSA and RS256, which between them cover every platform passkey and
// security key in use. Everything a response carries that LANcast did not ask
// about is ignored rather than half-checked.
//
// Own code over a library (go-webauthn) because the library's weight is almost
// entirely attestation: measured on 2026-10-11 it added ten modules, among them
// TPM, JWT and MessagePack, for a check this package never makes. CBOR is the
// one thing not written here: a hand-made parser for untrusted binary input is
// the attack surface a fuzzed decoder exists to remove.
package passkey

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"

	"github.com/fxamacker/cbor/v2"
)

// COSE algorithm identifiers LANcast accepts, in the order it prefers them.
const (
	AlgES256 = -7
	AlgEdDSA = -8
	AlgRS256 = -257
)

// Algorithms is what registration offers the browser.
var Algorithms = []int{AlgES256, AlgEdDSA, AlgRS256}

// Authenticator data flags (WebAuthn §6.1).
const (
	flagUP = 0x01 // user present
	flagUV = 0x04 // user verified
	flagBE = 0x08 // backup eligible: may be synced
	flagBS = 0x10 // backed up: is synced
	flagAT = 0x40 // attested credential data included
)

/*
 * dec is the one CBOR decoder this package uses, configured for input from a
 * stranger: a map with a repeated key is refused rather than resolved, so two
 * parsers can never read one key differently, and nesting and sizes are
 * bounded well below anything a real authenticator sends.
 */
var dec = func() cbor.DecMode {
	dm, err := cbor.DecOptions{
		DupMapKey:        cbor.DupMapKeyEnforcedAPF,
		MaxNestedLevels:  8,
		MaxArrayElements: 64,
		MaxMapPairs:      64,
		IndefLength:      cbor.IndefLengthForbidden,
	}.DecMode()
	if err != nil {
		panic(err)
	}
	return dm
}()

// ErrInvalid wraps every reason a response is refused. The reasons are for the
// log; a caller answers the browser with one sentence whatever the reason,
// because which check failed is information about how to forge one.
var ErrInvalid = errors.New("passkey response refused")

func refuse(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// Credential is what is stored after a registration.
type Credential struct {
	ID             []byte
	PublicKey      []byte // the COSE key, verbatim, parsed again at each use
	Algorithm      int
	SignCount      uint32
	AAGUID         []byte
	BackupEligible bool
	BackupState    bool
}

// Expect is what a response has to match: the challenge this server issued,
// the RP ID, and the origins allowed to answer for it.
type Expect struct {
	Challenge []byte
	RPID      string
	// Origin reports whether an origin may answer. Callers allow https on the
	// RP ID's own host at any port the server listens on.
	Origin func(origin string) bool
}

// OriginForRPID allows https origins whose host is exactly the RP ID, on any
// port. The port is not a security boundary between two listeners of one
// server, and a passkey made through :443 must work through :8080.
func OriginForRPID(rpID string) func(string) bool {
	return func(origin string) bool {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != "https" || u.Path != "" && u.Path != "/" {
			return false
		}
		return strings.EqualFold(u.Hostname(), rpID)
	}
}

type clientData struct {
	Type        string `json:"type"`
	Challenge   string `json:"challenge"`
	Origin      string `json:"origin"`
	CrossOrigin bool   `json:"crossOrigin"`
}

func checkClientData(raw []byte, wantType string, e Expect) error {
	var cd clientData
	if err := json.Unmarshal(raw, &cd); err != nil {
		return refuse("client data is not JSON")
	}
	if cd.Type != wantType {
		return refuse("client data type %q, want %q", cd.Type, wantType)
	}
	got, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(cd.Challenge, "="))
	if err != nil || len(e.Challenge) == 0 || subtle.ConstantTimeCompare(got, e.Challenge) != 1 {
		return refuse("challenge does not match")
	}
	if e.Origin == nil || !e.Origin(cd.Origin) {
		return refuse("origin %q not allowed", cd.Origin)
	}
	if cd.CrossOrigin {
		return refuse("cross-origin request")
	}
	return nil
}

type authData struct {
	rpIDHash  []byte
	flags     byte
	signCount uint32
	// present only when flagAT is set
	aaguid []byte
	credID []byte
	cose   []byte
}

func parseAuthData(b []byte) (*authData, error) {
	if len(b) < 37 {
		return nil, refuse("authenticator data too short")
	}
	ad := &authData{rpIDHash: b[:32], flags: b[32], signCount: binary.BigEndian.Uint32(b[33:37])}
	if ad.flags&flagAT == 0 {
		return ad, nil
	}
	rest := b[37:]
	if len(rest) < 18 {
		return nil, refuse("attested credential data too short")
	}
	ad.aaguid = rest[:16]
	n := int(binary.BigEndian.Uint16(rest[16:18]))
	rest = rest[18:]
	if n == 0 || n > 1023 || len(rest) < n {
		return nil, refuse("credential id length %d invalid", n)
	}
	ad.credID = rest[:n]
	rest = rest[n:]
	// The COSE key is one CBOR item, followed by extensions if any. Decode
	// exactly one item and keep its bytes, so trailing extension data can
	// never be mistaken for part of the key.
	d := dec.NewDecoder(bytes.NewReader(rest))
	var key cbor.RawMessage
	if err := d.Decode(&key); err != nil {
		return nil, refuse("credential public key is not CBOR")
	}
	ad.cose = append([]byte(nil), key...)
	return ad, nil
}

func checkAuthData(ad *authData, rpID string) error {
	want := sha256.Sum256([]byte(rpID))
	if subtle.ConstantTimeCompare(ad.rpIDHash, want[:]) != 1 {
		return refuse("RP ID hash does not match %q", rpID)
	}
	if ad.flags&flagUP == 0 {
		return refuse("user not present")
	}
	if ad.flags&flagUV == 0 {
		return refuse("user not verified")
	}
	return nil
}

type attestationObject struct {
	Fmt      string          `cbor:"fmt"`
	AttStmt  cbor.RawMessage `cbor:"attStmt"`
	AuthData []byte          `cbor:"authData"`
}

// VerifyRegistration checks a navigator.credentials.create() response and
// returns the credential to store.
//
// The attestation statement is not examined: LANcast asks for "none" and does
// not decide which authenticators are good enough. The format is not checked
// either, because a browser may answer "none" with any format and that is not
// a reason to refuse somebody's passkey.
func VerifyRegistration(e Expect, clientDataJSON, attestation []byte) (*Credential, error) {
	if err := checkClientData(clientDataJSON, "webauthn.create", e); err != nil {
		return nil, err
	}
	var ao attestationObject
	if err := dec.Unmarshal(attestation, &ao); err != nil {
		return nil, refuse("attestation object is not CBOR")
	}
	ad, err := parseAuthData(ao.AuthData)
	if err != nil {
		return nil, err
	}
	if err := checkAuthData(ad, e.RPID); err != nil {
		return nil, err
	}
	if ad.flags&flagAT == 0 {
		return nil, refuse("no credential in a registration")
	}
	// Parsed now so a key LANcast could never verify against is refused at
	// registration, not discovered at the first sign-in.
	_, alg, err := parseCOSEKey(ad.cose)
	if err != nil {
		return nil, err
	}
	return &Credential{
		ID:             append([]byte(nil), ad.credID...),
		PublicKey:      ad.cose,
		Algorithm:      alg,
		SignCount:      ad.signCount,
		AAGUID:         append([]byte(nil), ad.aaguid...),
		BackupEligible: ad.flags&flagBE != 0,
		BackupState:    ad.flags&flagBS != 0,
	}, nil
}

// Assertion is what a sign-in proved.
type Assertion struct {
	SignCount   uint32
	BackupState bool
}

// VerifyAssertion checks a navigator.credentials.get() response against a
// stored credential's public key.
//
// It does not judge the signature counter; the caller owns the stored value
// and decides (ADR 0082 §4).
func VerifyAssertion(e Expect, publicKey []byte, clientDataJSON, authenticatorData, signature []byte) (*Assertion, error) {
	if err := checkClientData(clientDataJSON, "webauthn.get", e); err != nil {
		return nil, err
	}
	ad, err := parseAuthData(authenticatorData)
	if err != nil {
		return nil, err
	}
	if err := checkAuthData(ad, e.RPID); err != nil {
		return nil, err
	}
	pub, alg, err := parseCOSEKey(publicKey)
	if err != nil {
		return nil, err
	}
	cdHash := sha256.Sum256(clientDataJSON)
	signed := make([]byte, 0, len(authenticatorData)+len(cdHash))
	signed = append(append(signed, authenticatorData...), cdHash[:]...)
	if err := verifySignature(pub, alg, signed, signature); err != nil {
		return nil, err
	}
	return &Assertion{SignCount: ad.signCount, BackupState: ad.flags&flagBS != 0}, nil
}

// coseKey holds the COSE_Key members LANcast reads (RFC 9053).
type coseKey struct {
	Kty int             `cbor:"1,keyasint"`
	Alg int             `cbor:"3,keyasint"`
	Crv cbor.RawMessage `cbor:"-1,keyasint"` // an int for EC2/OKP, the modulus for RSA
	X   []byte          `cbor:"-2,keyasint"` // x for EC2/OKP, the exponent for RSA
	Y   []byte          `cbor:"-3,keyasint"`
}

func parseCOSEKey(b []byte) (crypto.PublicKey, int, error) {
	var k coseKey
	if err := dec.Unmarshal(b, &k); err != nil {
		return nil, 0, refuse("public key is not a COSE key")
	}
	switch {
	case k.Kty == 2 && k.Alg == AlgES256:
		var crv int
		if err := dec.Unmarshal(k.Crv, &crv); err != nil || crv != 1 {
			return nil, 0, refuse("ES256 key not on P-256")
		}
		if len(k.X) != 32 || len(k.Y) != 32 {
			return nil, 0, refuse("ES256 coordinates are not 32 bytes")
		}
		point := append(append([]byte{4}, k.X...), k.Y...)
		pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), point)
		if err != nil {
			return nil, 0, refuse("ES256 key is not a point on P-256")
		}
		return pub, AlgES256, nil
	case k.Kty == 1 && k.Alg == AlgEdDSA:
		var crv int
		if err := dec.Unmarshal(k.Crv, &crv); err != nil || crv != 6 {
			return nil, 0, refuse("EdDSA key is not Ed25519")
		}
		if len(k.X) != ed25519.PublicKeySize {
			return nil, 0, refuse("Ed25519 key is not 32 bytes")
		}
		return ed25519.PublicKey(append([]byte(nil), k.X...)), AlgEdDSA, nil
	case k.Kty == 3 && k.Alg == AlgRS256:
		var n []byte
		if err := dec.Unmarshal(k.Crv, &n); err != nil {
			return nil, 0, refuse("RSA modulus missing")
		}
		mod := new(big.Int).SetBytes(n)
		if mod.BitLen() < 2048 {
			return nil, 0, refuse("RSA key shorter than 2048 bits")
		}
		e := new(big.Int).SetBytes(k.X)
		if !e.IsInt64() || e.Int64() < 3 || e.Int64() > 1<<31-1 || e.Bit(0) == 0 {
			return nil, 0, refuse("RSA exponent invalid")
		}
		return &rsa.PublicKey{N: mod, E: int(e.Int64())}, AlgRS256, nil
	}
	return nil, 0, refuse("key type %d with algorithm %d is not accepted", k.Kty, k.Alg)
}

func verifySignature(pub crypto.PublicKey, alg int, signed, sig []byte) error {
	switch alg {
	case AlgES256:
		h := sha256.Sum256(signed)
		if ecdsa.VerifyASN1(pub.(*ecdsa.PublicKey), h[:], sig) {
			return nil
		}
	case AlgEdDSA:
		if ed25519.Verify(pub.(ed25519.PublicKey), signed, sig) {
			return nil
		}
	case AlgRS256:
		h := sha256.Sum256(signed)
		if rsa.VerifyPKCS1v15(pub.(*rsa.PublicKey), crypto.SHA256, h[:], sig) == nil {
			return nil
		}
	}
	return refuse("signature does not verify")
}
