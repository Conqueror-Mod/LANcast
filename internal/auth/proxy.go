package auth

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

/*
 * Trusted proxies: who a request is from, when a reverse proxy sits in front.
 *
 * Everything that asks "who is this" reads RemoteAddr — ClientKey for the
 * per-address throttle, InsideNetwork for the per-account backoff's exemption,
 * the sign-in page's home-network check, the request log, a session's device
 * address. Behind a reverse proxy RemoteAddr is the proxy, and with the proxy
 * on the same machine that made **every caller loopback**: the backoff never
 * applied, the sign-in page showed account names to the internet, and every
 * failed password counted against one shared address.
 *
 * Reading X-Forwarded-For from anybody would be worse than not reading it:
 * the header is whatever the caller types, and "I am 127.0.0.1" is the first
 * thing an attacker would try. So it is read only from addresses the owner
 * has named, and the list is empty by default, which is exactly the old
 * behaviour.
 *
 * Done once, at the edge, by rewriting RemoteAddr, rather than in each of the
 * places above. Teaching five call sites to look through a proxy is five
 * chances for one of them to forget, and the one that forgets is a bypass.
 */

// ErrBadProxy reports an entry that is not an address or a network.
var ErrBadProxy = errors.New("not an IP address or CIDR network")

// ErrProxyTooWide reports a network so wide that trusting it would mean
// trusting what anybody on the internet says about themselves.
var ErrProxyTooWide = errors.New("network too wide to trust")

// MaxTrustedProxies bounds the list. A household has one proxy, perhaps two.
const MaxTrustedProxies = 32

/*
 * ParseTrustedProxies turns the owner's list into networks, and normalises it.
 *
 * A bare address becomes a /32 or /128. The normalised form is returned
 * alongside, so what is stored and shown is what is enforced: "10.0.0.1" and
 * "10.0.0.1/32" are one entry, not two spellings of it.
 *
 * Refused: anything that does not parse, and any network wider than a /8 or
 * an IPv6 /16. "Trust everybody" is not a proxy configuration, it is turning
 * the check off while appearing to have configured it.
 */
func ParseTrustedProxies(entries []string) (nets []*net.IPNet, normal []string, err error) {
	if len(entries) > MaxTrustedProxies {
		return nil, nil, fmt.Errorf("at most %d trusted proxies", MaxTrustedProxies)
	}
	seen := map[string]bool{}
	for _, raw := range entries {
		e := strings.TrimSpace(raw)
		if e == "" {
			continue
		}
		var n *net.IPNet
		if strings.Contains(e, "/") {
			_, parsed, perr := net.ParseCIDR(e)
			if perr != nil {
				return nil, nil, fmt.Errorf("%q: %w", e, ErrBadProxy)
			}
			n = parsed
		} else {
			ip := net.ParseIP(e)
			if ip == nil {
				return nil, nil, fmt.Errorf("%q: %w", e, ErrBadProxy)
			}
			bits := 128
			if v4 := ip.To4(); v4 != nil {
				ip, bits = v4, 32
			}
			n = &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}
		}
		ones, bits := n.Mask.Size()
		if (bits == 32 && ones < 8) || (bits == 128 && ones < 16) {
			return nil, nil, fmt.Errorf("%q: %w", e, ErrProxyTooWide)
		}
		s := n.String()
		if seen[s] {
			continue
		}
		seen[s] = true
		nets = append(nets, n)
		normal = append(normal, s)
	}
	return nets, normal, nil
}

func trusted(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

/*
 * ForwardedClient is the address a request is from, looking through trusted
 * proxies, and whether it differs from RemoteAddr.
 *
 * X-Forwarded-For is read **right to left**: each proxy appends the address it
 * received the request from, so the rightmost entries are the ones written by
 * proxies the owner runs, and anything further left was written by whoever
 * sent the request. The first entry, from the right, that is not itself a
 * trusted proxy is the client. Reading the leftmost entry instead — the
 * common mistake — believes the one value the caller chose.
 *
 * Every X-Forwarded-For line counts, in order, because a proxy may add a
 * second header rather than extend the first.
 *
 * A trusted proxy that sends no usable header leaves the request as it was.
 * That is the old behaviour, and a misconfigured proxy is no worse off than
 * before the list existed.
 */
func ForwardedClient(r *http.Request, nets []*net.IPNet) (string, bool) {
	if len(nets) == 0 {
		return "", false
	}
	peer := net.ParseIP(ClientKey(r))
	if peer == nil || !trusted(peer, nets) {
		return "", false
	}
	var hops []string
	for _, line := range r.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(line, ",") {
			hops = append(hops, strings.TrimSpace(part))
		}
	}
	var client net.IP
	for i := len(hops) - 1; i >= 0; i-- {
		ip := parseHop(hops[i])
		if ip == nil {
			// Garbage in the chain. Everything left of it is untrustworthy
			// too, so stop at the last good hop rather than skip past it.
			break
		}
		client = ip
		if !trusted(ip, nets) {
			break
		}
	}
	if client == nil {
		return "", false
	}
	return client.String(), true
}

// parseHop reads one entry, tolerating the port and brackets some proxies add.
func parseHop(h string) net.IP {
	if ip := net.ParseIP(h); ip != nil {
		return ip
	}
	if host, _, err := net.SplitHostPort(h); err == nil {
		return net.ParseIP(host)
	}
	return net.ParseIP(strings.Trim(h, "[]"))
}

/*
 * TrustProxies is the middleware: when the request came through a trusted
 * proxy, the handlers inside see the client's address as RemoteAddr.
 *
 * The list is read per request through nets, so a change in Settings applies
 * to the next request with no restart. The port is zero because the client's
 * port is not known; nothing in LANcast reads it.
 */
func TrustProxies(nets func() []*net.IPNet, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if client, ok := ForwardedClient(r, nets()); ok {
			r2 := new(http.Request)
			*r2 = *r
			r2.RemoteAddr = net.JoinHostPort(client, "0")
			r = r2
		}
		next.ServeHTTP(w, r)
	})
}
