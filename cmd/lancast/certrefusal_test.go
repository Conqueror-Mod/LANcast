package main

import (
	"errors"
	"strings"
	"testing"

	"lancast/internal/knownserver"
)

/*
 * R11-3. A remembered remote server is opened directly only when the key it
 * is presenting now is the one on record. Anything else -- a reissued
 * certificate, a key nobody can vouch for, nothing answering -- goes to the
 * picker, where a person is shown what changed before the window is given
 * any pin. The old path handed the stored pin to the window and let
 * Chromium's click-through warning decide.
 */
func TestARemoteServerOpensOnlyWhenItsKeyMatches(t *testing.T) {
	if got := remoteLaunchReason(remoteAddr, knownserver.TrustMatch, nil); got != "" {
		t.Errorf("a matching key was not opened: %q", got)
	}
	for name, trust := range map[string]knownserver.Trust{
		"rotated":  knownserver.TrustRotated,
		"mismatch": knownserver.TrustMismatch,
		"unknown":  knownserver.TrustUnknown,
	} {
		got := remoteLaunchReason(remoteAddr, trust, nil)
		if got == "" {
			t.Errorf("%s: opened directly; it must go to the picker", name)
		}
	}
	if got := remoteLaunchReason(remoteAddr, knownserver.TrustMatch, errors.New("refused")); !strings.Contains(got, "could not reach") {
		t.Errorf("an unreachable server: %q", got)
	}
}

// The sentences name the address and say that nothing was connected, so the
// picker never reads like the app forgot the server.
func TestTheReasonsSayNothingWasConnected(t *testing.T) {
	for _, got := range []string{
		remoteLaunchReason(remoteAddr, knownserver.TrustRotated, nil),
		remoteLaunchReason(remoteAddr, knownserver.TrustMismatch, nil),
	} {
		if !strings.Contains(got, remoteAddr) || !strings.Contains(got, "did not connect") {
			t.Errorf("reason %q does not name the address and say it did not connect", got)
		}
	}
	remote := certificateRefusedReason(target{address: remoteAddr})
	local := certificateRefusedReason(target{address: ":8080", local: true})
	for _, got := range []string{remote, local} {
		if !strings.Contains(got, "refused") {
			t.Errorf("reason %q does not say the certificate was refused", got)
		}
	}
	if !strings.Contains(remote, remoteAddr) {
		t.Errorf("the remote reason does not name the address: %q", remote)
	}
}

// Only the server's own page failing sends the window to the picker.
func TestOnlyTheServersOwnOriginLeavesForThePicker(t *testing.T) {
	origins := []string{"https://192.168.1.21:8080", "https://media.example.test"}
	for uri, want := range map[string]bool{
		"https://192.168.1.21:8080/":             true,
		"https://192.168.1.21:8080/api/items?q=": true,
		"https://MEDIA.example.test/settings":    true,
		"https://192.168.1.21:8443/":             false,
		"http://192.168.1.21:8080/":              false,
		"https://images.example.org/poster.jpg":  false,
		"":                                       false,
		"about:blank":                            false,
	} {
		if got := sameOrigin(uri, origins...); got != want {
			t.Errorf("sameOrigin(%q) = %v, want %v", uri, got, want)
		}
	}
}
