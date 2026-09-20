package knownserver

import (
	"strings"
	"testing"
	"time"
)

/*
 * Addresses a person might actually type.
 *
 * The point of every case here is that it must reach the *same record* as the
 * one that was pinned. An address that parses to something new is not a
 * cosmetic fault: it is a second entry with no pin, which asks somebody to
 * trust a machine they are already trusting -- and a trust prompt that appears
 * when it should not is how people learn to click through trust prompts.
 */
func TestParseAddressCanonicalises(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"a bare host takes the default port", "media-pc", "media-pc:8080"},
		{"a bare IPv4 takes the default port", "192.168.1.66", "192.168.1.66:8080"},
		{"host and port are kept", "192.168.1.66:8080", "192.168.1.66:8080"},
		{"another port is kept", "192.168.1.66:9000", "192.168.1.66:9000"},
		{"an https URL is an address", "https://192.168.1.66:8080", "192.168.1.66:8080"},
		{"a trailing slash is not part of it", "https://192.168.1.66:8080/", "192.168.1.66:8080"},
		{"a pasted deep link is still an address",
			"https://192.168.1.66:8080/movies?sort=added", "192.168.1.66:8080"},
		{"http is accepted too", "http://media-pc:8080", "media-pc:8080"},
		{"surrounding space is not part of it", "  192.168.1.66  ", "192.168.1.66:8080"},
		{"case in a hostname does not matter", "Media-PC", "media-pc:8080"},
		{"a bracketed IPv6 literal keeps its brackets", "[fe80::1]:8080", "[fe80::1]:8080"},
		{"a bare IPv6 literal gains brackets and the default port",
			"fe80::1", "[fe80::1]:8080"},
		{"an IPv6 URL is an address", "https://[fe80::1]:9000", "[fe80::1]:9000"},
		{"loopback is not special here", "127.0.0.1:8080", "127.0.0.1:8080"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAddress(tc.in)
			if err != nil {
				t.Fatalf("ParseAddress(%q) = error %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseAddress(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The forms that must be refused, each for its own reason.
func TestParseAddressRefuses(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"nothing typed yet", ""},
		{"only space", "   "},
		{"a scheme we do not speak", "ftp://media-pc"},
		{"a file URL", "file:///C:/media"},
		{"a port that is not a number", "media-pc:eighty"},
		{"a port out of range", "media-pc:70000"},
		{"a port of zero", "media-pc:0"},
		{"a negative port", "media-pc:-1"},
		{"credentials, which we would otherwise drop in silence", "https://me:pw@media-pc:8080"},
		{"two hosts", "media-pc other-pc:8080"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAddress(tc.in)
			if err == nil {
				t.Fatalf("ParseAddress(%q) = %q, want an error", tc.in, got)
			}
		})
	}
}

// The three outcomes, which is the whole decision.
func TestCheck(t *testing.T) {
	const pin = "3Xk0aBcD1efGh2IjKlMn3OpQrStUvWxYz0123456789="
	const other = "ZZZZaBcD1efGh2IjKlMn3OpQrStUvWxYz0123456789="

	l := List{}.Accept(Server{Address: "192.168.1.66:8080", Name: "Chris", Pin: pin})

	t.Run("an address never seen is a question, not a failure", func(t *testing.T) {
		if got := l.Check("192.168.1.99:8080", pin); got != TrustUnknown {
			t.Errorf("Check = %v, want unknown", got)
		}
	})

	t.Run("the key that was accepted connects", func(t *testing.T) {
		if got := l.Check("192.168.1.66:8080", pin); got != TrustMatch {
			t.Errorf("Check = %v, want match", got)
		}
	})

	// Refused because this record carries no identity to appeal to. With one,
	// the same change is a rotation to confirm -- see rotation_test.go, which
	// is where the interesting half of this rule lives.
	t.Run("a different key with nothing to appeal to is refused", func(t *testing.T) {
		if got := l.Check("192.168.1.66:8080", other); got != TrustMismatch {
			t.Errorf("Check = %v, want mismatch", got)
		}
	})

	t.Run("no key offered at a known address is not a match", func(t *testing.T) {
		// A server that answered without TLS where a pin was accepted is the
		// same fault as the wrong key: whatever is there is not what was
		// trusted.
		if got := l.Check("192.168.1.66:8080", ""); got != TrustMismatch {
			t.Errorf("Check = %v, want mismatch", got)
		}
	})

	t.Run("a record with no pin is forgettable, not permanently broken", func(t *testing.T) {
		broken := List{Servers: []Server{{Address: "media-pc:8080"}}}
		if got := broken.Check("media-pc:8080", pin); got != TrustUnknown {
			t.Errorf("Check = %v, want unknown", got)
		}
	})
}

