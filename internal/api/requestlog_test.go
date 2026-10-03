package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

// captureLog points the server's log at a buffer, at Info, the level a real
// install runs at, so a Debug line is invisible here exactly as it would be
// there.
func (h *harness) captureLog() *bytes.Buffer {
	var buf bytes.Buffer
	h.srvAPI.log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return &buf
}

func countLines(buf *bytes.Buffer, substr string) int {
	n := 0
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, substr) {
			n++
		}
	}
	return n
}

// A refused cross-origin write is logged, once per client per window however
// many times it is repeated, and says where it came from.
func TestCrossOriginRefusalIsLoggedOnce(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	buf := h.captureLog()

	for range 10 {
		resp := h.doOrigin(t, "POST", "/api/libraries", "https://evil.example", "", map[string]any{})
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
	}
	if n := countLines(buf, `reason=cross-origin`); n != 1 {
		t.Fatalf("10 refused cross-origin writes logged %d lines, want 1:\n%s", n, buf)
	}
	if !strings.Contains(buf.String(), "origin=https://evil.example") {
		t.Errorf("the refusal does not say which origin was refused:\n%s", buf)
	}
}

/*
 * The three kinds of 401 are told apart in the log, and a tab with a dead
 * cookie asking many routes is one line, not one per route.
 */
func TestUnauthenticatedIsLoggedByKindAndOnce(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	buf := h.captureLog()

	// A cookie the server does not know: somebody who was signed in.
	h.cookie = &http.Cookie{Name: h.cookie.Name, Value: "not-a-session-the-server-knows"}
	for _, route := range []string{"/api/libraries", "/api/items", "/api/people", "/api/libraries", "/api/items"} {
		resp := h.authed(t, "GET", route, nil)
		resp.Body.Close()
	}
	if n := countLines(buf, `reason="session expired or unknown"`); n != 1 {
		t.Fatalf("a dead cookie across five requests logged %d lines, want 1:\n%s", n, buf)
	}
	if strings.Contains(buf.String(), "not-a-session-the-server-knows") {
		t.Error("the cookie's value reached the log")
	}

	// A Bearer key nobody issued.
	resp := h.doOrigin(t, "GET", "/api/libraries", "", "lck_not_a_real_key", nil)
	resp.Body.Close()
	if !strings.Contains(buf.String(), `reason="API key not recognised"`) {
		t.Errorf("an unknown API key was not told apart:\n%s", buf)
	}
	if strings.Contains(buf.String(), "lck_not_a_real_key") {
		t.Error("the key's value reached the log")
	}

	// Nobody at all: ordinary, and Info rather than Warn.
	h.cookie = nil
	resp = h.authed(t, "GET", "/api/libraries", nil)
	resp.Body.Close()
	var line string
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.Contains(l, `reason="not signed in"`) {
			line = l
		}
	}
	if line == "" || !strings.Contains(line, "level=INFO") {
		t.Errorf("a visitor with no credential should be one Info line, got %q", line)
	}
}

// Signing in and out are recorded with the account, and nothing secret.
func TestSessionEventsAreLogged(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	buf := h.captureLog()
	h.cookie = nil

	resp := h.do(t, "POST", "/api/auth/login", map[string]any{"username": testUser, "password": "a good long password"})
	resp.Body.Close()
	h.cookie = sessionCookie(resp)
	if !strings.Contains(buf.String(), `msg="signed in" account=`+testUser) {
		t.Fatalf("a successful login was not logged with its account:\n%s", buf)
	}

	resp = h.authed(t, "POST", "/api/auth/logout", nil)
	resp.Body.Close()
	if !strings.Contains(buf.String(), `msg="signed out" account=`+testUser) {
		t.Errorf("signing out was not logged with its account:\n%s", buf)
	}
	if strings.Contains(buf.String(), "a good long password") {
		t.Error("the password reached the log")
	}
}

/*
 * A playback decision is one line with the title, the method and the reason,
 * and asking again for the same thing inside the window is not a new line.
 * Before this, a film that played directly left no trace at all.
 */
func TestPlaybackDecisionIsLoggedOnce(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	id := h.addFile(t, "Dreamcatcher (2003).mkv", []byte("not really a film"))
	buf := h.captureLog()

	for range 3 {
		resp := h.authed(t, "GET", "/api/items/"+itoa(id)+"/playback?can=hevc,matroska", nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	}
	if n := countLines(buf, `msg="playback decided"`); n != 1 {
		t.Fatalf("three identical asks logged %d decisions, want 1:\n%s", n, buf)
	}
	line := buf.String()
	for _, want := range []string{"item=" + itoa(id), `title="Dreamcatcher`, "method=", "claims=hevc,matroska", "account=" + testUser} {
		if !strings.Contains(line, want) {
			t.Errorf("the decision line lacks %q:\n%s", want, line)
		}
	}
}
