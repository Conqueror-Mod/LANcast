package api

import (
	"net/http"
	"testing"
)

// An account chooses its own picture and reads it back where the rail reads
// the rest of who it is: /api/auth/status.
func TestAnAccountChoosesItsAvatarAndReadsItBack(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	status := func() string {
		var body struct {
			User struct {
				Avatar *string `json:"avatar"`
			} `json:"user"`
		}
		decode(t, h.authed(t, "GET", "/api/auth/status", nil), &body)
		if body.User.Avatar == nil {
			t.Fatal("auth status carries no avatar field")
		}
		return *body.User.Avatar
	}
	if got := status(); got != "" {
		t.Fatalf("a new account's avatar = %q, want none", got)
	}

	var put struct {
		Avatar string `json:"avatar"`
	}
	decode(t, h.authed(t, "PUT", "/api/profile/avatar", map[string]any{"avatar": "wolf"}), &put)
	if put.Avatar != "wolf" {
		t.Errorf("response avatar = %q, want wolf", put.Avatar)
	}
	if got := status(); got != "wolf" {
		t.Errorf("avatar after choosing = %q, want wolf", got)
	}

	h.authed(t, "PUT", "/api/profile/avatar", map[string]any{"avatar": ""}).Body.Close()
	if got := status(); got != "" {
		t.Errorf("avatar after clearing = %q, want none", got)
	}
}

// Only the drawn set: the value is shown to every client, so a URL or free
// text must never be stored.
func TestAnUnknownAvatarIsA400(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	for _, bad := range []string{"dragon", "https://example.com/me.png"} {
		resp := h.authed(t, "PUT", "/api/profile/avatar", map[string]any{"avatar": bad})
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("avatar %q: status = %d, want 400", bad, resp.StatusCode)
		}
	}
}
