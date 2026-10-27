package auth

import (
	"strings"
	"testing"
	"time"
)

// RFC 6238 Appendix B, SHA-1, cut to six digits (the low six of the RFC's
// eight, which is what truncation to six produces).
func TestTOTPMatchesTheRFCVectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for _, tc := range []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
	} {
		if got := totpAt(secret, TOTPStep(time.Unix(tc.unix, 0))); got != tc.want {
			t.Errorf("T=%d: got %s, want %s", tc.unix, got, tc.want)
		}
	}
}

func TestVerifyTOTPAcceptsTheNeighbouringStepsAndRefusesReuse(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1111111111, 0)
	cur := TOTPStep(now)

	code := totpAt(secret, cur-1)
	step, ok := VerifyTOTP(secret, code, now, 0)
	if !ok || step != cur-1 {
		t.Fatalf("previous step's code refused: %v %d", ok, step)
	}
	// Spent: the same code again, inside its window, is refused.
	if _, ok := VerifyTOTP(secret, code, now, step); ok {
		t.Fatal("a used code was accepted twice")
	}
	// Two steps away is too far.
	if _, ok := VerifyTOTP(secret, totpAt(secret, cur-2), now, 0); ok {
		t.Fatal("a code two steps old was accepted")
	}
	// Typed with a space, as apps show it.
	c := totpAt(secret, cur)
	if _, ok := VerifyTOTP(secret, c[:3]+" "+c[3:], now, 0); !ok {
		t.Fatal("a code typed with a space was refused")
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := VerifyTOTP(secret, bad, now, 0); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestTheKeyAndLinkAreWhatAppsRead(t *testing.T) {
	secret := []byte("12345678901234567890")
	if got := TOTPKey(secret); got != "GEZD GNBV GY3T QOJQ GEZD GNBV GY3T QOJQ" {
		t.Fatalf("key = %q", got)
	}
	u := TOTPURL("LANcast", "dana", secret)
	if !strings.HasPrefix(u, "otpauth://totp/LANcast:dana?") || !strings.Contains(u, "secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ") ||
		!strings.Contains(u, "issuer=LANcast") {
		t.Fatalf("url = %s", u)
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, err := NewRecoveryCodes(10)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 9 || c[4] != '-' || seen[c] {
			t.Fatalf("bad or repeated code %q", c)
		}
		seen[c] = true
		if NormalizeRecoveryCode(strings.ToUpper(strings.ReplaceAll(c, "-", " "))) != c {
			t.Fatalf("%q does not survive being typed in capitals with a space", c)
		}
	}
	if NormalizeRecoveryCode("short") != "" {
		t.Fatal("a malformed code normalised to something")
	}
}
