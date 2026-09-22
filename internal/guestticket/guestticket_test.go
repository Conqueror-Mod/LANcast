package guestticket

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"lancast/internal/identity"
)

// two servers, so "the right key" and "a key" are never the same thing.
type server struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
	fp   string
}

func newServer(t *testing.T) server {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return server{priv: priv, pub: pub, fp: identity.FingerprintOf(pub)}
}

type fixture struct {
	georgia server // issues
	chris   server // verifies
	mallory server // paired with nobody
	now     time.Time
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	return fixture{
		georgia: newServer(t), chris: newServer(t), mallory: newServer(t),
		now: time.Unix(1_700_000_000, 0),
	}
}

// pinned is what the host knows: the key it recorded at pairing, and nothing
// about anybody it has not paired with.
func (f fixture) pinned(issuer string) (ed25519.PublicKey, bool) {
	if strings.EqualFold(issuer, identity.Normalize(f.georgia.fp)) {
		return f.georgia.pub, true
	}
	return nil, false
}

func (f fixture) claims() Claims {
	return Claims{
		Issuer:   f.georgia.fp,
		Subject:  "u_georgia",
		Audience: f.chris.fp,
		Nonce:    "nonce-1",
		IssuedAt: f.now,
		Expires:  f.now.Add(2 * time.Minute),
	}
}

func (f fixture) mint(t *testing.T, c Claims) string {
	t.Helper()
	tok, err := Mint(f.georgia.priv, c)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	return tok
}

func TestAGoodTicketVerifies(t *testing.T) {
	f := newFixture(t)
	tok := f.mint(t, f.claims())

	got, err := Verify(tok, f.chris.fp, f.pinned, f.now)
	if err != nil {
		t.Fatalf("verify: %v (%s)", err, Reason(err))
	}
	if got.Subject != "u_georgia" {
		t.Errorf("subject = %q, want the person the ticket names", got.Subject)
	}
	if got.Nonce != "nonce-1" {
		t.Errorf("nonce = %q; the caller needs it to spend it", got.Nonce)
	}
}

/*
 * Every refusal, in one table, because the property being tested is that they
 * are all refusals — and that they are all the *same* refusal from outside.
 *
 * The reason is asserted separately and only as a substring, so the tests say
 * which case they mean without pinning wording that is for a log rather than
 * for a contract.
 */
func TestEveryBadTicketIsRefused(t *testing.T) {
	f := newFixture(t)

	cases := []struct {
		name   string
		token  func() string
		at     time.Time
		reason string
	}{
		{
			name: "audience is another server",
			token: func() string {
				c := f.claims()
				c.Audience = f.mallory.fp
				return f.mint(t, c)
			},
			reason: "audience",
		},
		{
			name: "issuer is not paired",
			token: func() string {
				c := f.claims()
				c.Issuer = f.mallory.fp
				tok, err := Mint(f.mallory.priv, c)
				if err != nil {
					t.Fatal(err)
				}
				return tok
			},
			reason: "not a paired server",
		},
		{
			name: "signed by the wrong key",
			token: func() string {
				// Claims say Georgia, signature is Mallory's. The pinned key
				// is Georgia's, so this is the substitution attack.
				c := f.claims()
				tok, err := Mint(f.mallory.priv, c)
				if err != nil {
					t.Fatal(err)
				}
				return tok
			},
			reason: "signature does not verify",
		},
		{
			name:   "expired",
			token:  func() string { return f.mint(t, f.claims()) },
			at:     f.now.Add(10 * time.Minute),
			reason: "expired",
		},
		{
			name: "issued beyond the skew allowance",
			token: func() string {
				c := f.claims()
				c.IssuedAt = f.now.Add(Skew + time.Minute)
				c.Expires = c.IssuedAt.Add(time.Minute)
				return f.mint(t, c)
			},
			reason: "future",
		},
		{
			name:   "not two parts",
			token:  func() string { return "onlyonepart" },
			reason: "two parts",
		},
		{
			name:   "payload is not base64",
			token:  func() string { return "!!!.also-bad" },
			reason: "base64",
		},
		{
			name: "truncated payload",
			token: func() string {
				tok := f.mint(t, f.claims())
				p, s, _ := strings.Cut(tok, ".")
				return p[:len(p)/2] + "." + s
			},
			reason: "",
		},
		{
			name: "a bit flipped in the payload",
			token: func() string {
				tok := f.mint(t, f.claims())
				b := []byte(tok)
				// Late in the payload, so the domain prefix still matches and
				// the signature is what catches it.
				i := strings.IndexByte(tok, '.') - 3
				b[i] ^= 'A' ^ 'B'
				return string(b)
			},
			reason: "",
		},
		{
			name:   "empty",
			token:  func() string { return "" },
			reason: "two parts",
		},
		{
			name: "absurdly long",
			token: func() string {
				return strings.Repeat("A", MaxTokenLen+1) + ".AAAA"
			},
			reason: "too long",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at := c.at
			if at.IsZero() {
				at = f.now
			}
			_, err := Verify(c.token(), f.chris.fp, f.pinned, at)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
			if err.Error() != ErrInvalid.Error() {
				t.Errorf("message = %q, want the same message every refusal "+
					"gives: %q", err.Error(), ErrInvalid.Error())
			}
			if c.reason != "" && !strings.Contains(Reason(err), c.reason) {
				t.Errorf("reason = %q, want it to mention %q", Reason(err), c.reason)
			}
		})
	}
}

