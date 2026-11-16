package dlna

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"time"
)

/*
 * Announce runs discovery until ctx ends: it answers searches on the SSDP
 * multicast group, announces the device when it starts and every MaxAge/3
 * after, and says goodbye on the way out so a TV drops the server at once.
 *
 * device.Location is a template: each message is given the address of this
 * machine that the other side can actually reach. A reply carries the local
 * address the OS would route to the searcher, and an announcement carries
 * the address chosen by preferredIP. A machine with two networks would
 * otherwise send a TV a URL on the network it is not on.
 */
func Announce(ctx context.Context, d Device, port int, preferredIP func() string, log *slog.Logger) error {
	group, err := net.ResolveUDPAddr("udp4", MulticastAddr)
	if err != nil {
		return err
	}
	// Listen on the LAN address: the one announcements carry, and the one a
	// TV on the same network searches across (listenSSDP says why it is bound
	// to it rather than to every address).
	lan := preferredIP()
	if lan == "" {
		return errors.New("ssdp: this machine has no LAN address to listen on")
	}
	listen, err := listenSSDP(lan)
	if err != nil {
		return err
	}
	defer func() { listen.Close() }()
	own := ownNet(lan)
	checked := time.Now()
	// moved re-binds discovery when preferredIP no longer gives the address
	// it is bound to, and reports whether it did.
	moved := func() bool {
		now := preferredIP()
		if now == "" || now == lan {
			return false
		}
		next, err := listenSSDP(now)
		if err != nil {
			log.Warn("dlna: move discovery to the new address", "address", now, "error", err)
			return false
		}
		listen.Close()
		listen, lan, own = next, now, ownNet(now)
		log.Info("dlna: discovery moved", "address", lan)
		return true
	}
	send, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return err
	}
	defer send.Close()

	at := func(host string) Device {
		dd := d
		dd.Location = "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/dlna/device.xml"
		return dd
	}
	announce := func(alive bool) {
		host := preferredIP()
		if host == "" {
			return
		}
		for _, msg := range at(host).Notifies(alive) {
			if _, err := send.WriteToUDP(msg, group); err != nil {
				log.Debug("dlna: announce", "error", err)
			}
		}
	}

	announce(true)
	defer announce(false)
	tick := time.NewTicker(time.Duration(MaxAge/3) * time.Second)
	defer tick.Stop()

	buf := make([]byte, 4096)
	quiet := newSeenOnce(256)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			announce(true)
		default:
		}
		// Follow the address. Announcements already carry preferredIP's
		// answer of the moment; without this, the socket stayed on the
		// address it was bound to at start and, after a DHCP change, heard
		// no searches until DLNA was switched off and on (R10-8).
		if time.Since(checked) >= rebindEvery {
			checked = time.Now()
			moved()
		}
		_ = listen.SetReadDeadline(time.Now().Add(time.Second))
		n, from, err := listen.ReadFromUDP(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			if ctx.Err() != nil {
				return nil
			}
			// A socket on an address that has gone away fails here; one on
			// an address that is still ours has really failed.
			if moved() {
				continue
			}
			return err
		}
		s, ok := ParseSearch(buf[:n])
		if !ok {
			continue
		}
		// Only the local network is answered, as on the HTTP side (ADR 0086
		// §4). A search from anywhere else is either from a network that
		// cannot fetch from us anyway or spoofed, and five answers to a
		// hundred-byte search is a small reflector pointed at whoever the
		// spoofed address belongs to (R10-8).
		var host string
		if mayAnswer(from.IP, own) {
			host = localIPFor(from)
		}
		var answers []string
		var dd Device
		if host != "" {
			dd = at(host)
			answers = dd.Answers(s)
		}
		/*
		 * At debug, so "is the TV's search reaching us at all" is answered by
		 * the log rather than by guessing at the firewall.
		 *
		 * A search we answer is always logged. One we do not is logged the
		 * first time each sender asks for each target, and then left out:
		 * on Aither (2026-11-08) a router asking every second for an internet
		 * gateway, which LANcast is not, was a quarter of lancastd.log, and
		 * pushed the evidence for two open reports out of it.
		 */
		if len(answers) > 0 || quiet.first(from.IP.String()+" "+s.Target) {
			log.Debug("dlna: search", "from", from.String(), "target", s.Target,
				"answered", len(answers) > 0)
		}
		if host == "" {
			continue
		}
		for _, target := range answers {
			if _, err := send.WriteToUDP(dd.SearchResponse(target), from); err != nil {
				log.Debug("dlna: reply", "to", from, "error", err)
			}
		}
	}
}

// rebindEvery is how often discovery checks that it is still listening on
// the address preferredIP gives.
const rebindEvery = 30 * time.Second

/*
 * mayAnswer reports whether a search from ip is one to answer: from a
 * private, loopback or link-local address, or from the network the discovery
 * socket is on.
 *
 * The last is for a machine whose LAN address is itself public, as on a host
 * given a static public address: its neighbours share its subnet without
 * sharing a private range. own is nil when that network could not be found,
 * and then only the private ranges are answered.
 */
func mayAnswer(ip net.IP, own *net.IPNet) bool {
	if ip == nil {
		return false
	}
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return true
	}
	return own != nil && own.Contains(ip)
}

// ownNet is the network of the interface holding ip, or nil if no interface
// does.
func ownNet(ip string) *net.IPNet {
	want := net.ParseIP(ip)
	if want == nil {
		return nil
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.Equal(want) {
			return &net.IPNet{IP: n.IP.Mask(n.Mask), Mask: n.Mask}
		}
	}
	return nil
}

// localIPFor is the address of this machine that would be used to reach
// remote: a connected UDP socket sends nothing, but the OS picks its route
// and so its local address.
func localIPFor(remote *net.UDPAddr) string {
	c, err := net.DialUDP("udp4", nil, remote)
	if err != nil {
		return ""
	}
	defer c.Close()
	if a, ok := c.LocalAddr().(*net.UDPAddr); ok {
		return a.IP.String()
	}
	return ""
}

/*
 * seenOnce remembers which unanswered searches have been logged, so each is
 * logged once rather than every second. Bounded: past its size it forgets
 * everything and starts again, which logs a repeat now and then rather than
 * growing without end on a busy network.
 */
type seenOnce struct {
	max  int
	seen map[string]bool
}

func newSeenOnce(max int) *seenOnce {
	return &seenOnce{max: max, seen: map[string]bool{}}
}

// first reports whether key has not been seen since the last reset, and
// marks it seen.
func (q *seenOnce) first(key string) bool {
	if q.seen[key] {
		return false
	}
	if len(q.seen) >= q.max {
		q.seen = map[string]bool{}
	}
	q.seen[key] = true
	return true
}
