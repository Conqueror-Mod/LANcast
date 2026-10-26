package api

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// pickerVia asks for the sign-in page's accounts from a proxy on this machine,
// on behalf of a client at forwardedFor.
func pickerVia(t *testing.T, h *harness, forwardedFor string) []string {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/auth/accounts", nil)
	req.RemoteAddr = "127.0.0.1:51000"
	req.Header.Set("X-Forwarded-For", forwardedFor)
	rec := httptest.NewRecorder()
	h.srvAPI.Handler().ServeHTTP(rec, req)
	var body struct {
		Accounts []struct {
			Name string `json:"name"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range body.Accounts {
		names = append(names, a.Name)
	}
	return names
}

/*
 * The case the setting exists for: a reverse proxy on the same machine made
 * every caller loopback, so the sign-in page listed account names to the
 * internet. With the proxy trusted, the stranger it forwards is a stranger.
 */
func TestATrustedProxyStopsTheInternetLookingLikeHome(t *testing.T) {
	a := newAccessHarness(t)
	a.srvAPI.localNets = func() []*net.IPNet { return nil }

	if got := pickerVia(t, a.harness, "203.0.113.9"); len(got) == 0 {
		t.Fatal("before trusting the proxy, everything through it looks like this machine — the bug this fixes")
	}

	cur := a.settings.Get()
	cur.TrustedProxies = []string{"127.0.0.1/32"}
	if err := a.settings.Set(cur); err != nil {
		t.Fatal(err)
	}
	if got := pickerVia(t, a.harness, "203.0.113.9"); len(got) != 0 {
		t.Errorf("an internet client through the proxy was shown %v", got)
	}
	// Someone at home, through the same proxy, still gets the picker.
	if got := pickerVia(t, a.harness, "192.168.1.20"); len(got) < 2 {
		t.Errorf("a home client through the proxy was shown %v", got)
	}
	// A forged "I am at home" to the left of the real address is not believed.
	if got := pickerVia(t, a.harness, "192.168.1.20, 203.0.113.9"); len(got) != 0 {
		t.Errorf("a forged entry was believed: shown %v", got)
	}
}

func TestTrustedProxiesSettingIsValidatedAndNormalised(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")

	for _, bad := range [][]string{{"proxy.local"}, {"0.0.0.0/0"}} {
		resp := h.authed(t, "PUT", "/api/settings", map[string]any{"trusted_proxies": bad})
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%v: status %d, want 400", bad, resp.StatusCode)
		}
	}

	resp := h.authed(t, "PUT", "/api/settings", map[string]any{"trusted_proxies": []string{" 127.0.0.1 ", "10.0.0.0/24"}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var got struct {
		TrustedProxies []string `json:"trusted_proxies"`
	}
	decode(t, h.authed(t, "GET", "/api/settings", nil), &got)
	if len(got.TrustedProxies) != 2 || got.TrustedProxies[0] != "127.0.0.1/32" || got.TrustedProxies[1] != "10.0.0.0/24" {
		t.Fatalf("stored %v", got.TrustedProxies)
	}

	// An empty list turns it off, and reads back as [] rather than null.
	resp = h.authed(t, "PUT", "/api/settings", map[string]any{"trusted_proxies": []string{}})
	resp.Body.Close()
	raw := map[string]json.RawMessage{}
	decode(t, h.authed(t, "GET", "/api/settings", nil), &raw)
	if string(raw["trusted_proxies"]) != "[]" {
		t.Fatalf("cleared list reads as %s", raw["trusted_proxies"])
	}
}
