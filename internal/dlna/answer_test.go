package dlna

import (
	"net"
	"testing"
)

/*
 * R10-8. Discovery answers the local network and nothing else, matching the
 * HTTP side's rule (ADR 0086 §4).
 *
 * The case that matters is a machine whose own address is public: its
 * neighbours are on its subnet without being in a private range, and the
 * rest of the internet is not. Documentation ranges stand in for both.
 */
func TestDiscoveryAnswersOnlyTheLocalNetwork(t *testing.T) {
	_, public, err := net.ParseCIDR("203.0.113.0/24")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		from string
		own  *net.IPNet
		want bool
	}{
		{"192.168.1.50", nil, true},
		{"10.0.0.7", nil, true},
		{"172.16.4.4", nil, true},
		{"169.254.10.10", nil, true},
		{"127.0.0.1", nil, true},
		{"203.0.113.9", public, true}, // a neighbour of a public-addressed host
		{"198.51.100.1", public, false},
		{"8.8.8.8", public, false},
		{"8.8.8.8", nil, false},
		{"203.0.113.9", nil, false}, // no subnet found: private ranges only
	} {
		if got := mayAnswer(net.ParseIP(c.from), c.own); got != c.want {
			t.Errorf("mayAnswer(%s, %v) = %t, want %t", c.from, c.own, got, c.want)
		}
	}
	if mayAnswer(nil, public) {
		t.Error("an unparsed source was answered")
	}
}

// ownNet finds the subnet of an address this machine holds, and nothing for
// one it does not.
func TestOwnNetIsTheInterfaceSubnet(t *testing.T) {
	n := ownNet("127.0.0.1")
	if n == nil || !n.Contains(net.ParseIP("127.0.0.2")) {
		t.Fatalf("ownNet(127.0.0.1) = %v, want the loopback network", n)
	}
	if n := ownNet("198.51.100.77"); n != nil {
		t.Errorf("ownNet of an address nobody here holds = %v, want nil", n)
	}
}
