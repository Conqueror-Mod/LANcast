package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"lancast/internal/auth"
)

// loginAs authenticates and returns the session cookie, without disturbing the
// harness's own admin cookie.
func (h *harness) loginAs(t *testing.T, username, password string) *http.Cookie {
	t.Helper()
	resp := h.do(t, "POST", "/api/auth/login",
		map[string]any{"username": username, "password": password})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("login as %s failed: %d %s", username, resp.StatusCode, body)
	}
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c
		}
	}
	t.Fatalf("login as %s set no cookie", username)
	return nil
}

// doAs issues a request carrying a specific session cookie.
func (h *harness) doAs(t *testing.T, cookie *http.Cookie, method, path string, body any) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, h.srv.URL+path, r)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// addMember creates a member account through the admin API and returns its cookie.
func (h *harness) addMember(t *testing.T, name, password string) *http.Cookie {
	t.Helper()
	resp := h.authed(t, "POST", "/api/users",
		map[string]any{"username": name, "password": password, "role": "member"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create member failed: %d %s", resp.StatusCode, body)
	}
	return h.loginAs(t, name, password)
}

// A member may watch but must be refused the admin-only powers on the server,
// not merely have the buttons hidden in the client (ADR 0015).
func TestMemberIsDeniedAdminPowers(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	member := h.addMember(t, "viewer", "another long password")

	adminOnly := []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/libraries", map[string]any{"name": "x", "kind": "movie", "path": h.dir}},
		{"GET", "/api/browse", nil},
		{"PUT", "/api/settings", map[string]any{"auto_enrich": true}},
		{"GET", "/api/users", nil},
		{"POST", "/api/users", map[string]any{"username": "z", "password": "xxxxxxxxxx"}},
		// Re-probing a library is hours of ffprobe. A member must not be able
		// to start it.
		{"POST", "/api/probe/refresh", nil},
	}
	for _, tc := range adminOnly {
		resp := h.doAs(t, member, tc.method, tc.path, tc.body)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("member %s %s = %d, want 403", tc.method, tc.path, resp.StatusCode)
		}
		resp.Body.Close()
	}

	// But a member can still browse the library and list it — watching is theirs.
	for _, path := range []string{"/api/libraries", "/api/items"} {
		resp := h.doAs(t, member, "GET", path, nil)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("member GET %s = %d, want 200", path, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestAdminCanManageUsers(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	// Create.
	resp := h.authed(t, "POST", "/api/users",
		map[string]any{"username": "bob", "password": "bob good password", "role": "member"})
	var created map[string]any
	decode(t, resp, &created)
	if created["role"] != "member" || created["name"] != "bob" {
		t.Errorf("created user = %v", created)
	}

	// Duplicate name is refused.
	wantError(t, h.authed(t, "POST", "/api/users",
		map[string]any{"username": "BOB", "password": "bob good password"}), 409, "conflict")

	// List shows both.
	var list struct {
		Users []map[string]any `json:"users"`
	}
	decode(t, h.authed(t, "GET", "/api/users", nil), &list)
	if len(list.Users) != 2 {
		t.Fatalf("listed %d users, want 2", len(list.Users))
	}

	// Reset bob's password, then bob logs in with it.
	id, _ := created["id"].(string)
	resp = h.authed(t, "POST", "/api/users/"+id+"/password",
		map[string]any{"new_password": "bob brand new password"})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("reset password = %d, want 204", resp.StatusCode)
	}
	resp.Body.Close()
	h.loginAs(t, "bob", "bob brand new password") // fatals if it fails

	// Delete bob.
	resp = h.authed(t, "DELETE", "/api/users/"+id, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestLastAdminCannotBeDeleted(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	var list struct {
		Users []map[string]any `json:"users"`
	}
	decode(t, h.authed(t, "GET", "/api/users", nil), &list)
	adminID, _ := list.Users[0]["id"].(string)

	wantError(t, h.authed(t, "DELETE", "/api/users/"+adminID, nil), 409, "conflict")
}
