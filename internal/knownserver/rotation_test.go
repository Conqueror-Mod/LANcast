package knownserver

import "testing"

/*
 * The correction to ADR 0070, stated as the cases that were wrong before it.
 *
 * The original rule treated any change in the TLS serving key as evidence the
 * server had been replaced. ADR 0044 had already rejected pinning a serving
 * certificate as an identity, because that certificate is *designed* to
 * regenerate: tlscert treats a missing or corrupt file as a cache miss, and the
 * bring-your-own-certificate path exists so an operator can rotate one.
 *
 * This project has a third instance of it, and it is the one that would have
 * been met first: the documented repair for a certificate whose SANs predate a
 * new network interface is to delete the certificate and key and restart. Under
 * the original rule, performing that repair told the other household that
 * something on the network was impersonating the server.
 */

const (
	oldPin   = "AaAaAaAa1111BbBbBbBb2222CcCcCcCc3333DdDdDdD="
	newPin   = "ZzZzZzZz9999YyYyYyYy8888XxXxXxXx7777WwWwWwW="
	identity = "AEJE-4G6X-EG33-AUPJ-U7LG-STT4-N74U-6D4B"
	other    = "BBBB-4G6X-EG33-AUPJ-U7LG-STT4-N74U-6D4B"
)

func known() List {
	l := List{}.Accept(Server{Address: "media-pc:8080", Name: "Chris", Pin: oldPin})
	return l.RecordIdentity("media-pc:8080", identity)
}

/*
 * The case the old rule got wrong, and the reason for all of this.
 *
 * A regenerated certificate at a server whose identity is known is a rotation.
 * It is still confirmed -- it is not proof of a rotation -- but it must not be
 * the sentence reserved for an attacker, because spending that warning on
 * maintenance is how people learn to click past it.
 */
func TestARegeneratedCertificateIsARotationAndNotAnAttack(t *testing.T) {
	got := known().Check("media-pc:8080", newPin)

	if got == TrustMismatch {
		t.Fatal("a new serving key at a known identity was called an attack; " +
			"deleting cert+key is this project's own documented repair")
	}
	if got != TrustRotated {
		t.Errorf("Check = %v, want rotated", got)
	}
}

// Without an identity the two explanations really are indistinguishable, so
// the refusal stands. This is the honest half of the old behaviour.
func TestAChangedKeyWithNoIdentityOnRecordIsStillRefused(t *testing.T) {
	l := List{}.Accept(Server{Address: "media-pc:8080", Pin: oldPin})

	if got := l.Check("media-pc:8080", newPin); got != TrustMismatch {
		t.Errorf("Check = %v, want mismatch", got)
	}
}

// The unchanged case must not have been disturbed by any of this.
func TestTheSameServingKeyStillJustConnects(t *testing.T) {
	if got := known().Check("media-pc:8080", oldPin); got != TrustMatch {
		t.Errorf("Check = %v, want match", got)
	}
}

/*
 * A confirmed rotation is recorded, and the identity survives it.
 *
 * If accepting a rotation dropped the identity, the next rotation would be a
 * refusal and the anchor would have been spent on one use.
 */
func TestAcceptingARotationKeepsTheIdentityAndTheName(t *testing.T) {
	l := known().AcceptRotation("media-pc:8080", newPin)

	s, ok := l.Find("media-pc:8080")
	if !ok {
		t.Fatal("the server is gone")
	}
	if s.Pin != newPin {
		t.Errorf("pin = %q, want the new one", s.Pin)
	}
	if s.Identity != identity {
		t.Errorf("identity = %q, want it kept", s.Identity)
	}
	if s.Name != "Chris" {
		t.Errorf("name = %q, want it kept", s.Name)
	}
	if got := l.Check("media-pc:8080", newPin); got != TrustMatch {
		t.Errorf("after accepting, Check = %v, want match", got)
	}
}

// The identity is the strong refusal, and it is not softened by anything.
func TestADifferentIdentityIsRefused(t *testing.T) {
	if got := known().CheckIdentity("media-pc:8080", other); got != TrustMismatch {
		t.Errorf("CheckIdentity = %v, want mismatch", got)
	}
}

func TestTheSameIdentityIsAMatch(t *testing.T) {
	if got := known().CheckIdentity("media-pc:8080", identity); got != TrustMatch {
		t.Errorf("CheckIdentity = %v, want match", got)
	}
}

// One fingerprint, two spellings. A screen groups it and a person typing it
// may not, and neither is a different server.
func TestIdentityComparisonIgnoresGroupingAndCase(t *testing.T) {
	spellings := []string{
		"aeje-4g6x-eg33-aupj-u7lg-stt4-n74u-6d4b",
		"AEJE4G6XEG33AUPJU7LGSTT4N74U6D4B",
		"AEJE 4G6X EG33 AUPJ U7LG STT4 N74U 6D4B",
	}
	for _, spelling := range spellings {
		if got := known().CheckIdentity("media-pc:8080", spelling); got != TrustMatch {
			t.Errorf("CheckIdentity(%q) = %v, want match", spelling, got)
		}
	}
}

/*
 * RecordIdentity never overwrites.
 *
 * It is called after signing in, which means it is called against whatever
 * server actually answered. If it replaced a stored identity, an impostor that
 * got as far as a session would rewrite the anchor and every later check would
 * agree with it -- turning the strongest refusal in the package into a
 * formality.
 */
func TestRecordIdentityDoesNotOverwriteOne(t *testing.T) {
	l := known().RecordIdentity("media-pc:8080", other)

	s, _ := l.Find("media-pc:8080")
	if s.Identity != identity {
		t.Errorf("identity = %q, want the first one kept", s.Identity)
	}
	if got := l.CheckIdentity("media-pc:8080", other); got != TrustMismatch {
		t.Error("an overwritten identity would have made this a match")
	}
}

// Recording an empty identity is a no-op rather than a way to clear one.
func TestRecordingNothingChangesNothing(t *testing.T) {
	l := known().RecordIdentity("media-pc:8080", "")
	s, _ := l.Find("media-pc:8080")
	if s.Identity != identity {
		t.Errorf("identity = %q, want it untouched", s.Identity)
	}
}

// A server never signed in to has no identity, and that is ordinary: the route
// that reports one is session-gated by ADR 0044.
func TestAnUnvisitedServerHasNoIdentityYet(t *testing.T) {
	l := List{}.Accept(Server{Address: "media-pc:8080", Pin: oldPin})
	if got := l.CheckIdentity("media-pc:8080", identity); got != TrustUnknown {
		t.Errorf("CheckIdentity = %v, want unknown", got)
	}
}
