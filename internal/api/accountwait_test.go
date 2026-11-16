package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

// A refused sign-in takes as long for a name that is nobody as for a real
// account, so the timing does not sort a list of names into real and not real
// (R01-7). On main the gap was 196 ms against 0.5 ms; this asks only that the
// unknown name take at least a quarter as long, which a machine under load
// still clears and the old shortcut could not.
func TestASignInForNobodyTakesAsLongAsOne(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	timeIt := func(user string) time.Duration {
		best := time.Duration(1<<63 - 1)
		for i := 0; i < 3; i++ {
			start := time.Now()
			resp := h.do(t, "POST", "/api/auth/login", map[string]any{"username": user, "password": "not the password"})
			resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("%s: status %d", user, resp.StatusCode)
			}
			if d := time.Since(start); d < best {
				best = d
			}
		}
		return best
	}
	known, unknown := timeIt(testUser), timeIt("nobody-here")
	t.Logf("existing account %v, unknown name %v", known, unknown)
	if unknown < known/4 {
		t.Errorf("an unknown name is answered in %v against %v for a real one", unknown, known)
	}
}
