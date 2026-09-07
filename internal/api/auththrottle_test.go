package api

import (
	"net/http"
	"testing"

	"lancast/internal/auth"
)

/*
 * Changing a password is throttled, and shares the login counter.
 *
 * The handler verifies `current_password` with bcrypt at cost 12 before doing
 * anything else, which makes an unbounded version two things at once: a
 * password oracle for whoever holds a stolen session — the password outlives
 * every session being revoked, and people reuse it — and about a hundred
 * milliseconds of deliberate work per request with nothing bounding it.
 *
 * Login has been throttled since long before this. This route was simply
 * missed, which is the ordinary way a gap appears: not a decision, an omission
 * in a second place that asks the same question.
 */
func TestChangingAPasswordIsThrottled(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")

	// One more than the throttle allows, all wrong.
	var last *http.Response
	for i := 0; i < auth.NewThrottle().Max+1; i++ {
		if last != nil {
			last.Body.Close()
		}
		last = h.authed(t, "POST", "/api/auth/password", map[string]any{
			"current_password": "not the password",
			"new_password":     "a different long password",
		})
	}
	defer last.Body.Close()

	if last.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 — unlimited attempts against the current "+
			"password is a bcrypt oracle for anybody holding a stolen session",
			last.StatusCode)
	}
}

/*
 * And an attacker cannot get a fresh budget by moving between the two routes.
 *
 * Sharing one counter is the point: somebody who has burned their login
 * attempts should not find a full allowance waiting on the password-change
 * route, which asks the same question and costs the same bcrypt.
 */
func TestTheLoginAndPasswordRoutesShareOneBudget(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")

	for i := 0; i < auth.NewThrottle().Max; i++ {
		resp := h.do(t, "POST", "/api/auth/login", map[string]any{
			"username": testUser, "password": "wrong",
		})
		resp.Body.Close()
	}

	resp := h.authed(t, "POST", "/api/auth/password", map[string]any{
		"current_password": "also wrong",
		"new_password":     "a different long password",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429 — the password route handed out a fresh "+
			"budget after the login route's was spent", resp.StatusCode)
	}
}

// A correct current password clears the counter, exactly as a successful login
// does: it is the same proof.
func TestACorrectPasswordClearsTheThrottle(t *testing.T) {
	h := newHarness(t)
	pw := "a long enough password"
	h.secure(t, pw)

	for i := 0; i < auth.NewThrottle().Max-1; i++ {
		resp := h.authed(t, "POST", "/api/auth/password", map[string]any{
			"current_password": "wrong", "new_password": "another long password",
		})
		resp.Body.Close()
	}

	resp := h.authed(t, "POST", "/api/auth/password", map[string]any{
		"current_password": pw, "new_password": "another long password",
	})
	resp.Body.Close()
	// 204: the handler answers No Content, having also revoked every session.
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("the correct password was refused: %d", resp.StatusCode)
	}

	// The budget is fresh again, so a later mistake is not instantly a 429.
	resp = h.do(t, "POST", "/api/auth/login", map[string]any{
		"username": testUser, "password": "wrong",
	})
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		t.Error("a successful password change did not clear the counter")
	}
}
