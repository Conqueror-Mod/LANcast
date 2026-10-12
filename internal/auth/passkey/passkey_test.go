package passkey

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

const rpID = "example.dynv6.net"

/*
 * authenticator plays the other side of the ceremony: it holds a key, builds
 * authenticator data and client data the way a browser and an authenticator
 * do, and signs. Every test below is this authenticator talking to the real
 * verifier, then one field at a time made wrong.
 */
type authenticator struct {
	alg    int
	signer crypto.Signer
	credID []byte
	count  uint32
	flags  byte
}

func newAuthenticator(t *testing.T, alg int) *authenticator {
	t.Helper()
	var s crypto.Signer
	var err error
	switch alg {
	case AlgES256:
		s, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case AlgEdDSA:
		_, s, err = ed25519.GenerateKey(rand.Reader)
	case AlgRS256:
		s, err = rsa.GenerateKey(rand.Reader, 2048)
	}
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	rand.Read(id)
	return &authenticator{alg: alg, signer: s, credID: id, flags: flagUP | flagUV | flagBE | flagBS}
}

func (a *authenticator) coseKey(t *testing.T) []byte {
	t.Helper()
	var m map[int]any
	switch pub := a.signer.Public().(type) {
	case *ecdsa.PublicKey:
		b, _ := pub.Bytes()
		m = map[int]any{1: 2, 3: AlgES256, -1: 1, -2: b[1:33], -3: b[33:]}
	case ed25519.PublicKey:
		m = map[int]any{1: 1, 3: AlgEdDSA, -1: 6, -2: []byte(pub)}
	case *rsa.PublicKey:
		m = map[int]any{1: 3, 3: AlgRS256, -1: pub.N.Bytes(), -2: []byte{1, 0, 1}}
	}
	b, err := cbor.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (a *authenticator) authData(rp string, attested []byte) []byte {
	h := sha256.Sum256([]byte(rp))
	b := append([]byte{}, h[:]...)
	flags := a.flags
	if attested != nil {
		flags |= flagAT
	}
	b = append(b, flags)
	b = binary.BigEndian.AppendUint32(b, a.count)
	return append(b, attested...)
}

func clientJSON(typ string, challenge []byte, origin string) []byte {
	b, _ := json.Marshal(map[string]any{"type": typ,
		"challenge": base64.RawURLEncoding.EncodeToString(challenge), "origin": origin})
	return b
}

func (a *authenticator) register(t *testing.T, challenge []byte, origin, rp string) (cd, att []byte) {
	t.Helper()
	attested := make([]byte, 16) // AAGUID of zeros, as "none" attestation often sends
	attested = binary.BigEndian.AppendUint16(attested, uint16(len(a.credID)))
	attested = append(append(attested, a.credID...), a.coseKey(t)...)
	att, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{},
		"authData": a.authData(rp, attested)})
	if err != nil {
		t.Fatal(err)
	}
	return clientJSON("webauthn.create", challenge, origin), att
}

func (a *authenticator) sign(t *testing.T, challenge []byte, origin, rp string) (cd, ad, sig []byte) {
	t.Helper()
	a.count++
	cd = clientJSON("webauthn.get", challenge, origin)
	ad = a.authData(rp, nil)
	h := sha256.Sum256(cd)
	msg := append(append([]byte{}, ad...), h[:]...)
	var err error
	switch a.alg {
	case AlgEdDSA:
		sig, err = a.signer.Sign(rand.Reader, msg, crypto.Hash(0))
	default:
		d := sha256.Sum256(msg)
		sig, err = a.signer.Sign(rand.Reader, d[:], crypto.SHA256)
	}
	if err != nil {
		t.Fatal(err)
	}
	return cd, ad, sig
}

func expect(ch []byte) Expect {
	return Expect{Challenge: ch, RPID: rpID, Origin: OriginForRPID(rpID)}
}

func challenge() []byte {
	b := make([]byte, 32)
	rand.Read(b)
	return b
}

const origin = "https://example.dynv6.net"

func TestRoundTripEveryAlgorithm(t *testing.T) {
	for name, alg := range map[string]int{"ES256": AlgES256, "EdDSA": AlgEdDSA, "RS256": AlgRS256} {
		t.Run(name, func(t *testing.T) {
			a := newAuthenticator(t, alg)
			ch := challenge()
			cd, att := a.register(t, ch, origin, rpID)
			cred, err := VerifyRegistration(expect(ch), cd, att)
			if err != nil {
				t.Fatalf("registration refused: %v", err)
			}
			if cred.Algorithm != alg || string(cred.ID) != string(a.credID) || !cred.BackupEligible {
				t.Fatalf("credential %+v", cred)
			}

			ch = challenge()
			cd, ad, sig := a.sign(t, ch, origin, rpID)
			got, err := VerifyAssertion(expect(ch), cred.PublicKey, cd, ad, sig)
			if err != nil {
				t.Fatalf("sign-in refused: %v", err)
			}
			if got.SignCount != a.count {
				t.Fatalf("sign count %d, want %d", got.SignCount, a.count)
			}
		})
	}
}

