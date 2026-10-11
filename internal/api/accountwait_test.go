package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"lancast/internal/auth"
)

// loginFrom calls the login handler as though from addr. The harness server
// is on loopback, which InsideNetwork exempts, so the outside has to be
// constructed by hand.
func loginFrom(t *testing.T, h *harness, addr, user, pw string) int {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"username": user, "password": pw})
	r := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(b))
	r.RemoteAddr = addr
	w := httptest.NewRecorder()
	h.srvAPI.authLogin(w, r)
	return w.Code
}

/*
 * Guessing from many addresses is slowed down per account.
 *
 * Every attempt below comes from a different public address, so the per-address
 * throttle never sees more than one from each. Before AccountBackoff, the
 * correct password on attempt one thousand would have been accepted with no
 * wait at all.
 */
func TestGuessingFromManyAddressesIsSlowedPerAccount(t *testing.T) {
	h := newHarness(t)
	pw := "a long enough password"
	h.secure(t, pw)

	free := auth.NewAccountBackoff().Free
	for i := 0; i < free+1; i++ {
		if code := loginFrom(t, h, fmt.Sprintf("203.0.113.%d:4000", i+1), testUser, "wrong"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d, want 401", i+1, code)
		}
	}

	// The right password, from yet another address, during the wait: refused
	// without being checked. That is what makes the wait cost an attacker
	// anything — the answer is withheld, not merely delayed.
	if code := loginFrom(t, h, "198.51.100.7:4000", testUser, pw); code != http.StatusTooManyRequests {
		t.Fatalf("status %d during the wait, want 429", code)
	}
}

// The owner at home is never made to wait by somebody else's attack: the
// account backoff does not apply from a private address. The per-address
// throttle still does.
func TestAnAttackCannotLockTheOwnerOutAtHome(t *testing.T) {
	h := newHarness(t)
	pw := "a long enough password"
	h.secure(t, pw)

	for i := 0; i < auth.NewAccountBackoff().Free+5; i++ {
		loginFrom(t, h, fmt.Sprintf("203.0.113.%d:4000", i+1), testUser, "wrong")
	}
	if code := loginFrom(t, h, "192.168.1.20:4000", testUser, pw); code != http.StatusOK {
		t.Fatalf("owner at home got %d during an outside attack, want 200", code)
	}
}

// A 429 must not reveal whether an account exists: an invented name waits
// exactly like a real one.
func TestTheWaitDoesNotRevealWhetherAnAccountExists(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")

	free := auth.NewAccountBackoff().Free
	for _, name := range []string{testUser, "nobody-by-this-name"} {
		for i := 0; i < free+1; i++ {
			loginFrom(t, h, fmt.Sprintf("203.0.113.%d:4000", i+1), name, "wrong")
		}
		if code := loginFrom(t, h, "198.51.100.7:4000", name, "wrong"); code != http.StatusTooManyRequests {
			t.Errorf("%q: status %d, want 429 — a wait for real accounts only "+
				"would answer whether the account exists", name, code)
		}
	}
}
