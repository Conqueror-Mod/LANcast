// Package netguard makes an HTTP client that will not reach the machine it is
// running on, or the network that machine sits in.
//
// It exists for one shape of request: a fetch whose URL came from somewhere
// less trusted than LANcast's own code. A plugin asking the host to fetch
// something, and — once a provider can be a plugin — an artwork URL the host
// downloads on a record's say-so.
//
// # Why a name check is not enough
//
// The plugin capability model grants outbound access by *hostname*: a manifest
// lists `api.example.com` and the host compares the URL's host against it. That
// is the right shape and it is not sufficient on its own, because a hostname is
// not a destination. Whoever owns `api.example.com` also owns what it resolves
// to, and can point it at `127.0.0.1` — at which point an allowlist that
// matched the string has approved a connection to the server itself.
//
// The person installing the plugin sees `api.example.com` in the grant dialog
// and agrees to something reasonable. They are not shown an A record, and it
// can change after they agree.
//
// # So the check is on the address, after resolution
//
// The guard runs in the dialer's Control hook, which fires once the name has
// been resolved and immediately before the socket connects. That placement is
// the whole point: checking earlier means checking a name, and a name can lie
// or change between the check and the connection. Every address the resolver
// returns is checked, so a host answering with one public and one private
// address cannot slip through on a retry.
//
// # What this is not
//
// Not a replacement for the allowlist. The allowlist says which *services* a
// plugin may talk to, which is a question about trust; this says which
// *addresses* anything may be reached at, which is a question about the network.
// A plugin still needs both.
//
// And not a defence against a hostile public service — a plugin granted a host
// can still talk to it, which is what being granted means.
package netguard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// ErrBlocked is returned when a connection would reach a private or local
// address. It carries the address rather than only the name, because the name
// is the part that looked fine.
type ErrBlocked struct{ Addr string }

func (e ErrBlocked) Error() string {
	return fmt.Sprintf("refused to connect to %s: private and local addresses are not reachable from here", e.Addr)
}

/*
 * blocked reports whether an address is one this process must not reach on
 * somebody else's behalf.
 *
 * Loopback and the private ranges are the obvious half. The rest are the ones
 * that get forgotten and are exactly where the interesting targets live:
 *
 *   - link-local unicast, 169.254.0.0/16, which holds the cloud metadata
 *     endpoint at 169.254.169.254 — the single most valuable address to reach
 *     on a hosted machine, and the reason this category is not merely tidy.
 *   - unique local addresses, fc00::/7, the IPv6 equivalent of a private range.
 *   - the unspecified address, which some stacks route to loopback.
 *   - anything that is not a global unicast address at all: multicast,
 *     broadcast, and the reserved space nothing should be dialling.
 *
 * IPv6 is checked on its own terms rather than by mapping to IPv4, because
 * ::1 and fc00::/7 have no v4 spelling and a v4-only check would pass them.
 */
func blocked(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() {
		return true
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	// Everything left that is not ordinary routable internet: multicast,
	// broadcast, and the reserved blocks. Checked last so the categories above
	// keep their own names in the code.
	return !ip.IsGlobalUnicast()
}

// Dialer returns a dialer that refuses private and local addresses.
func Dialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		/*
		 * Control runs after the address is resolved and before connect(2), for
		 * every address tried. Returning an error here fails that attempt.
		 *
		 * This is the only hook with both facts available — the resolved
		 * address, and the chance to refuse. A check on the URL happens too
		 * early to know where the name points; a check on the response happens
		 * after the request has already been delivered.
		 */
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return ErrBlocked{Addr: address}
			}
			ip := net.ParseIP(host)
			if blocked(ip) {
				return ErrBlocked{Addr: address}
			}
			return nil
		},
	}
}

// Client returns an HTTP client that will not reach a private or local address,
// with the given overall timeout.
func Client(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:           Dialer().DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
			// A redirect is a fresh dial, so it goes through the same guard —
			// which is what stops a public URL redirecting to 127.0.0.1.
			MaxIdleConnsPerHost: 4,
		},
	}
}

// DialContext is the guarded dial, for callers that already have a transport.
func DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return Dialer().DialContext(ctx, network, addr)
}
