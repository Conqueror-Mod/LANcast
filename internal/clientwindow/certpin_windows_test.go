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

// Each switch alone, because the two orders through the function are different
// code and only one of them appends.
func TestBrowserArgsForEachSwitchAlone(t *testing.T) {
	t.Setenv(browserArgsEnv, "")
	if err := applyBrowserArgs("", true); err != nil {
		t.Fatalf("devtools alone: %v", err)
	}
	if got := os.Getenv(browserArgsEnv); got != "--auto-open-devtools-for-tabs" {
		t.Errorf("devtools alone set %q", got)
	}

	t.Setenv(browserArgsEnv, "")
	if err := applyBrowserArgs(goodPin(), false); err != nil {
		t.Fatalf("pin alone: %v", err)
	}
	if got := os.Getenv(browserArgsEnv); !strings.HasPrefix(got, "--ignore-certificate-errors-spki-list=") {
		t.Errorf("pin alone set %q", got)
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
