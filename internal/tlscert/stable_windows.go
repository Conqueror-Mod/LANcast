//go:build windows

package tlscert

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

/*
 * Which of this machine's addresses are worth writing down.
 *
 * Windows keeps more than one global IPv6 address per interface, and they do
 * not all mean the same thing. A **temporary** address (RFC 4941) exists to
 * make outbound traffic hard to correlate: a new one is generated regularly,
 * the old one is deprecated within about a day and gone within a week.
 *
 * That is fine for a certificate, which covers whatever somebody might type,
 * and wrong for an **invite** — which is recorded once at pairing and, today,
 * never re-learned ([ADR 0044](../../docs/adr/0044-server-identity-and-peering.md)
 * §5). An invite carrying a temporary address accumulates an entry that stops
 * existing while still being tried, and every attempt on it costs a connect
 * timeout before a working address is reached.
 *
 * # Why this asks Windows instead of reading the bits
 *
 * There is no way to tell a temporary address from a stable one by looking at
 * it. RFC 7217 stable-privacy addresses are *also* random-looking and *also*
 * SLAAC, and they are exactly the ones that must be kept. The only thing that
 * knows the difference is the stack that generated them, which records it as
 * `SuffixOrigin` — so that is what is asked.
 *
 * # It fails open
 *
 * Anything this cannot classify is **kept**. A slightly stale invite costs a
 * timeout; an invite missing the only reachable address costs the pairing. The
 * cheaper mistake is the one to make.
 */

// temporaryAddresses returns the addresses Windows says were generated to be
// thrown away. Everything else — including addresses this could not ask about —
// is absent from the set, and so is kept by the caller.
func temporaryAddresses() map[string]bool {
	out := map[string]bool{}

	// The buffer is grown on demand: the table's size depends on how many
	// interfaces and addresses exist, and asking twice is the documented way.
	var size uint32
	const flags = windows.GAA_FLAG_SKIP_ANYCAST |
		windows.GAA_FLAG_SKIP_MULTICAST |
		windows.GAA_FLAG_SKIP_DNS_SERVER |
		windows.GAA_FLAG_SKIP_FRIENDLY_NAME
	err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, nil, &size)
	if err != windows.ERROR_BUFFER_OVERFLOW || size == 0 {
		return out
	}
	buf := make([]byte, size)
	adapters := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
	if err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, adapters, &size); err != nil {
		return out
	}

	for a := adapters; a != nil; a = a.Next {
		for u := a.FirstUnicastAddress; u != nil; u = u.Next {
			/*
			 * Random is the temporary one. It is deliberately the *only* value
			 * excluded: Dhcp and LinkLayerAddress are stable by construction,
			 * Manual is somebody's decision, and WellKnown and Other are not
			 * ours to second-guess.
			 *
			 * A deprecated address is also dropped, whatever its origin. It is
			 * still assigned and still answers, but the stack has stopped
			 * choosing it and it is on its way out — which is the same reason
			 * not to write it into something kept for months.
			 */
			temporary := u.SuffixOrigin == windows.IpSuffixOriginRandom
			dying := u.DadState == windows.IpDadStateDeprecated
			if !temporary && !dying {
				continue
			}
			// SocketAddress.IP returns nil for anything that is not an
			// IPv4 or IPv6 address, which is the only case there is here.
			if ip := u.Address.IP(); ip != nil {
				out[ip.String()] = true
			}
		}
	}
	return out
}
