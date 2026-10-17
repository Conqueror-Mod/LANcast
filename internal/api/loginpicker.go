package api

import (
	"net"
	"net/http"
	"sort"
)

/*
 * The sign-in page's account picker (Chris, 2026-10-17).
 *
 * Tiles of the accounts on this server, so somebody at home picks their name
 * instead of typing it. A name is half of every login, and this server answers
 * the internet at its public name, so the list goes **only to the home
 * network**: this computer, private and link-local addresses, and addresses on
 * a network one of this machine's own interfaces is on. The last is what
 * makes a phone on the house Wi-Fi count when it speaks IPv6: its address is a
 * global one, but it is inside the house's /64, which a stranger's is not.
 *
 * Anything else gets an empty list, which the page reads as "no picker" and
 * shows the ordinary form. Not a 403: an empty answer says nothing about
 * whether there is anything to hide. Admins can turn it off entirely
 * (login_picker).
 *
 * Only names. No roles, no ids, and nothing about passkeys or passwords.
 */

type loginAccount struct {
	Name string `json:"name"`
}

func (s *Server) loginAccounts(w http.ResponseWriter, r *http.Request) {
	out := struct {
		Accounts []loginAccount `json:"accounts"`
	}{Accounts: []loginAccount{}}
	if !s.settings.Get().LoginPicker || !s.fromHomeNetwork(r) {
		writeJSON(w, http.StatusOK, out)
		return
	}
	users, err := s.st.ListUsers(r.Context())
	if err != nil {
		s.writeInternal(w, err, "list accounts")
		return
	}
	for _, u := range users {
		out.Accounts = append(out.Accounts, loginAccount{Name: u.Name})
	}
	sort.Slice(out.Accounts, func(i, j int) bool { return out.Accounts[i].Name < out.Accounts[j].Name })
	writeJSON(w, http.StatusOK, out)
}

// fromHomeNetwork is whether a request came from this computer or the network
// it is on.
func (s *Server) fromHomeNetwork(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	nets := s.localNets
	if nets == nil {
		nets = interfaceNets
	}
	return onHomeNetwork(ip, nets())
}

// onHomeNetwork is the rule, apart from where the interfaces come from.
func onHomeNetwork(ip net.IP, local []*net.IPNet) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}
	for _, n := range local {
		// A network as wide as the internet is not a home network: an
		// interface reporting /0 must not make every address local.
		if ones, _ := n.Mask.Size(); ones == 0 {
			continue
		}
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// interfaceNets is the networks this machine's interfaces are on.
func interfaceNets() []*net.IPNet {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []*net.IPNet
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			out = append(out, n)
		}
	}
	return out
}
