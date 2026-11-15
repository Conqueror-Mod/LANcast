package main

import (
	"strings"
	"testing"
	"time"

	"lancast/internal/knownserver"
)

// The page has to carry its reason, because the first thing it must do is say
// why it is on screen.
func TestPickerPageCarriesItsReason(t *testing.T) {
	page := pickerPage("The server you were using is no longer one you have accepted.")

	if strings.Contains(page, "__REASON__") {
		t.Error("the placeholder survived; the page would show it literally")
	}
	if !strings.Contains(page, "no longer one you have accepted") {
		t.Error("the reason is not in the page")
	}
}

// Opened from inside the app there is no reason, and the page must not show a
// gap where one would be.
func TestPickerPageWithNoReasonIsStillWholeAndSubstituted(t *testing.T) {
	page := pickerPage("")
	if strings.Contains(page, "__REASON__") {
		t.Error("the placeholder survived")
	}
	if !strings.Contains(page, "lancastServers") {
		t.Error("the page lost its script")
	}
}

/*
 * A reason is built by this program, but it can carry an address, and an
 * address is whatever somebody typed. Escaping it is two lines; not escaping
 * it would let a pasted string close a tag on the one screen whose job is to
 * be trusted.
 */
func TestPickerPageEscapesItsReason(t *testing.T) {
	page := pickerPage(`</p><script>alert('x')</script>`)

	if strings.Contains(page, "<script>alert") {
		t.Error("a reason was substituted as markup")
	}
	if !strings.Contains(page, "&lt;/p&gt;") {
		t.Error("the reason was not escaped")
	}
}

// Two bindings with one name is a mistake that would otherwise be settled by
// map iteration order, which is to say differently on different runs.
func TestMergingBindingsRefusesACollision(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a duplicate binding name was accepted")
		}
	}()
	withServerBindings(
		map[string]any{"lancastThing": func() {}},
		map[string]any{"lancastThing": func() {}},
	)
}

func TestMergingBindingsKeepsBoth(t *testing.T) {
	got := withServerBindings(
		map[string]any{"a": func() {}},
		map[string]any{"b": func() {}},
	)
	if len(got) != 2 {
		t.Errorf("%d bindings, want 2", len(got))
	}
}

// The row the picker draws marks exactly one server as current, and it is the
// one being used — the only thing gold means on this page.
func TestServerRowsMarkTheCurrentServer(t *testing.T) {
	l := knownserver.List{}.Accept(knownserver.Server{
		Address: "192.168.1.21:8080", Name: "Dana", Pin: "p", Accepted: time.Now(),
	})
	l = l.Accept(knownserver.Server{Address: "other:8080", Pin: "q"})

	rows := serverRows(l, "192.168.1.21:8080")

	if len(rows) != 2 {
		t.Fatalf("%d rows, want 2", len(rows))
	}
	current := 0
	for _, r := range rows {
		if r["current"] == true {
			current++
			if r["address"] != "192.168.1.21:8080" {
				t.Errorf("the wrong row is current: %v", r["address"])
			}
		}
	}
	if current != 1 {
		t.Errorf("%d rows marked current, want exactly 1", current)
	}
}

// Nothing in a row leaks the pin. The page never needs it, and a page that
// held one could offer it back.
func TestServerRowsDoNotCarryThePin(t *testing.T) {
	l := knownserver.List{}.Accept(knownserver.Server{Address: "a:8080", Pin: "secret-pin"})

	for _, r := range serverRows(l, "") {
		for key, v := range r {
			if s, ok := v.(string); ok && strings.Contains(s, "secret-pin") {
				t.Errorf("row field %q carries the pin", key)
			}
		}
	}
}

/*
 * The three things a changed key can mean, and they must not read alike.
 *
 * The first version of this feature had one sentence and used it for every
 * change, including the ones caused by ordinary maintenance. These assert the
 * distinction rather than leaving it to whoever edits the strings next.
 */
