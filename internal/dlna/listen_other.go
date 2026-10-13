//go:build !windows

package dlna

import "net"

// listenSSDP opens the discovery socket everywhere but Windows, joined on the
// interface that holds ip so a machine with several networks listens on the
// LAN. Linux delivers multicast to a wildcard-bound member, which is what
// net.ListenMulticastUDP gives; Windows does not (listen_windows.go).
func listenSSDP(ip string) (*net.UDPConn, error) {
	group, err := net.ResolveUDPAddr("udp4", MulticastAddr)
	if err != nil {
		return nil, err
	}
	return net.ListenMulticastUDP("udp4", interfaceWith(ip), group)
}

// interfaceWith is the interface holding ip, or nil (the system's choice).
func interfaceWith(ip string) *net.Interface {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for i := range ifs {
		addrs, _ := ifs[i].Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.String() == ip {
				return &ifs[i]
			}
		}
	}
	return nil
}
