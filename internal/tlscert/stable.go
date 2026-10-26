package tlscert

import (
	"bytes"
	"net"
)

/*
 * Which of this machine's addresses are worth writing down — the decision,
 * kept apart from the machine it is about.
 *
 * The platform files only *report*: what each adapter is and what the stack
 * says about each of its addresses. Everything below is a function over those
 * facts, so the rules are asserted against fixtures (Aither's real adapter
 * table among them) rather than against whatever the computer running the
 * tests happens to hold — which is unknowable and different on CI. The same
 * split keeps probe.ParseJSON pure.
 *
 * Two kinds of address are left out of anything recorded:
 *
 *  1. **Ones the stack means to replace.** A temporary IPv6 address (RFC 4941)
 *     is regenerated regularly and gone within a week; a deprecated one is on
 *     its way out whatever its origin.
 *
 *  2. **Ones only this machine can reach.** A hypervisor's host-side adapter —
 *     the WSL switch, Hyper-V's Default Switch, VMware's VMnet1/8, VirtualBox's
 *     host-only network — puts this machine on a network whose only other
 *     members are its own virtual machines. Its address is real, answers, and
 *     no other computer can route to it. On Aither the WSL switch put
 *     172.23.96.1 into every invite, and WSL picks that subnet afresh when it
 *     builds the network, so it is also an address that moves.
 */

// adapter is what the stack reports about one network interface.
type adapter struct {
	// MAC is the interface's link-layer address; empty for tunnels that have
	// none.
	MAC net.HardwareAddr
	// HasGateway is whether the stack has a default gateway on this interface
	// for either family — whether anything beyond this link is reached
	// through it.
	HasGateway bool
	Addrs      []addrFacts
}

// addrFacts is what the stack reports about one unicast address.
type addrFacts struct {
	IP net.IP
	// Temporary is an RFC 4941 privacy address: generated to be thrown away.
	// Windows records it as SuffixOrigin Random. Its stable-privacy sibling
	// (SuffixOrigin Link) is equally random-looking and is NOT temporary.
	Temporary bool
	// Deprecated is still assigned but no longer chosen by the stack.
	Deprecated bool
}

/*
 * hypervisorHostOUIs are the vendor prefixes hypervisors give the adapter they
 * create on the *host* side of a virtual network.
 *
 * This is the property that separates a host-only switch from a VPN. Both are
 * virtual, both lack a hardware interface, both usually lack a default gateway
 * — and a VPN overlay (ZeroTier, Tailscale, WireGuard) is precisely how a
 * friend's server reaches this one, so "virtual and no gateway" would throw
 * away the address that matters most to the people using it. A VPN's adapter
 * does not carry a hypervisor's prefix; the WSL switch's does.
 *
 * Each one is a registered OUI, not a guess about a range of IP addresses.
 */
var hypervisorHostOUIs = [][]byte{
	{0x00, 0x15, 0x5d}, // Microsoft Hyper-V — WSL, Default Switch, internal switches
	{0x00, 0x50, 0x56}, // VMware — VMnet1 (host-only), VMnet8 (NAT)
	{0x0a, 0x00, 0x27}, // VirtualBox host-only adapter
}

/*
 * hostOnly is whether an adapter connects this machine only to its own guests.
 *
 * Both conditions are required. A hypervisor prefix alone is not enough: a
 * Hyper-V *external* switch moves the host's real LAN address onto a vEthernet
 * adapter, and that adapter can carry a Hyper-V MAC while being the very
 * interface the house routes through. What tells them apart is the gateway — an
 * external switch has the router behind it, a host-only one has nothing,
 * because on that network this machine *is* the router.
 */
func hostOnly(a adapter) bool {
	if a.HasGateway || len(a.MAC) < 3 {
		return false
	}
	for _, oui := range hypervisorHostOUIs {
		if bytes.Equal(a.MAC[:3], oui) {
			return true
		}
	}
	return false
}

/*
 * unrecordable returns the addresses an invite must not carry.
 *
 * Anything absent from the set is kept — including every address on a platform
 * that reports no facts. A stale entry costs a timeout (and since #804 not even
 * that, because addresses are raced); a missing one can cost the pairing. The
 * cheaper mistake is the one to make.
 */
func unrecordable(adapters []adapter) map[string]bool {
	out := map[string]bool{}
	for _, a := range adapters {
		skipAll := hostOnly(a)
		for _, f := range a.Addrs {
			if f.IP == nil {
				continue
			}
			if skipAll || f.Temporary || f.Deprecated {
				out[f.IP.String()] = true
			}
		}
	}
	return out
}

/*
 * without is the filter, over two lists.
 */
func without(all []string, skip map[string]bool) []string {
	if len(skip) == 0 {
		return all
	}
	out := make([]string, 0, len(all))
	for _, ip := range all {
		if !skip[ip] {
			out = append(out, ip)
		}
	}
	/*
	 * Never nothing.
	 *
	 * If every address this machine has is one that is going away or that only
	 * this machine can reach, a server with no address at all cannot introduce
	 * itself — and ErrNoAddress is a much worse answer than an address that
	 * might not work. Keeping the lot is the honest fallback.
	 */
	if len(out) == 0 {
		return all
	}
	return out
}
