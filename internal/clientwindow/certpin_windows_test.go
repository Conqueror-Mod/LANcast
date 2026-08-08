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
