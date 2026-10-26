package tlscert

import (
	"net"
	"slices"
	"testing"
)

/*
 * Which addresses an invite is allowed to carry.
 *
 * An invite is recorded once at pairing and never re-learned (ADR 0044 §5), so
 * an address that expires becomes an entry that no longer exists and is still
 * tried — a connect timeout on every peer operation. A real peer record was
 * found holding three of them.
 *
 * The rules are asserted over two lists rather than against this machine: what
 * addresses a computer holds is unknowable from here and different on CI.
 * `TestShowThisMachinesAddresses` is the other half, run by hand.
 */
func TestAnInviteDropsWhatIsGoingToDisappear(t *testing.T) {
	const (
		dhcp      = "2001:db8:1:1::2e"
		stable    = "2001:db8:1:1:a51e:ee8c:6fb1:966b"
		temporary = "2001:db8:1:1:e98b:9995:5005:6453"
		lan       = "192.168.1.21"
	)
	all := []string{dhcp, stable, temporary, lan}

	got := without(all, map[string]bool{temporary: true})

	if slices.Contains(got, temporary) {
		t.Error("the temporary address survived into an invite")
	}
	for _, want := range []string{dhcp, stable, lan} {
		if !slices.Contains(got, want) {
			t.Errorf("%s was dropped and should have been kept", want)
		}
	}
}

/*
 * Anything unclassified is kept, which is the direction the mistake has to go.
 *
 * A stale entry costs a timeout. A missing one can cost the pairing — and on
 * every platform that is not Windows nothing is classified at all, so this is
 * the ordinary case rather than an edge.
 */
func TestNothingKnownMeansNothingDropped(t *testing.T) {
	all := []string{"192.168.1.21", "2001:db8:1:1::2e"}

	for _, skip := range []map[string]bool{nil, {}} {
		got := without(all, skip)
		if !slices.Equal(got, all) {
			t.Errorf("with %v known, got %v, want everything", skip, got)
		}
	}
}

/*
 * A machine whose every address is temporary still gets to introduce itself.
 *
 * Returning nothing would produce ErrNoAddress — "this server has no address
 * another machine could reach it on" — which is a far worse answer than an
 * address that expires in a few days.
 */
func TestAMachineIsNeverLeftWithNoAddress(t *testing.T) {
	all := []string{"2001:db8:1:1:e98b:9995:5005:6453"}

	got := without(all, map[string]bool{all[0]: true})

	if len(got) == 0 {
		t.Fatal("every address was dropped; this server cannot introduce itself")
	}
	if !slices.Equal(got, all) {
		t.Errorf("got %v, want the lot back", got)
	}
}

/*
 * The certificate is not narrowed.
 *
 * It covers whatever somebody might type, and a temporary address is real
 * today — somebody can be looking at it right now. Only what is *written down*
 * is filtered. Asserted as a property, so a later tidy-up that points
 * LoadOrGenerate at StableIPs fails here.
 */
func TestTheCertificateStillCoversEverything(t *testing.T) {
	all := LocalIPs()
	stable := StableIPs()

	if len(stable) > len(all) {
		t.Fatalf("StableIPs returned more than LocalIPs: %d > %d", len(stable), len(all))
	}
	for _, ip := range stable {
		if !slices.Contains(all, ip) {
			t.Errorf("%s is in the invite list and not in the certificate list", ip)
		}
	}
}

func mac(s string) net.HardwareAddr {
	m, err := net.ParseMAC(s)
	if err != nil {
		panic(err)
	}
	return m
}

func facts(ips ...string) []addrFacts {
	out := make([]addrFacts, 0, len(ips))
	for _, ip := range ips {
		out = append(out, addrFacts{IP: net.ParseIP(ip)})
	}
	return out
}

/*
 * Aither, as `Get-NetIPAddress` and `Get-NetAdapter` showed it on 2026-10-26.
 *
 * Its invite carried four addresses and one of them was 172.23.96.1 — the WSL
 * switch, a Hyper-V adapter (MAC 00-15-5D-…) with no gateway, on a network
 * whose only other member is the WSL VM. The two public IPv6 addresses are
 * both stable: ::2e is DHCPv6, and …a51e:ee8c:6fb1:966b is SuffixOrigin Link
 * (stable-privacy), unlike its seven Random siblings. Prefixes are real here
 * because the rule must work on exactly this table.
 */