func TestARotationAsksRatherThanAccuses(t *testing.T) {
	msg := rotationMessage("192.168.1.21:8080")
	lower := strings.ToLower(msg)

	for _, want := range []string{
		"192.168.1.21:8080",
		"reissued",
		"certificate fingerprint", // a fact about the new connection (R11-1)
		"reinstalled",
	} {
		if !strings.Contains(lower, strings.ToLower(want)) {
			t.Errorf("the rotation message does not mention %q:\n%s", want, msg)
		}
	}
	/*
	 * R11-1. The identity is what the client already holds, so an impostor
	 * is shown the same one the operator reads out and the comparison passes
	 * every time. The message must not send anybody to it.
	 */
	if strings.Contains(lower, "identity fingerprint") {
		t.Errorf("the rotation message sends the person to the identity, which cannot tell an impostor apart:\n%s", msg)
	}
	// It must not be the impostor sentence. Spending that warning on a
	// reissued certificate is what the amendment to ADR 0070 exists to stop.
	for _, forbidden := range []string{"in its place", "impersonat"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("the rotation message accuses (%q):\n%s", forbidden, msg)
		}
	}
}

// The refusal is for a changed key with nothing to check it against, and it
// has to say *why* it cannot be checked, or it reads as an accusation nobody
// can act on.
func TestTheRefusalExplainsWhyItCannotCheck(t *testing.T) {
	msg := mismatchMessage("192.168.1.21:8080")

	for _, want := range []string{
		"192.168.1.21:8080",
		"identity",
		"never got far enough",
		"forget this server",
		"do not connect",
	} {
		if !strings.Contains(strings.ToLower(msg), strings.ToLower(want)) {
			t.Errorf("the refusal does not mention %q:\n%s", want, msg)
		}
	}
}

// The strongest one, and the only refusal that is not about a certificate.
func TestAChangedIdentityIsTheStrongestRefusal(t *testing.T) {
	msg := identityMismatchMessage("192.168.1.21:8080")

	for _, want := range []string{
		"identity has changed",
		"never regenerated",
		"backup", // it survives one, so a restore is not the explanation
		"data directory",
	} {
		if !strings.Contains(strings.ToLower(msg), strings.ToLower(want)) {
			t.Errorf("the identity refusal does not mention %q:\n%s", want, msg)
		}
	}
}

/*
 * R11-1, on the screen itself. The rotation view used to show "Identity you
 * accepted" -- the value from this client's own record -- and its button
 * said the identity matched. Both pointed the person at a comparison an
 * impostor passes every time.
 */
func TestTheRotationScreenComparesTheCertificateNotTheRecord(t *testing.T) {
	page := pickerPage("")
	for _, gone := range []string{"identity_display", "rotate-identity", "identity matches"} {
		if strings.Contains(page, gone) {
			t.Errorf("the picker still carries %q", gone)
		}
	}
	for _, want := range []string{
		"The certificate matches",
		"lancastServerAcceptRotation(\n            probed.address, rotationShown)",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the picker does not carry %q", want)
		}
	}
}

// What was compared must be what is stored: a rotation is accepted only if
// the fingerprint the person read is the key at the address now.
func TestAcceptingARotationNeedsTheKeyThatWasShown(t *testing.T) {
	const pin = "AbCdEfGh1234IjKlMnOp5678QrStUvWx9012YzAbCdE="
	if !sameFingerprint(knownserver.Fingerprint(pin), pin) {
		t.Error("the grouped form of a pin does not match the pin")
	}
	for _, other := range []string{
		"",
		"abcdefgh 1234IjKl MnOp5678 QrStUvWx 9012YzAb CdE=",
		"AbCdEfGh",
	} {
		if sameFingerprint(other, pin) {
			t.Errorf("%q was taken for %q", other, pin)
		}
	}
	if sameFingerprint("", "") {
		t.Error("nothing shown and nothing offered counted as a match")
	}
}
