package clientwindow

import (
	"net/url"
	"strings"
	"sync"
)

/*
 * Which documents may hold the window, and so reach its bindings (review
 * finding R11-2).
 *
 * The window exposes about sixty native functions to whatever top-level
 * document it holds, and they are injected into every document it loads. With
 * ADR 0070 that includes *remote* servers' pages, and nothing stopped a page
 * sending the main frame to any https site, which then got the same
 * functions. So the window keeps a set of origins it was opened on or was
 * sent to by this program, and two things are checked against it:
 *
 *   - a top-level navigation to any other http(s) origin is cancelled, and
 *     the address is handed to the system browser instead;
 *   - a binding call whose source is an http(s) document on any other origin
 *     is dropped.
 *
 * A document that is not http(s) is allowed. That is the picker, which this
 * program gives the view with NavigateToString and which has no origin of its
 * own, and anything a permitted page made for itself (about:blank, a blob:
 * of its own origin). Chromium blocks a page navigating the top frame to
 * data:, and a page reaching a foreign http(s) origin is exactly what the
 * navigation check refuses. What is decided by a document *being the picker*
 * -- trusting a server -- is not left to this check at all: those bindings
 * ask for a secret only the picker's page carries (cmd/lancast/picker.go).
 */
type originPolicy struct {
	mu      sync.Mutex
	allowed map[string]bool
}

func newOriginPolicy(urls ...string) *originPolicy {
	p := &originPolicy{allowed: map[string]bool{}}
	for _, u := range urls {
		p.add(u)
	}
	return p
}

// add permits raw's origin. A host-initiated navigation calls it first, so
// this program can always send the window where it means to: a server that
// moved port (ADR 0089), the public name (publicname.go).
func (p *originPolicy) add(raw string) {
	if o, ok := originOf(raw); ok {
		p.mu.Lock()
		p.allowed[o] = true
		p.mu.Unlock()
	}
}

// allows reports whether a document at raw may hold the window.
func (p *originPolicy) allows(raw string) bool {
	// The scheme read by hand rather than by url.Parse, which refuses some
	// data: addresses outright -- and refusing the picker's would leave the
	// window with no way to choose a server.
	scheme, _, found := strings.Cut(raw, ":")
	if !found {
		return false
	}
	switch strings.ToLower(scheme) {
	case "http", "https":
	default:
		return true
	}
	o, ok := originOf(raw)
	if !ok {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.allowed[o]
}

// originOf is scheme://host:port, lower-cased, with the default port written
// out so https://a and https://a:443 are one origin.
func originOf(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	port := u.Port()
	switch {
	case port != "":
	case scheme == "https":
		port = "443"
	case scheme == "http":
		port = "80"
	default:
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host + ":" + port, true
}