func aither() []adapter {
	wifi := adapter{
		MAC:        mac("ac:12:03:d8:1d:be"),
		HasGateway: true,
		Addrs: []addrFacts{
			{IP: net.ParseIP("2600:1702:5ac7:9200::2e")},
			{IP: net.ParseIP("2600:1702:5ac7:9200:a51e:ee8c:6fb1:966b")},
			{IP: net.ParseIP("2600:1702:5ac7:9200:ccbe:8278:4fd4:12f6"), Temporary: true},
			{IP: net.ParseIP("2600:1702:5ac7:9200:ecec:a0a3:4e2c:c8ea"), Temporary: true, Deprecated: true},
			{IP: net.ParseIP("192.168.1.160")},
		},
	}
	wsl := adapter{
		MAC:   mac("00:15:5d:cb:75:9d"),
		Addrs: facts("172.23.96.1"),
	}
	return []adapter{wifi, wsl}
}

func TestAithersInviteListsOnlyWhatAnotherMachineCanReach(t *testing.T) {
	all := []string{
		"2600:1702:5ac7:9200::2e",
		"2600:1702:5ac7:9200:a51e:ee8c:6fb1:966b",
		"2600:1702:5ac7:9200:ccbe:8278:4fd4:12f6",
		"2600:1702:5ac7:9200:ecec:a0a3:4e2c:c8ea",
		"192.168.1.160",
		"172.23.96.1",
	}
	want := []string{
		"2600:1702:5ac7:9200::2e",
		"2600:1702:5ac7:9200:a51e:ee8c:6fb1:966b",
		"192.168.1.160",
	}

	got := without(all, unrecordable(aither()))

	if !slices.Equal(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

/*
 * Every hypervisor's host-side adapter, and the things that look like one and
 * must not be treated as one.
 */
func TestHostOnlyAdapters(t *testing.T) {
	cases := []struct {
		name string
		a    adapter
		drop bool
	}{
		{"WSL switch", adapter{MAC: mac("00:15:5d:cb:75:9d")}, true},
		{"Hyper-V Default Switch", adapter{MAC: mac("00:15:5d:01:02:03")}, true},
		{"VMware VMnet8 (NAT)", adapter{MAC: mac("00:50:56:c0:00:08")}, true},
		{"VirtualBox host-only", adapter{MAC: mac("0a:00:27:00:00:0c")}, true},

		// A Hyper-V *external* switch carries the house's own address on a
		// vEthernet adapter. It has the router behind it, so it stays.
		{"Hyper-V external switch", adapter{MAC: mac("00:15:5d:aa:bb:cc"), HasGateway: true}, false},

		// A VPN overlay is how a friend's server reaches this one. Virtual,
		// no hardware, no default gateway — and not a hypervisor's prefix.
		{"ZeroTier", adapter{MAC: mac("1a:2b:3c:4d:5e:6f")}, false},
		{"WireGuard / Tailscale (no MAC)", adapter{}, false},

		{"physical Wi-Fi", adapter{MAC: mac("ac:12:03:d8:1d:be"), HasGateway: true}, false},
		{"physical NIC with no gateway (static LAN)", adapter{MAC: mac("2c:f0:5d:86:29:97")}, false},
	}
	for _, c := range cases {
		if got := hostOnly(c.a); got != c.drop {
			t.Errorf("%s: hostOnly = %v, want %v", c.name, got, c.drop)
		}
	}
}

// A host-only adapter loses every address on it, IPv4 and IPv6 alike.
func TestAHostOnlyAdapterLosesAllItsAddresses(t *testing.T) {
	a := adapter{MAC: mac("00:15:5d:01:02:03"), Addrs: facts("172.29.112.1", "fd00:abcd::1")}

	skip := unrecordable([]adapter{a})

	for _, ip := range []string{"172.29.112.1", "fd00:abcd::1"} {
		if !skip[ip] {
			t.Errorf("%s on a host-only adapter was kept", ip)
		}
	}
}

// Temporary and deprecated addresses are dropped wherever they live; the
// stable addresses beside them are not.
func TestRotatingAddressesAreDroppedOnAnyAdapter(t *testing.T) {
	a := adapter{
		MAC:        mac("1a:2b:3c:4d:5e:6f"),
		HasGateway: true,
		Addrs: []addrFacts{
			{IP: net.ParseIP("2001:db8::1")},
			{IP: net.ParseIP("2001:db8::2"), Temporary: true},
			{IP: net.ParseIP("2001:db8::3"), Deprecated: true},
		},
	}

	skip := unrecordable([]adapter{a})

	if skip["2001:db8::1"] || !skip["2001:db8::2"] || !skip["2001:db8::3"] {
		t.Errorf("got %v, want only ::2 and ::3", skip)
	}
}

// No facts — every platform but Windows — drops nothing.
func TestNoFactsDropsNothing(t *testing.T) {
	if got := unrecordable(nil); len(got) != 0 {
		t.Errorf("got %v from no facts", got)
	}
}
