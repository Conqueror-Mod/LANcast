package api

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

/*
 * The sign-in page's account picker. The names are half of every login and
 * this server answers the internet at its public name, so the assertion that
 * matters most is the empty list for a stranger.
 */

func TestTheHomeNetworkRule(t *testing.T) {
	_, house, _ := net.ParseCIDR("2001:db8:1:1::/64")
	_, everything, _ := net.ParseCIDR("::/0")
	local := []*net.IPNet{house, everything}
	cases := map[string]bool{
		"127.0.0.1":          true,  // this computer
		"::1":                true,  // this computer
		"192.168.1.20":       true,  // the house's IPv4
		"10.0.0.5":           true,  // a private range
		"fe80::1":            true,  // link-local
		"2001:db8:1:1::abcd": true,  // a phone on the house Wi-Fi, by IPv6
		"2001:db8:1:2::abcd": false, // the next network over
		"203.0.113.9":        false, // the internet
		"2001:db8:ffff::1":   false, // the internet, IPv6
	}
	for addr, want := range cases {
		if got := onHomeNetwork(net.ParseIP(addr), local); got != want {
			t.Errorf("%s: home = %v, want %v", addr, got, want)
		}
	}
}

func pickerAsk(t *testing.T, h *harness, remote string) []string {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/auth/accounts", nil)
	req.RemoteAddr = remote
	rec := httptest.NewRecorder()
	h.srvAPI.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d", remote, rec.Code)
	}
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

func TestThePickerListsAccountsAtHomeOnly(t *testing.T) {
	a := newAccessHarness(t)
	a.srvAPI.localNets = func() []*net.IPNet { return nil }

	home := pickerAsk(t, a.harness, "192.168.1.20:51000")
	if len(home) < 2 {
		t.Fatalf("at home the picker listed %v, want every account", home)
	}
	if got := pickerAsk(t, a.harness, "203.0.113.9:51000"); len(got) != 0 {
		t.Errorf("from the internet the picker listed %v, want nothing", got)
	}

	// Off means off, at home too.
	cur := a.settings.Get()
	cur.LoginPicker = false
	if err := a.settings.Set(cur); err != nil {
		t.Fatal(err)
	}
	if got := pickerAsk(t, a.harness, "192.168.1.20:51000"); len(got) != 0 {
		t.Errorf("turned off, the picker listed %v", got)
	}
}