/*
 * The domain prefix is what stops a signature made for one purpose being
 * offered for another. The identity key also signs TLS certificates, so a
 * verifier that ignored the prefix would accept any signed blob that happened
 * to parse.
 */
func TestASignatureOverAnotherDomainIsRefused(t *testing.T) {
	f := newFixture(t)
	c := f.claims()

	// The same fields, signed under a different prefix — what a future format
	// revision, or another feature reusing this key, would produce.
	payload := append([]byte("lancast-something-else/1"), c.encode()[len(domain):]...)
	sig := ed25519.Sign(f.georgia.priv, payload)
	tok := enc.EncodeToString(payload) + "." + enc.EncodeToString(sig)

	_, err := Verify(tok, f.chris.fp, f.pinned, f.now)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("accepted a signature made for another purpose: %v", err)
	}
	if !strings.Contains(Reason(err), "domain") {
		t.Errorf("reason = %q, want the domain prefix to be what caught it", Reason(err))
	}
}

// A trailing byte is a different ticket wearing this one's signature, so the
// decoder must consume exactly what it was given.
func TestTrailingBytesAreRefused(t *testing.T) {
	f := newFixture(t)
	c := f.claims()
	payload := append(c.encode(), 0x00)
	sig := ed25519.Sign(f.georgia.priv, payload)
	tok := enc.EncodeToString(payload) + "." + enc.EncodeToString(sig)

	_, err := Verify(tok, f.chris.fp, f.pinned, f.now)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("accepted a payload with a trailing byte: %v", err)
	}
}

// Fingerprints are rendered with separators in some places and not others, so
// a ticket must not be refused for being spelled the way a person reads it.
func TestFingerprintSpellingDoesNotMatter(t *testing.T) {
	f := newFixture(t)
	c := f.claims()
	c.Audience = identity.Group(f.chris.fp)
	tok := f.mint(t, c)

	if _, err := Verify(tok, f.chris.fp, f.pinned, f.now); err != nil {
		t.Errorf("grouped audience refused: %v (%s)", err, Reason(err))
	}
}

/*
 * Mint refuses a ticket that would be unsafe rather than filling in a default.
 *
 * Each of these is a security property: no audience is the replayable ticket,
 * no nonce cannot be spent, and no expiry does not expire. A zero value that
 * quietly became "now" or "never" is how one of them goes missing.
 */
func TestMintRefusesAnUnsafeTicket(t *testing.T) {
	f := newFixture(t)

	for _, c := range []struct {
		name   string
		break_ func(*Claims)
	}{
		{"no audience", func(c *Claims) { c.Audience = "" }},
		{"no nonce", func(c *Claims) { c.Nonce = "" }},
		{"no subject", func(c *Claims) { c.Subject = "" }},
		{"no expiry", func(c *Claims) { c.Expires = time.Time{} }},
		{"expiry before issue", func(c *Claims) { c.Expires = c.IssuedAt.Add(-time.Second) }},
		{"oversized field", func(c *Claims) { c.Subject = strings.Repeat("x", maxField+1) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			cl := f.claims()
			c.break_(&cl)
			if _, err := Mint(f.georgia.priv, cl); err == nil {
				t.Error("minted it anyway")
			}
		})
	}
}

// Round-tripping must be exact, or the verifier is signing over something
// other than what the minter meant.
func TestEncodingRoundTrips(t *testing.T) {
	f := newFixture(t)
	want := f.claims()
	want.Issuer = identity.Normalize(want.Issuer)
	want.Audience = identity.Normalize(want.Audience)

	got, err := decode(want.encode())
	if err != nil {
		t.Fatal(err)
	}
	if got.Issuer != want.Issuer || got.Subject != want.Subject ||
		got.Audience != want.Audience || got.Nonce != want.Nonce ||
		!got.IssuedAt.Equal(want.IssuedAt) || !got.Expires.Equal(want.Expires) {
		t.Errorf("round trip changed the claims:\n got %+v\nwant %+v", got, want)
	}
}
