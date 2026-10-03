package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"lancast/internal/auth"
)

// sessionCookie returns the session cookie a response set, or nil.
func sessionCookie(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	return nil
}

/*
 * A session in daily use is not signed out at day thirty.
 *
 * The server extended the session on every request; the cookie was set once
 * at login and never again, so the browser dropped it thirty days after the
 * password was last typed. Measured: a session created 09-02 11:50, used
 * daily, last seen 10-02 11:50, with a server-side expiry in November.
 */
func TestSessionCookieKeepsPaceWithUse(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	issued := h.cookie.Value

	// Straight after login the cookie is fresh; sending it again is noise.
	resp := h.authed(t, "GET", "/api/libraries", nil)
	resp.Body.Close()
	if c := sessionCookie(resp); c != nil {
		t.Fatalf("re-sent the cookie on the first request after login (MaxAge %d)", c.MaxAge)
	}

	// Some hours of use later.
	h.srvAPI.cookieMu.Lock()
	h.srvAPI.cookieSent[auth.HashToken(issued)] = time.Now().Add(-7 * time.Hour)
	h.srvAPI.cookieMu.Unlock()

	resp = h.authed(t, "GET", "/api/libraries", nil)
	resp.Body.Close()
	c := sessionCookie(resp)
	if c == nil {
		t.Fatal("a session in use did not have its cookie renewed; the browser will drop it at day thirty")
	}
	if c.Value != issued {
		t.Errorf("renewal changed the token; it must extend the same session, not mint one")
	}
	if want := int(auth.SessionTTL.Seconds()); c.MaxAge != want {
		t.Errorf("renewed MaxAge = %d, want the full %d", c.MaxAge, want)
	}

	// And not again on the very next request.
	resp = h.authed(t, "GET", "/api/libraries", nil)
	resp.Body.Close()
	if sessionCookie(resp) != nil {
		t.Error("re-sent the cookie on every request; once per refresh window is the point")
	}
}

// The page asks /api/auth/status at launch, which is the request most likely
// to be the first after a long gap, so it renews too.
func TestAuthStatusRenewsTheCookie(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	h.srvAPI.cookieMu.Lock()
	h.srvAPI.cookieSent[auth.HashToken(h.cookie.Value)] = time.Now().Add(-7 * time.Hour)
	h.srvAPI.cookieMu.Unlock()

	resp := h.authed(t, "GET", "/api/auth/status", nil)
	resp.Body.Close()
	if sessionCookie(resp) == nil {
		t.Fatal("auth status did not renew the cookie of a session it reported as authenticated")
	}
}

// No session, no cookie: renewal never hands out a cookie it did not receive.
func TestNoCookieForAnUnauthenticatedCaller(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	h.cookie = nil
	resp := h.authed(t, "GET", "/api/auth/status", nil)
	resp.Body.Close()
	if sessionCookie(resp) != nil {
		t.Fatal("an unauthenticated caller was sent a session cookie")
	}
}

/*
 * A refused login says why, in the log and only in the log.
 *
 * Somebody was turned away with "incorrect username or password" for twenty
 * minutes and the server had recorded nothing, so the cause could not be
 * found afterwards. The reply stays one sentence for both cases; the log tells
 * them apart, and never holds the password.
 */
func TestRefusedLoginsAreLoggedWithoutThePassword(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	h.cookie = nil
	var buf bytes.Buffer
	h.srvAPI.log = slog.New(slog.NewTextHandler(&buf, nil))

	wrong := h.do(t, "POST", "/api/auth/login", map[string]any{"username": testUser, "password": "hunter2-wrong"})
	wrong.Body.Close()
	unknown := h.do(t, "POST", "/api/auth/login", map[string]any{"username": "s3cret-typed-in-name-box", "password": "x"})
	unknown.Body.Close()

	log := buf.String()
	if !strings.Contains(log, `reason="wrong password"`) || !strings.Contains(log, "account="+testUser) {
		t.Errorf("a wrong password was not logged with its account:\n%s", log)
	}
	if !strings.Contains(log, `reason="no such account"`) {
		t.Errorf("an unknown account was not logged:\n%s", log)
	}
	for _, secret := range []string{"hunter2-wrong", "s3cret-typed-in-name-box"} {
		if strings.Contains(log, secret) {
			t.Errorf("the log holds %q; a password, or a name that matched no account, must never be written down", secret)
		}
	}
}

func TestThrottledLoginIsLogged(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	h.cookie = nil
	var buf bytes.Buffer
	h.srvAPI.log = slog.New(slog.NewTextHandler(&buf, nil))

	for range 12 {
		r := h.do(t, "POST", "/api/auth/login", map[string]any{"username": testUser, "password": "wrong"})
		r.Body.Close()
	}
	if !strings.Contains(buf.String(), `reason=throttled`) {
		t.Errorf("throttled attempts left no trace:\n%s", buf.String())
	}
}
