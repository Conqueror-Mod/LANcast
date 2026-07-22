package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"lancast/internal/auth"
)

// login sets a password and returns a client carrying the session cookie.
func (h *harness) secure(t *testing.T, password string) *http.Client {
	t.Helper()
	resp := h.do(t, "POST", "/api/auth/setup", map[string]any{"password": password})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("setup failed: %d %s", resp.StatusCode, body)
	}

	jar := h.srv.Client()
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			h.cookie = c
		}
	}
	if h.cookie == nil {
		t.Fatal("setup did not return a session cookie")
	}
	return jar
}

// authed issues a request carrying the session cookie.
func (h *harness) authed(t *testing.T, method, path string, body any) *http.Response {
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
	if h.cookie != nil {
		req.AddCookie(h.cookie)
	}
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// Before setup the API is open, because the server is loopback-only until a
// password exists and requiring a session would lock the owner out of setup.
func TestUnsecuredServerIsOpen(t *testing.T) {
	h := newHarness(t)

	resp := h.do(t, "GET", "/api/libraries", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 before a password is set", resp.StatusCode)
	}

	var st map[string]any
	decode(t, h.do(t, "GET", "/api/auth/status", nil), &st)
	if st["configured"] != false || st["authenticated"] != true {
		t.Errorf("status = %v, want unconfigured and open", st)
	}
}

func TestSetupThenRequiresAuth(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	// Without the cookie, the API is closed.
	resp := h.do(t, "GET", "/api/libraries", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without a session", resp.StatusCode)
	}

	// With it, open again.
	authed := h.authed(t, "GET", "/api/libraries", nil)
	defer authed.Body.Close()
	if authed.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 with a session", authed.StatusCode)
	}
}

// Streaming must be gated too — it is the endpoint that serves the actual
// media, and a public stream URL would make the password decorative.
func TestStreamRequiresAuth(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "movie.mkv", []byte("some bytes"))
	h.secure(t, "a good long password")

	resp := h.do(t, "GET", "/api/stream/"+itoa(id), nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for an unauthenticated stream", resp.StatusCode)
	}

	ok := h.authed(t, "GET", "/api/stream/"+itoa(id), nil)
	defer ok.Body.Close()
	if ok.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 with a session", ok.StatusCode)
	}
}

func TestSetupRejectsWeakPassword(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "POST", "/api/auth/setup", map[string]any{"password": "short"}),
		400, "bad_request")
}

func TestSetupTwiceIsRefused(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	wantError(t, h.do(t, "POST", "/api/auth/setup", map[string]any{"password": "another password"}),
		409, "conflict")
}

func TestLogin(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	h.cookie = nil

	wantError(t, h.do(t, "POST", "/api/auth/login", map[string]any{"password": "wrong"}),
		401, "unauthorized")

	resp := h.do(t, "POST", "/api/auth/login", map[string]any{"password": "a good long password"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var found bool
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			found = true
			h.cookie = c
		}
	}
	if !found {
		t.Fatal("login did not set a session cookie")
	}

	ok := h.authed(t, "GET", "/api/libraries", nil)
	defer ok.Body.Close()
	if ok.StatusCode != http.StatusOK {
		t.Errorf("the issued session does not work: %d", ok.StatusCode)
	}
}

func TestLogout(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	resp := h.authed(t, "POST", "/api/auth/logout", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	// The session must be dead server-side, not merely cleared in the browser.
	after := h.authed(t, "GET", "/api/libraries", nil)
	defer after.Body.Close()
	if after.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 — the session survived logout", after.StatusCode)
	}
}

// Changing the password must revoke every session. A password change that
// leaves old sessions alive has not actually locked anyone out — which is the
// entire reason sessions live server-side rather than in a signed cookie.
func TestPasswordChangeRevokesSessions(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	resp := h.authed(t, "POST", "/api/auth/password", map[string]any{
		"current_password": "a good long password",
		"new_password":     "an even better password",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	after := h.authed(t, "GET", "/api/libraries", nil)
	defer after.Body.Close()
	if after.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 — the old session survived a password change", after.StatusCode)
	}
}

func TestPasswordChangeNeedsCurrentPassword(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	resp := h.authed(t, "POST", "/api/auth/password", map[string]any{
		"current_password": "not it",
		"new_password":     "an even better password",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// SameSite=Strict covers browsers; the origin check covers everything else and
// the cases where SameSite is not honoured.
func TestCrossOriginStateChangeRefused(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	req, _ := http.NewRequest("POST", h.srv.URL+"/api/libraries",
		bytes.NewReader([]byte(`{"name":"x","kind":"movie","path":"C:\\"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	req.AddCookie(h.cookie)

	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for a cross-origin state change", resp.StatusCode)
	}
}

// Reads are not blocked by the origin check; the session gate already covers
// them, and blocking cross-origin GETs would break embedding a stream URL.
func TestCrossOriginReadStillNeedsSessionOnly(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	req, _ := http.NewRequest("GET", h.srv.URL+"/api/libraries", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.AddCookie(h.cookie)

	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestLoginThrottled(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	h.cookie = nil

	var throttled bool
	for i := 0; i < 25; i++ {
		resp := h.do(t, "POST", "/api/auth/login", map[string]any{"password": "wrong"})
		code := resp.StatusCode
		resp.Body.Close()
		if code == http.StatusTooManyRequests {
			throttled = true
			break
		}
	}
	if !throttled {
		t.Error("unlimited password attempts were allowed against a single shared secret")
	}
}

// The web assets must stay public: the login form lives in them, and gating
// the page behind a session no one can obtain yet is a locked door with the
// key inside.
func TestWebAssetsArePublic(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	resp := h.do(t, "GET", "/", nil)
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Error("the login page itself requires a session")
	}
}

func TestHealthStaysPublic(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	resp := h.do(t, "GET", "/api/health", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want health reachable without credentials", resp.StatusCode)
	}
}
