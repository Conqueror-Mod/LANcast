//go:build windows

package clientwindow

import (
	"crypto/sha256"
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func goodPin() string {
	sum := sha256.Sum256([]byte("a public key"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// The switch has to be the pinning one. --ignore-certificate-errors would
// accept any certificate from anyone on the LAN, which is the attack TLS is
// there to stop — a one-word difference between pinning and disabling.
func TestPinUsesTheSpkiListSwitchAndNotBlanketIgnore(t *testing.T) {
	t.Setenv(browserArgsEnv, "")
	if err := applyCertPin(goodPin()); err != nil {
		t.Fatalf("applyCertPin: %v", err)
	}

	got := os.Getenv(browserArgsEnv)
	if !strings.HasPrefix(got, "--ignore-certificate-errors-spki-list=") {
		t.Errorf("browser args = %q, want the spki-list switch", got)
	}
	if got == "--ignore-certificate-errors" || strings.Contains(got, "--ignore-certificate-errors ") {
		t.Errorf("browser args = %q — that disables verification entirely", got)
	}
	if !strings.HasSuffix(got, goodPin()) {
		t.Errorf("browser args = %q, want it to end with the pin", got)
	}
}

// No certificate, no switch. A loopback server is plain HTTP and pinning
// nothing must not leave a stray argument behind.
func TestNoPinSetsNothing(t *testing.T) {
	t.Setenv(browserArgsEnv, "")
	if err := applyCertPin(""); err != nil {
		t.Fatalf("applyCertPin: %v", err)
	}
	if got := os.Getenv(browserArgsEnv); got != "" {
		t.Errorf("browser args = %q, want empty", got)
	}
}

// The switch takes a comma-separated list, so a value carrying a comma or a
// space could append switches of its own. The pin comes off local disk, which
// makes that unlikely rather than impossible — and the check is two lines.
func TestMalformedPinsAreRefused(t *testing.T) {
	for _, bad := range []string{
		"not base64!",
		"c2hvcnQ=", // valid base64, wrong length
		goodPin() + ",--ignore-certificate-errors",
		goodPin() + " --disable-web-security",
		strings.Repeat("A", 43) + "=" + ",x",
	} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv(browserArgsEnv, "")
			if err := applyCertPin(bad); err == nil {
				t.Errorf("accepted %q", bad)
			}
			if got := os.Getenv(browserArgsEnv); got != "" {
				t.Errorf("a refused pin still set browser args to %q", got)
			}
		})
	}
}

// Someone already steering the browser keeps their setting, and is told rather
// than quietly overridden or silently appended to.
func TestExistingBrowserArgsAreNotClobbered(t *testing.T) {
	t.Setenv(browserArgsEnv, "--some-existing-flag")
	err := applyCertPin(goodPin())
	if err == nil {
		t.Fatal("expected an error when the variable is already set")
	}
	if got := os.Getenv(browserArgsEnv); got != "--some-existing-flag" {
		t.Errorf("existing args became %q", got)
	}
}

/*
 * Developer tools must not cost the certificate pin.
 *
 * The pin is the switch that makes this window work beyond loopback at all —
 * without it the web view refuses the server's self-signed certificate
 * outright and the app never appears. Adding a second switch to the same
 * environment variable is exactly the shape of change that quietly replaces
 * the first.
 *
 * It is also a combination nothing else exercises: a developer runs against
 * loopback, which has no certificate and therefore no pin, so pin-plus-devtools
 * is the path that only ever happens on somebody else's machine. That is the
 * v0.8.0 lesson — correct in every environment except the one that ships.
 */
func TestDevToolsKeepsTheCertificatePin(t *testing.T) {
	t.Setenv(browserArgsEnv, "")
	if err := applyBrowserArgs(goodPin(), true); err != nil {
		t.Fatalf("applyBrowserArgs: %v", err)
	}
	got := os.Getenv(browserArgsEnv)
	if !strings.Contains(got, "--ignore-certificate-errors-spki-list="+goodPin()) {
		t.Errorf("the pin was lost when developer tools were added: %q", got)
	}
	if !strings.Contains(got, "--auto-open-devtools-for-tabs") {
		t.Errorf("developer tools were not requested: %q", got)
	}
}

/*
 * Each optional switch on its own, because the paths through the function are
 * different code and only some of them append.
 *
 * This asserted equality — that developer tools alone set *only* developer
 * tools — which was true while every switch was optional. The switches that
 * keep a minimised window awake are not optional: they are what makes presence
 * tell the truth about somebody who is still there, so they ride every window.
 * The rule this test protects is therefore no longer "alone" but "present, and
 * not at the cost of the others".
 */
func TestBrowserArgsForEachSwitchAlone(t *testing.T) {
	t.Setenv(browserArgsEnv, "")
	if err := applyBrowserArgs("", true); err != nil {
		t.Fatalf("devtools with no pin: %v", err)
	}
	got := os.Getenv(browserArgsEnv)
	if !strings.Contains(got, "--auto-open-devtools-for-tabs") {
		t.Errorf("devtools with no pin set %q", got)
	}
	// No pin means no pinning switch — a window that trusts everybody is the
	// one thing worse than one that falls asleep.
	if strings.Contains(got, "--ignore-certificate-errors") {
		t.Errorf("a certificate switch appeared with no pin: %q", got)
	}

	t.Setenv(browserArgsEnv, "")
	if err := applyBrowserArgs(goodPin(), false); err != nil {
		t.Fatalf("pin with no devtools: %v", err)
	}
	got = os.Getenv(browserArgsEnv)
	if !strings.HasPrefix(got, "--ignore-certificate-errors-spki-list=") {
		t.Errorf("pin with no devtools set %q", got)
	}
	if strings.Contains(got, "devtools") {
		t.Errorf("developer tools appeared unasked: %q", got)
	}
}

// A malformed pin still fails loudly with developer tools on. Turning on an
// inspector must never be a way to skip validation of the security switch.
func TestDevToolsDoesNotExcuseABadPin(t *testing.T) {
	t.Setenv(browserArgsEnv, "")
	if err := applyBrowserArgs("not-a-pin", true); err == nil {
		t.Error("a malformed pin was accepted when developer tools were on")
	}
	if got := os.Getenv(browserArgsEnv); strings.Contains(got, "devtools") {
		t.Errorf("switches were set despite a bad pin: %q", got)
	}
}

// Something else steering the browser is still refused rather than appended to.
func TestBrowserArgsRefusesWhenSomethingElseIsSteering(t *testing.T) {
	t.Setenv(browserArgsEnv, "--some-other-switch")
	if err := applyBrowserArgs("", true); err == nil {
		t.Error("developer tools were added on top of somebody else's switches")
	}
}

/*
 * A minimised window is still somebody being there.
 *
 * Chromium throttles and then freezes timers in a backgrounded page, and this
 * window's polling is what refreshes presence — there is no heartbeat of its
 * own. Measured before the fix: minimise, and a friend on a paired server sees
 * you offline after exactly ninety seconds, which is onlineTimeout expiring
 * with nothing arriving. Back within a few seconds on restore.
 *
 * Asserted on the switches rather than on the behaviour, and the limit is worth
 * being plain about: this proves the window is *asked* for them. Whether
 * Chromium honours them is a fact about Chromium, and the only instrument for
 * it is two machines and a stopwatch — which is how the fault was found.
 */
func TestAMinimisedWindowKeepsItsTimers(t *testing.T) {
	for _, c := range []struct {
		name string
		pin  string
	}{
		{"with a pin", goodPin()},
		{"without one", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(browserArgsEnv, "")
			if err := applyBrowserArgs(c.pin, false); err != nil {
				t.Fatalf("applyBrowserArgs: %v", err)
			}
			got := os.Getenv(browserArgsEnv)

			for _, want := range liveWhileMinimised {
				if !strings.Contains(got, want) {
					t.Errorf("browser args = %q, missing %s", got, want)
				}
			}
			// The pin still has to be there and still has to be the pinning
			// switch: a window that stays awake and trusts everybody is worse
			// than one that falls asleep.
			if c.pin != "" && !strings.Contains(got, "--ignore-certificate-errors-spki-list="+c.pin) {
				t.Errorf("browser args = %q, the certificate pin did not survive", got)
			}
			if strings.HasPrefix(got, " ") || strings.Contains(got, "  ") {
				t.Errorf("browser args = %q has an empty argument in it", got)
			}
		})
	}
}