/*
 * The same key is the same server, whatever the certificate around it says.
 *
 * The pin is over the SubjectPublicKeyInfo rather than the whole certificate,
 * so a certificate reissued with new dates over the same key still matches.
 * That is worth holding, and it is *all* this proves.
 *
 * It is deliberately no longer offered as the reason a mismatch means an
 * attack. It does not: a serving certificate is regenerated whenever its file
 * is missing or corrupt, and deleting cert and key is this project's own
 * documented repair for stale SANs. The identity is what carries that weight
 * now -- rotation_test.go.
 */
func TestTheSameKeyInANewCertificateStillMatches(t *testing.T) {
	const key = "3Xk0aBcD1efGh2IjKlMn3OpQrStUvWxYz0123456789="
	l := List{}.Accept(Server{Address: "media-pc:8080", Pin: key})

	if got := l.Check("media-pc:8080", key); got != TrustMatch {
		t.Fatalf("Check = %v, want match", got)
	}
}

func TestAcceptReplacesAndKeepsTheName(t *testing.T) {
	l := List{}.Accept(Server{Address: "media-pc:8080", Name: "Chris", Pin: "one"})
	l = l.Accept(Server{Address: "media-pc:8080", Pin: "two"})

	if len(l.Servers) != 1 {
		t.Fatalf("%d servers, want 1 — re-accepting must replace, not append", len(l.Servers))
	}
	if l.Servers[0].Pin != "two" {
		t.Errorf("pin = %q, want the new one", l.Servers[0].Pin)
	}
	if l.Servers[0].Name != "Chris" {
		t.Errorf("name = %q, want the name already given to be kept", l.Servers[0].Name)
	}
}

func TestAcceptStampsTheTime(t *testing.T) {
	before := time.Now()
	l := List{}.Accept(Server{Address: "media-pc:8080", Pin: "one"})
	if l.Servers[0].Accepted.Before(before) {
		t.Errorf("accepted = %v, want a time from this call", l.Servers[0].Accepted)
	}
}

func TestForgetIsTheWayBack(t *testing.T) {
	const pin = "one"
	l := List{}.Accept(Server{Address: "media-pc:8080", Pin: pin})
	l = l.Accept(Server{Address: "other-pc:8080", Pin: "two"})

	l = l.Forget("media-pc:8080")

	if _, ok := l.Find("media-pc:8080"); ok {
		t.Error("the forgotten server is still there")
	}
	if _, ok := l.Find("other-pc:8080"); !ok {
		t.Error("forgetting one server removed another")
	}
	// And a forgotten server is askable again, which is the point of it.
	if got := l.Check("media-pc:8080", "anything"); got != TrustUnknown {
		t.Errorf("Check = %v, want unknown after forgetting", got)
	}
}

// A fingerprint is read aloud by one person to another. It has to survive that.
func TestFingerprintIsReadable(t *testing.T) {
	got := Fingerprint("3Xk0aBcD1efGh2IjKlMn")
	want := "3Xk0aBcD 1efGh2Ij KlMn"
	if got != want {
		t.Errorf("Fingerprint = %q, want %q", got, want)
	}
	if strings.ReplaceAll(got, " ", "") != "3Xk0aBcD1efGh2IjKlMn" {
		t.Error("grouping changed the pin itself")
	}
}