// A passkey made through one port must sign in through the other; the RP ID
// has no port and neither listener is more trusted than the other.
func TestAnyPortOnTheRPIDIsTheSameOrigin(t *testing.T) {
	ok := OriginForRPID(rpID)
	for _, o := range []string{origin, origin + ":8080", "https://EXAMPLE.dynv6.net"} {
		if !ok(o) {
			t.Errorf("%s refused", o)
		}
	}
	for _, o := range []string{"http://example.dynv6.net", "https://evil.example", "https://example.dynv6.net.evil.example",
		"https://sub.example.dynv6.net", "https://192.168.1.20:8080", "null", ""} {
		if ok(o) {
			t.Errorf("%s accepted", o)
		}
	}
}

func refused(t *testing.T, err error, why string) {
	t.Helper()
	if err == nil {
		t.Fatalf("accepted a response with %s", why)
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("%s: error %v does not wrap ErrInvalid", why, err)
	}
}

func TestRegistrationRefusals(t *testing.T) {
	a := newAuthenticator(t, AlgES256)
	ch := challenge()

	cd, att := a.register(t, challenge(), origin, rpID)
	_, err := VerifyRegistration(expect(ch), cd, att)
	refused(t, err, "another challenge")

	cd, att = a.register(t, ch, "https://evil.example", rpID)
	_, err = VerifyRegistration(expect(ch), cd, att)
	refused(t, err, "a foreign origin")

	cd, att = a.register(t, ch, "https://evil.example", "evil.example")
	_, err = VerifyRegistration(expect(ch), cd, att)
	refused(t, err, "a foreign RP ID")

	cd, att = a.register(t, ch, origin, "evil.example")
	_, err = VerifyRegistration(expect(ch), cd, att)
	refused(t, err, "the right origin but another RP ID hash")

	cd, att = a.register(t, ch, origin, rpID)
	_, err = VerifyRegistration(expect(ch), clientJSON("webauthn.get", ch, origin), att)
	refused(t, err, "a sign-in's client data")
	_ = cd

	a.flags = flagUP // not verified: LANcast requires user verification
	cd, att = a.register(t, ch, origin, rpID)
	_, err = VerifyRegistration(expect(ch), cd, att)
	refused(t, err, "no user verification")

	a.flags = flagUV // verified but not present
	cd, att = a.register(t, ch, origin, rpID)
	_, err = VerifyRegistration(expect(ch), cd, att)
	refused(t, err, "no user presence")

	_, err = VerifyRegistration(Expect{RPID: rpID, Origin: OriginForRPID(rpID)}, cd, att)
	refused(t, err, "no challenge issued at all")

	var crossOrigin map[string]any
	json.Unmarshal(clientJSON("webauthn.create", ch, origin), &crossOrigin)
	crossOrigin["crossOrigin"] = true
	cdx, _ := json.Marshal(crossOrigin)
	a.flags = flagUP | flagUV
	_, att = a.register(t, ch, origin, rpID)
	_, err = VerifyRegistration(expect(ch), cdx, att)
	refused(t, err, "a cross-origin frame")

	_, err = VerifyRegistration(expect(ch), clientJSON("webauthn.create", ch, origin), []byte{0xff, 0x00})
	refused(t, err, "an attestation object that is not CBOR")
}

