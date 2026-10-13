package api

import (
	"net"
	"net/http"
	"sort"

	"lancast/internal/tlscert"
)

/*
 * Server › Network (docs/settings-plan.md §2, "Network"): this machine and the
 * LAN, as a status. Read-only by design. Whether the server answers the LAN is
 * decided by the loopback rule — no password, no LAN — and that gate is not a
 * toggle (CLAUDE.md); this only says which side of it the server is on, and
 * where somebody on the network would find it.
 *
 * Admin only: it lists this machine's addresses.
 */

// networkStatus is what the Network pane shows.
type networkStatus struct {
	// Port is the one this server listens on.
	Port string `json:"port"`
	// LANEnabled is whether other machines can reach it at all.
	LANEnabled bool `json:"lan_enabled"`
	// Secured is whether a password is set: the condition for LANEnabled.
	Secured bool `json:"secured"`
	// RestartRequired is a password set since starting, so the LAN opens on
	// the next restart rather than now.
	RestartRequired bool `json:"restart_required"`
	// Addresses are the URLs this machine answers on from the network: its
	// stable addresses, the same ones an invite to a friend names. Empty when
	// the server is loopback-only.
	Addresses []string `json:"addresses"`
	// CertificateFingerprint is the SPKI pin of the certificate being served,
	// for somebody checking that what their device trusts is this server.
	CertificateFingerprint string `json:"certificate_fingerprint"`
	// CustomCertificate is whether that certificate is one the operator
	// supplied rather than the one LANcast generated.
	CustomCertificate bool `json:"custom_certificate"`
}

func (s *Server) networkStatus(w http.ResponseWriter, r *http.Request) {
	_, port, _ := net.SplitHostPort(s.listenAddr)
	out := networkStatus{
		Port:                   port,
		LANEnabled:             s.lanBound,
		Secured:                s.secured(r.Context()),
		RestartRequired:        s.restartWidens,
		Addresses:              []string{},
		CertificateFingerprint: s.certFingerprint(),
		CustomCertificate:      s.settings.Get().CustomTLS(),
	}
	// A LAN-bound server serves HTTPS (the TLS work); a loopback-only one has
	// no address anybody else could use, so it lists none.
	if s.lanBound && port != "" {
		for _, ip := range homeFirst(tlscert.StableIPs()) {
			out.Addresses = append(out.Addresses, "https://"+net.JoinHostPort(ip, port))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

/*
 * homeFirst orders addresses so the one the pane leads with is the one a
 * phone in the same house would use.
 *
 * The system's own order is arbitrary, and the first time this pane was
 * looked at it led with a ZeroTier address (10.x) while the house's
 * 192.168.1.x came last. 192.168/16 is what nearly every home router hands
 * out, so it goes first; then the other private IPv4 ranges, which include
 * VPN overlays like ZeroTier; then IPv6. Stable within each group, so the
 * list does not reshuffle between visits.
 *
 * Display only. A friend's invite keeps its own order (peers race every
 * address anyway, since the dead-first-address fix).
 */
func homeFirst(ips []string) []string {
	rank := func(s string) int {
		ip := net.ParseIP(s)
		switch {
		case ip == nil:
			return 4
		case ip.To4() != nil && ip.To4()[0] == 192 && ip.To4()[1] == 168:
			return 0
		case ip.To4() != nil && ip.IsPrivate():
			return 1
		case ip.To4() != nil:
			return 2
		default:
			return 3
		}
	}
	out := append([]string(nil), ips...)
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}