// Developer tools still arrive, and alongside the rest rather than instead of
// them. This is the composition applyBrowserArgs exists to keep in one place.
func TestDevToolsJoinTheOtherSwitchesRatherThanReplacingThem(t *testing.T) {
	t.Setenv(browserArgsEnv, "")
	if err := applyBrowserArgs(goodPin(), true); err != nil {
		t.Fatalf("applyBrowserArgs: %v", err)
	}
	got := os.Getenv(browserArgsEnv)

	wanted := append([]string{
		"--auto-open-devtools-for-tabs",
		"--ignore-certificate-errors-spki-list=" + goodPin(),
	}, liveWhileMinimised...)
	for _, want := range wanted {
		if !strings.Contains(got, want) {
			t.Errorf("browser args = %q, missing %s", got, want)
		}
	}
}

// Somebody else steering the browser is still refused, with or without a pin.
// Adding to a value we did not write is a surprise in both directions.
func TestAnExistingBrowserArgsValueIsStillRefused(t *testing.T) {
	for _, pin := range []string{goodPin(), ""} {
		t.Setenv(browserArgsEnv, "--something-somebody-else-wanted")
		if err := applyBrowserArgs(pin, false); err == nil {
			t.Errorf("pin %q: no error when %s was already set", pin, browserArgsEnv)
		}
	}
}
