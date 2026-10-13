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
	listen, err := net.ListenMulticastUDP("udp4", nil, group)
	if err != nil {
		return err
	}
	defer listen.Close()
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
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			announce(true)
		default:
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
			return err
		}
		s, ok := ParseSearch(buf[:n])
		if !ok {
			continue
		}
		host := localIPFor(from)
		if host == "" {
			continue
		}
		dd := at(host)
		for _, target := range dd.Answers(s) {
			if _, err := send.WriteToUDP(dd.SearchResponse(target), from); err != nil {
				log.Debug("dlna: reply", "to", from, "error", err)
			}
		}
	}
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
