package api

import (
	"net/http"
	"testing"
)

// Intros skip themselves unless the account says otherwise, and the choice
// is the caller's own (ADR 0083).
func TestPlaybackPreferencesDefaultOnAndPerAccount(t *testing.T) {
	h := langHarness(t)
	member := h.addMember(t, "viewer", "correct horse battery")

	var got struct {
		SkipIntros bool `json:"skip_intros"`
	}
	decode(t, h.authed(t, "GET", "/api/profile/playback", nil), &got)
	if !got.SkipIntros {
		t.Fatal("a fresh account does not skip intros; the default is on")
	}

	decode(t, h.authed(t, "PUT", "/api/profile/playback", map[string]any{"skip_intros": false}), &got)
	if got.SkipIntros {
		t.Fatal("PUT false answered true")
	}
	decode(t, h.authed(t, "GET", "/api/profile/playback", nil), &got)
	if got.SkipIntros {
		t.Error("the choice did not stick")
	}

	// Another account is untouched by it.
	decode(t, h.doAs(t, member, "GET", "/api/profile/playback", nil), &got)
	if !got.SkipIntros {
		t.Error("one account's choice reached another")
	}

	// A body that chooses nothing is refused, not read as off.
	resp := h.authed(t, "PUT", "/api/profile/playback", map[string]any{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("PUT {} = %d, want 400", resp.StatusCode)
	}
}
