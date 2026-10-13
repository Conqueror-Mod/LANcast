package api

import (
	"net/http"
	"strings"
	"testing"
)

// Server › Network at the boundary: an administrator's status, which lists
// addresses only when other machines can actually reach the server.
func TestNetworkStatusFollowsTheLoopbackRule(t *testing.T) {
	h := langHarness(t)
	member := h.addMember(t, "viewer", "correct horse battery")
	if code := status(t, h.doAs(t, member, "GET", "/api/network", nil)); code != http.StatusForbidden {
		t.Errorf("member GET /api/network = %d, want 403", code)
	}

	var got networkStatus
	h.srvAPI.lanBound = false
	h.srvAPI.listenAddr = "127.0.0.1:8080"
	decode(t, h.authed(t, "GET", "/api/network", nil), &got)
	if got.LANEnabled || len(got.Addresses) != 0 || got.Port != "8080" || !got.Secured {
		t.Errorf("loopback-only status = %+v", got)
	}

	h.srvAPI.lanBound = true
	h.srvAPI.listenAddr = ":8080"
	decode(t, h.authed(t, "GET", "/api/network", nil), &got)
	if !got.LANEnabled {
		t.Errorf("LAN-bound status = %+v", got)
	}
	for _, a := range got.Addresses {
		if !strings.HasPrefix(a, "https://") || !strings.HasSuffix(a, ":8080") {
			t.Errorf("address %q is not an https URL on the listen port", a)
		}
	}
}

// The pane leads with the address a phone in the same house would use: the
// first real look at it led with a ZeroTier 10.x and listed 192.168.1.x last.
func TestTheHomeAddressComesFirst(t *testing.T) {
	got := homeFirst([]string{"10.121.240.235", "2600:1702::2e", "fd00::5", "192.168.1.160", "172.16.0.9"})
	want := []string{"192.168.1.160", "10.121.240.235", "172.16.0.9", "2600:1702::2e", "fd00::5"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("homeFirst = %v, want %v", got, want)
	}
}
