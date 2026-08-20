package api

import (
	"net/http"
	"testing"

	"lancast/internal/identity"
)

// The route reports this server's own fingerprint, in both forms, and they are
// the same value.
func TestIdentityReportsTheFingerprint(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	var got struct {
		Fingerprint string `json:"fingerprint"`
		Display     string `json:"fingerprint_display"`
		Name        string `json:"name"`
	}
	decode(t, h.authed(t, "GET", "/api/identity", nil), &got)

	if len(got.Fingerprint) != 52 {
		t.Errorf("fingerprint = %q (%d chars), want 52", got.Fingerprint, len(got.Fingerprint))
	}
	if got.Display == got.Fingerprint {
		t.Error("the display form is ungrouped; a client would have to group it itself")
	}
	// The two must never be able to disagree, because a person compares one and
	// a machine compares the other.
	if identity.Normalize(got.Display) != got.Fingerprint {
		t.Errorf("display %q does not normalize to %q", got.Display, got.Fingerprint)
	}
	if got.Name == "" {
		t.Error("name is empty; a peer list would show a blank row")
	}
}

// Stable across calls — the same server, not a value computed per request.
func TestIdentityIsStableAcrossRequests(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	var a, b struct {
		Fingerprint string `json:"fingerprint"`
	}
	decode(t, h.authed(t, "GET", "/api/identity", nil), &a)
	decode(t, h.authed(t, "GET", "/api/identity", nil), &b)
	if a.Fingerprint != b.Fingerprint {
		t.Errorf("identity changed between two requests: %s then %s", a.Fingerprint, b.Fingerprint)
	}
}

/*
 * Behind a session.
 *
 * The fingerprint reaches the people who need it out of band, in an invite —
 * never by being fetched off the network. A route anybody could read would be a
 * directory of one, which is the thing ADR 0044 declines to build.
 */
func TestIdentityNeedsASession(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	resp := h.do(t, "GET", "/api/identity", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without a session", resp.StatusCode)
	}
}
