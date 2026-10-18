//go:build windows

package dlna

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

/*
 * listenSSDP opens the discovery socket on Windows.
 *
 * Bound to the LAN address itself (ip:1900), sharing the port, and joined to
 * the group on that interface. net.ListenMulticastUDP binds the wildcard
 * 0.0.0.0:1900 instead, and on Windows that socket received nothing at all —
 * measured on Aither: not one search in its lifetime, while a socket bound to
 * 192.168.1.20:1900 received Windows' own searches and another device's
 * within seconds. Windows' SSDP service and qBittorrent bind the same way,
 * which is the convention this follows.
 */
func listenSSDP(ip string) (*net.UDPConn, error) {
	addr := net.ParseIP(ip).To4()
	if addr == nil {
		return nil, fmt.Errorf("ssdp: %q is not an IPv4 address", ip)
	}
	lc := net.ListenConfig{Control: func(network, address string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) {
			serr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
		}); err != nil {
			return err
		}
		return serr
	}}
	pc, err := lc.ListenPacket(context.Background(), "udp4", net.JoinHostPort(ip, "1900"))
	if err != nil {
		return nil, err
	}
	conn := pc.(*net.UDPConn)
	raw, err := conn.SyscallConn()
	if err != nil {
		conn.Close()
		return nil, err
	}
	var jerr error
	if err := raw.Control(func(fd uintptr) {
		mreq := windows.IPMreq{Multiaddr: [4]byte{239, 255, 255, 250}}
		copy(mreq.Interface[:], addr)
		jerr = windows.Setsockopt(windows.Handle(fd), windows.IPPROTO_IP, windows.IP_ADD_MEMBERSHIP,
			(*byte)(unsafe.Pointer(&mreq)), int32(unsafe.Sizeof(mreq)))
	}); err != nil {
		conn.Close()
		return nil, err
	}
	if jerr != nil {
		conn.Close()
		return nil, fmt.Errorf("ssdp: join group: %w", jerr)
	}
	return conn, nil
}
