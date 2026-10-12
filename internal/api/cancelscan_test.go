package api

import (
	"net/http"
	"testing"
)

// Cancelling with nothing running is an ordinary answer, not an error: the
// tray's Cancel pressed a moment after a scan finished wanted exactly this.
// The list is empty, never null, so a client can count it without a guard.
// (Stopping a running scan, and what that may and may not mark missing, is
// tested in internal/scan, where the walk can be stopped at an exact point.)
func TestCancellingWithNothingRunning(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, "POST", "/api/libraries/scan/cancel", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got map[string]any
	decode(t, resp, &got)
	ids, ok := got["cancelled"].([]any)
	if !ok || len(ids) != 0 {
		t.Fatalf("cancelled = %#v, want an empty list", got["cancelled"])
	}
}

// Cancelling is an administrator's, like starting one.
func TestCancellingScansIsAdminOnly(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	m := h.member(t, "kid", "another long password")
	resp := h.asUser(t, m, "POST", "/api/libraries/scan/cancel", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}
