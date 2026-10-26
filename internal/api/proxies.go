package api

import (
	"net"
	"strings"
	"sync"

	"lancast/internal/auth"
)

// proxyCache holds the parsed trusted-proxy list, re-parsed only when the
// setting changes. Every request asks, so it is worth not parsing CIDRs each
// time; the key is the stored list itself.
type proxyCache struct {
	mu     sync.Mutex
	parsed bool
	key    string
	nets   []*net.IPNet
}

// trustedProxies is the current list, for auth.TrustProxies. A stored entry
// that no longer parses (a hand-edited settings file) drops the whole list
// rather than part of it: half a list is a configuration nobody chose.
func (s *Server) trustedProxies() []*net.IPNet {
	list := s.settings.Get().TrustedProxies
	key := strings.Join(list, ",")
	s.proxies.mu.Lock()
	defer s.proxies.mu.Unlock()
	if s.proxies.parsed && key == s.proxies.key {
		return s.proxies.nets
	}
	nets, _, err := auth.ParseTrustedProxies(list)
	if err != nil {
		s.log.Warn("trusted proxies in settings do not parse; ignoring them", "error", err)
		nets = nil
	}
	s.proxies.parsed, s.proxies.key, s.proxies.nets = true, key, nets
	return nets
}