// Keys LANcast could never verify are refused at registration, not discovered
// at the first sign-in.
func TestUnacceptableKeysAreRefusedAtRegistration(t *testing.T) {
	a := newAuthenticator(t, AlgES256)
	ch := challenge()
	pub, _ := a.signer.Public().(*ecdsa.PublicKey).Bytes()
	for name, key := range map[string]map[int]any{
		"a point off the curve": {1: 2, 3: AlgES256, -1: 1, -2: pub[1:33], -3: make([]byte, 32)},
		"P-384 under ES256":     {1: 2, 3: AlgES256, -1: 2, -2: pub[1:33], -3: pub[33:]},
		"an unsupported alg":    {1: 2, 3: -35, -1: 1, -2: pub[1:33], -3: pub[33:]},
		"a short RSA modulus":   {1: 3, 3: AlgRS256, -1: make([]byte, 128), -2: []byte{1, 0, 1}},
		"a short Ed25519 key":   {1: 1, 3: AlgEdDSA, -1: 6, -2: make([]byte, 31)},
		"Ed448 under EdDSA":     {1: 1, 3: AlgEdDSA, -1: 7, -2: make([]byte, 57)},
	} {
		cose, _ := cbor.Marshal(key)
		attested := make([]byte, 16)
		attested = binary.BigEndian.AppendUint16(attested, uint16(len(a.credID)))
		attested = append(append(attested, a.credID...), cose...)
		att, _ := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{},
			"authData": a.authData(rpID, attested)})
		_, err := VerifyRegistration(expect(ch), clientJSON("webauthn.create", ch, origin), att)
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// A CBOR map with a key given twice is refused, so two parsers can never
// disagree about which value counts. Built from a registration that is
// accepted as it stands, so the duplicate is the only thing wrong with it.
func TestDuplicateCBORKeysAreRefused(t *testing.T) {
	a := newAuthenticator(t, AlgES256)
	ch := challenge()
	cd, att := a.register(t, ch, origin, rpID)
	if _, err := VerifyRegistration(expect(ch), cd, att); err != nil {
		t.Fatalf("control: the untouched registration was refused: %v", err)
	}
	if att[0] != 0xa3 {
		t.Fatalf("fixture: expected a 3-pair map header, got %#x", att[0])
	}
	dupPair, _ := cbor.Marshal("fmt")
	val, _ := cbor.Marshal("packed")
	dup := append([]byte{0xa4}, att[1:]...)
	dup = append(append(dup, dupPair...), val...)
	_, err := VerifyRegistration(expect(ch), cd, dup)
	refused(t, err, "a duplicated map key")
}

func TestSignInRefusals(t *testing.T) {
	a := newAuthenticator(t, AlgES256)
	ch := challenge()
	cd, att := a.register(t, ch, origin, rpID)
	cred, err := VerifyRegistration(expect(ch), cd, att)
	if err != nil {
		t.Fatal(err)
	}

	ch = challenge()
	cd, ad, sig := a.sign(t, ch, origin, rpID)

	_, err = VerifyAssertion(expect(challenge()), cred.PublicKey, cd, ad, sig)
	refused(t, err, "a replayed or foreign challenge")

	bad := append([]byte{}, sig...)
	bad[len(bad)-1] ^= 1
	_, err = VerifyAssertion(expect(ch), cred.PublicKey, cd, ad, bad)
	refused(t, err, "a corrupted signature")

	adFlipped := append([]byte{}, ad...)
	adFlipped[33] ^= 0xff // the counter, covered by the signature
	_, err = VerifyAssertion(expect(ch), cred.PublicKey, cd, adFlipped, sig)
	refused(t, err, "authenticator data altered after signing")

	other := newAuthenticator(t, AlgES256)
	cd2, ad2, sig2 := other.sign(t, ch, origin, rpID)
	_, err = VerifyAssertion(expect(ch), cred.PublicKey, cd2, ad2, sig2)
	refused(t, err, "another key's signature")

	cd3, ad3, sig3 := a.sign(t, ch, "https://evil.example", rpID)
	_, err = VerifyAssertion(expect(ch), cred.PublicKey, cd3, ad3, sig3)
	refused(t, err, "a phishing origin, even with a valid signature")

	_, err = VerifyAssertion(expect(ch), cred.PublicKey, clientJSON("webauthn.create", ch, origin), ad, sig)
	refused(t, err, "a registration's client data")

	_, err = VerifyAssertion(expect(ch), cred.PublicKey, cd, ad[:20], sig)
	refused(t, err, "truncated authenticator data")
}

// The challenge may arrive padded; base64url without padding is what the spec
// says, and padding is tolerated rather than a reason to refuse.
func TestPaddedChallengeIsTolerated(t *testing.T) {
	a := newAuthenticator(t, AlgEdDSA)
	ch := challenge()[:31] // 31 bytes encodes with padding
	cd, att := a.register(t, ch, origin, rpID)
	var m map[string]any
	json.Unmarshal(cd, &m)
	m["challenge"] = base64.URLEncoding.EncodeToString(ch)
	if !strings.HasSuffix(m["challenge"].(string), "=") {
		t.Fatal("fixture: expected a padded encoding")
	}
	cd, _ = json.Marshal(m)
	if _, err := VerifyRegistration(expect(ch), cd, att); err != nil {
		t.Fatalf("padded challenge refused: %v", err)
	}
}
