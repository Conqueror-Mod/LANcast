package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lancast/internal/config"
	"lancast/internal/publiccert"
)

/*
 * R01-1: before the first account exists, the server is bound to loopback and
 * requires no session. That keeps other machines out, not other origins in a
 * browser on this one. These tests hold the two checks that cover the gap,
 * and show the configured server is unchanged.
 */

// firstRunCall sends one request to the unsecured harness, with the Host and
// Origin a browser would put on it.
func firstRunCall(t *testing.T, h *harness, method, path, host, origin, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, h.srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	// text/plain: a "simple" request, which a browser sends with no preflight.
	req.Header.Set("Content-Type", "text/plain")
	if host != "" {
		req.Host = host
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

const mallory = `{"username":"mallory","password":"attacker chose this"}`

func TestFirstRunRefusesCrossOriginSetup(t *testing.T) {
	h := newHarness(t)
	resp := firstRunCall(t, h, "POST", "/api/auth/setup", "", "https://evil.example", mallory)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a cross-origin setup", resp.StatusCode)
	}
	if n, _ := h.st.CountUsers(t.Context()); n != 0 {
		t.Fatalf("users = %d after a refused setup", n)
	}
}

// A Referer from another site is the same signal when Origin is absent.
func TestFirstRunRefusesCrossSiteRefererOnAWrite(t *testing.T) {
	h := newHarness(t)
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/libraries",
		strings.NewReader(`{"name":"x","kind":"movie","path":"C:\\"}`))
	req.Header.Set("Referer", "https://evil.example/page")
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// DNS rebinding: a page whose own name now resolves to 127.0.0.1 is
// same-origin with the server to the browser, but its Host still says so.
func TestFirstRunRefusesAForeignHost(t *testing.T) {
	h := newHarness(t)
	for _, c := range []struct{ method, path, body string }{
		{"GET", "/api/browse", ""},
		{"GET", "/api/libraries", ""},
		{"GET", "/", ""},
		// Same-origin as far as the browser knows, so Origin matches Host.
		{"POST", "/api/auth/setup", mallory},
	} {
		host := "rebind.evil.example:8080"
		resp := firstRunCall(t, h, c.method, c.path, host, "http://"+host, c.body)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s with Host %s: status = %d, want 403", c.method, c.path, host, resp.StatusCode)
		}
	}
	if n, _ := h.st.CountUsers(t.Context()); n != 0 {
		t.Fatalf("users = %d after a rebinding setup", n)
	}
}

// What the owner's own clients send still works: the browser and the desktop
// window at localhost or 127.0.0.1, with an Origin to match or none at all
// (the Go clients: the tray's probe, the native player's relay).
func TestFirstRunAcceptsThisMachine(t *testing.T) {
	_, port, _ := strings.Cut(strings.TrimPrefix(newHarness(t).srv.URL, "http://"), ":")
	for _, host := range []string{"localhost:" + port, "127.0.0.1:" + port, "[::1]:" + port, "LOCALHOST:" + port, "localhost"} {
		h := newHarness(t)
		if resp := firstRunCall(t, h, "GET", "/api/auth/status", host, "", ""); resp.StatusCode != http.StatusOK {
			t.Errorf("GET status at %s: %d, want 200", host, resp.StatusCode)
		}
		resp := firstRunCall(t, h, "POST", "/api/auth/setup", host, "http://"+host,
			`{"username":"owner","password":"a good long password"}`)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("setup at %s: %d, want 200", host, resp.StatusCode)
		}
	}

	h := newHarness(t)
	if resp := firstRunCall(t, h, "POST", "/api/auth/setup", "", "",
		`{"username":"owner","password":"a good long password"}`); resp.StatusCode != http.StatusOK {
		t.Errorf("setup with no Origin (a local tool): %d, want 200", resp.StatusCode)
	}
}

/*
 * After reset-auth, a server with a valid public certificate has no account
 * again, and the desktop window opens it at the public name mapped to
 * 127.0.0.1 (cmd/lancast/publicname.go). Refusing that name would lock the
 * owner out of setup in their own window; an expired certificate means the
 * window no longer goes there, so neither does the exemption.
 */
func TestFirstRunAcceptsThePublicNameTheWindowOpens(t *testing.T) {
	h := newHarness(t)
	pc := h.settings.Get()
	pc.PublicCert = config.PublicCert{Enabled: true, Hostname: pkRP, CA: "letsencrypt"}
	if err := h.settings.Set(pc); err != nil {
		t.Fatal(err)
	}
	call := func() int {
		r := httptest.NewRequest("GET", "/api/auth/status", nil)
		r.Host = pkRP + ":8080"
		rec := httptest.NewRecorder()
		h.srvAPI.Handler().ServeHTTP(rec, r)
		return rec.Code
	}
	h.srvAPI.publicCert = certStatus{publiccert.Status{State: publiccert.StateActive, NotAfter: time.Now().Add(24 * time.Hour)}}
	if got := call(); got != http.StatusOK {
		t.Errorf("public name with a valid certificate: %d, want 200", got)
	}
	h.srvAPI.publicCert = certStatus{publiccert.Status{State: publiccert.StateFailing, NotAfter: time.Now().Add(-time.Hour)}}
	if got := call(); got != http.StatusForbidden {
		t.Errorf("public name with an expired certificate: %d, want 403", got)
	}
}

// A configured server is reached across the network by name and address, so
// the Host rule is for the unconfigured state only; its CSRF check is the one
// it always had.
func TestSecuredServerIgnoresHostButKeepsCSRF(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	req, _ := http.NewRequest("GET", h.srv.URL+"/api/libraries", nil)
	req.Host = "lancast.home.example:8080"
	req.AddCookie(h.cookie)
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("signed-in read at another name: %d, want 200", resp.StatusCode)
	}

	resp = firstRunCall(t, h, "POST", "/api/auth/setup", "", "https://evil.example", mallory)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin setup on a configured server: %d, want 403", resp.StatusCode)
	}
}
