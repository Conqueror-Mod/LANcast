package peer

import (
	"context"
	"crypto/tls"
	"net"
	"time"
)

/*
 * Reach finds which of a peer's addresses answers, by racing them.
 *
 * A peer records several addresses (ADR 0044 §5) and they used to be tried one
 * after another inside one short budget. That fails completely, not slowly,
 * when the first address is dead in the way that never refuses — an interface
 * the peer has since left, such as a VPN that was switched off. The connection
 * attempt hangs until the budget is spent, and the address that would have
 * answered in ten milliseconds is reached with no time left. Measured on
 * 2026-10-09: two servers on one LAN, each holding the other's old ZeroTier
 * address first, unable to see each other for nine days while every LAN
 * address answered.
 *
 * So the addresses race, as Happy Eyeballs (RFC 8305) races a host's
 * addresses: the first in `addrs` starts at once, each next one `stagger`
 * later, and the first to complete a **pinned TLS handshake** wins. The
 * handshake, not merely a TCP connect, because a machine that accepts and then
 * says nothing hangs just as surely, and because the pin is what proves the
 * address is the peer and not whatever holds that address now.
 *
 * Only the order changes. The request itself is still sent to one address at
 * a time by the caller, because some peer calls are POSTs and must not happen
 * twice. Returns `addrs` reordered with the winner first, or unchanged when
 * nothing answered in time — the caller then fails as it always did.
 */
func Reach(ctx context.Context, cfg *tls.Config, addrs []string, stagger time.Duration) []string {
	if len(addrs) < 2 || cfg == nil {
		return addrs
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Every attempt reports, success or not, so a peer whose addresses all
	// refuse at once is known at once rather than after the whole budget.
	type result struct {
		i  int
		ok bool
	}
	results := make(chan result, len(addrs))
	for i, addr := range addrs {
		go func(i int, addr string) {
			if i > 0 {
				select {
				case <-time.After(time.Duration(i) * stagger):
				case <-ctx.Done():
					results <- result{i, false}
					return
				}
			}
			d := tls.Dialer{NetDialer: &net.Dialer{}, Config: cfg}
			conn, err := d.DialContext(ctx, "tcp", addr)
			if err == nil {
				conn.Close()
			}
			results <- result{i, err == nil}
		}(i, addr)
	}

	for range addrs {
		select {
		case r := <-results:
			if !r.ok {
				continue
			}
			if r.i == 0 {
				return addrs
			}
			out := make([]string, 0, len(addrs))
			out = append(out, addrs[r.i])
			out = append(out, addrs[:r.i]...)
			return append(out, addrs[r.i+1:]...)
		case <-ctx.Done():
			return addrs
		}
	}
	return addrs
}
