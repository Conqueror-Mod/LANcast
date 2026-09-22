package guestticket

import (
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"

	"lancast/internal/identity"
)

// two servers, so "the right key" and "a key" are never the same thing.
type server struct {
	id identity.Identity
	fp string
}

func newServer(t *testing.T) server {
	t.Helper()
	id, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return server{id: id, fp: id.Fingerprint()}
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

// pinned is what the host knows: the fingerprints it recorded at pairing, and
// nothing about anybody it has not paired with.
func (f fixture) pinned(issuer string) bool {
	return strings.EqualFold(issuer, identity.Normalize(f.georgia.fp))
}

func (f fixture) claims() Claims {
	return Claims{
		Subject:  "u_georgia",
		Audience: f.chris.fp,
		Nonce:    "nonce-1",
		IssuedAt: f.now,
		Expires:  f.now.Add(2 * time.Minute),
	}
}

func (f fixture) mint(t *testing.T, c Claims) string {
	t.Helper()
	tok, err := Mint(f.georgia.id, c)
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
				tok, err := Mint(f.mallory.id, f.claims())
				if err != nil {
					t.Fatal(err)
				}
				return tok
			},
			reason: "not a paired server",
		},
		{
			// Georgia's key in the payload so the pin passes, Mallory's
			// signature over it. The substitution attack the key-in-ticket
			// design has to survive: carrying the key must not mean trusting
			// whoever assembled the bytes around it.
			name: "georgia's key, mallory's signature",
			token: func() string {
				c := f.claims()
				c.Key = f.georgia.id.Public()
				payload := c.encode()
				sig := ed25519.Sign(f.mallory.id.Signer().(ed25519.PrivateKey), payload)
				return enc.EncodeToString(payload) + "." + enc.EncodeToString(sig)
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
	c.Key = f.georgia.id.Public()
	payload := append([]byte("lancast-something-else/1"), c.encode()[len(domain):]...)
	sig := ed25519.Sign(f.georgia.id.Signer().(ed25519.PrivateKey), payload)
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
	c.Key = f.georgia.id.Public()
	payload := append(c.encode(), 0x00)
	sig := ed25519.Sign(f.georgia.id.Signer().(ed25519.PrivateKey), payload)
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
			if _, err := Mint(f.georgia.id, cl); err == nil {
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
	want.Key = f.georgia.id.Public()
	want.Audience = identity.Normalize(want.Audience)

	got, err := decode(want.encode())
	if err != nil {
		t.Fatal(err)
	}
	if got.Issuer() != want.Issuer() || got.Subject != want.Subject ||
		got.Audience != want.Audience || got.Nonce != want.Nonce ||
		!got.IssuedAt.Equal(want.IssuedAt) || !got.Expires.Equal(want.Expires) {
		t.Errorf("round trip changed the claims:\n got %+v\nwant %+v", got, want)
	}
}
