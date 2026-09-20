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
		Address: "192.168.1.66:8080", Name: "Chris", Pin: "p", Accepted: time.Now(),
	})
	l = l.Accept(knownserver.Server{Address: "other:8080", Pin: "q"})

	rows := serverRows(l, "192.168.1.66:8080")

	if len(rows) != 2 {
		t.Fatalf("%d rows, want 2", len(rows))
	}
	current := 0
	for _, r := range rows {
		if r["current"] == true {
			current++
			if r["address"] != "192.168.1.66:8080" {
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
 * The refusal is the sentence this feature is judged by, so its content is
 * asserted rather than left to whoever edits it next.
 *
 * It has to do three things: say the key changed, rule out the innocent
 * explanations that are not explanations, and name the two that are -- in an
 * order where somebody who reinstalled their server recognises themselves
 * before somebody who did not is reassured.
 */
func TestTheRefusalExplainsItself(t *testing.T) {
	msg := mismatchMessage("192.168.1.66:8080")

	for _, want := range []string{
		"192.168.1.66:8080",
		"public key",
		"renewed",       // the rotation that does NOT change a pin
		"network addre", // nor does gaining one
		"reinstalled",
		"forget this server",
		"should not connect",
	} {
		if !strings.Contains(strings.ToLower(msg), strings.ToLower(want)) {
			t.Errorf("the refusal does not mention %q:\n%s", want, msg)
		}
	}
}
