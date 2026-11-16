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

/*
 * A machine holding a public IPv4 address directly — which is how the server
 * this was written for is set up, with a static address — is on its
 * provider's subnet, and the interface rule made that subnet "home" (R01-14).
 * IPv4 now counts private ranges only; the house's IPv6 /64 still counts, so a
 * phone on the house Wi-Fi keeps its tiles, and a network wider than a /48 is
 * nobody's house.
 */
func TestAPublicIPv4InterfaceIsNotTheHouse(t *testing.T) {
	_, isp, _ := net.ParseCIDR("198.51.100.0/22") // the machine's own public address's subnet
	_, house, _ := net.ParseCIDR("2001:db8:1:1::/64")
	_, aggregate, _ := net.ParseCIDR("2001:db8::/32")
	local := []*net.IPNet{isp, house, aggregate}
	cases := map[string]bool{
		"198.51.100.77":        false, // a stranger on the provider's subnet
		"198.51.101.3":         false,
		"192.168.1.20":         true,  // the house's own IPv4, behind the router
		"::ffff:198.51.100.77": false, // the same stranger, IPv4-mapped
		"2001:db8:1:1::abcd":   true,  // a phone on the house's /64
		"2001:db8:9::1":        false, // inside the /32 aggregate, not the house
		"fd00::5":              true,  // unique-local
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
