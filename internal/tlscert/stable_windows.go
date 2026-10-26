//go:build windows

package tlscert

import (
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

/*
 * What Windows says about this machine's adapters — facts only. Which of them
 * an invite may carry is decided in stable.go, where it is tested.
 *
 * # Why this asks Windows instead of reading the bits
 *
 * There is no way to tell a temporary IPv6 address from a stable one by
 * looking at it. RFC 7217 stable-privacy addresses are *also* random-looking
 * and *also* SLAAC, and they are exactly the ones that must be kept. Only the
 * stack that generated them knows the difference, and it records it as
 * `SuffixOrigin`: Random is the temporary one, Link the stable-privacy one.
 * (On Aither, 2600:…:a51e:ee8c:6fb1:966b is Link — stable for as long as the
 * prefix is — while seven Random siblings rotate beside it.)
 *
 * Likewise, whether an adapter has a router behind it is the stack's routing
 * state, so the gateway list is asked for rather than inferred from addresses.
 *
 * # It fails open
 *
 * If the table cannot be read, nothing is reported and everything is kept.
 */
func adapterFacts() []adapter {
	var size uint32
	const flags = windows.GAA_FLAG_SKIP_ANYCAST |
		windows.GAA_FLAG_SKIP_MULTICAST |
		windows.GAA_FLAG_SKIP_DNS_SERVER |
		windows.GAA_FLAG_SKIP_FRIENDLY_NAME |
		windows.GAA_FLAG_INCLUDE_GATEWAYS

	// The buffer is grown on demand: the table's size depends on how many
	// interfaces and addresses exist, and asking twice is the documented way.
	err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, nil, &size)
	if err != windows.ERROR_BUFFER_OVERFLOW || size == 0 {
		return nil
	}
	buf := make([]byte, size)
	first := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
	if err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, first, &size); err != nil {
		return nil
	}

	var out []adapter
	for a := first; a != nil; a = a.Next {
		n := int(a.PhysicalAddressLength)
		if n > len(a.PhysicalAddress) {
			n = len(a.PhysicalAddress)
		}
		ad := adapter{
			MAC:        append(net.HardwareAddr(nil), a.PhysicalAddress[:n]...),
			HasGateway: a.FirstGatewayAddress != nil,
		}
		for u := a.FirstUnicastAddress; u != nil; u = u.Next {
			// SocketAddress.IP returns nil for anything that is not an
			// IPv4 or IPv6 address, which is the only case there is here.
			ip := u.Address.IP()
			if ip == nil {
				continue
			}
			ad.Addrs = append(ad.Addrs, addrFacts{
				IP:         ip,
				Temporary:  u.SuffixOrigin == windows.IpSuffixOriginRandom,
				Deprecated: u.DadState == windows.IpDadStateDeprecated,
			})
		}
		out = append(out, ad)
	}
	return out
}
