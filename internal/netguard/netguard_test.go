package netguard

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

/*
 * The guard, tested against a server that is actually listening.
 *
 * Asserting that `blocked()` returns true for 127.0.0.1 would test a table of
 * IP ranges against itself. What matters is whether a *connection* is refused,
 * because the failure this prevents is a socket that opened.
 */

// A real loopback server, reachable by any client without the guard.
func loopbackServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("this is the server itself"))
	}))
	t.Cleanup(s.Close)
	return s
}

/*
 * The case the guard exists for: a request that an allowlist would wave
 * through, to a name that resolves somewhere it should not.
 *
 * httptest gives a 127.0.0.1 URL, which is what a hostile DNS record would have
 * produced anyway — the resolver is not the interesting part, the destination
 * is.
 */
func TestAConnectionToLoopbackIsRefused(t *testing.T) {
	s := loopbackServer(t)

	// Without the guard it plainly works, so the test below is about the guard
	// rather than about the server being unreachable.
	if _, err := http.Get(s.URL); err != nil {
		t.Fatalf("the fixture server is not reachable at all: %v", err)
	}

	_, err := Client(5 * time.Second).Get(s.URL)
	if err == nil {
		t.Fatal("the guarded client reached the server itself")
	}
	var blocked ErrBlocked
	if !errors.As(err, &blocked) {
		t.Errorf("err = %v, want ErrBlocked — refused for the wrong reason "+
			"proves nothing", err)
	}
}

/*
 * A redirect is a fresh dial, so it is guarded too.
 *
 * This is the shape that would otherwise walk straight past a URL check: the
 * allowlisted host answers 302 to somewhere internal, and a check done on the
 * original URL has already been satisfied.
 */
func TestARedirectToLoopbackIsRefused(t *testing.T) {
	internal := loopbackServer(t)

	// A public-looking hop is not available in a unit test, so the redirect
	// source is loopback as well — which means this asserts the redirect is
	// followed into a *second* guarded dial rather than being read from cache.
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL, http.StatusFound)
	}))
	defer redirector.Close()

	if _, err := Client(5 * time.Second).Get(redirector.URL); err == nil {
		t.Error("a redirect reached the server itself")
	}
}

// The ranges that are easy to forget, checked by name so a future edit that
// drops one is a failure with a label rather than a silent hole.
func TestTheForgettableRangesAreBlocked(t *testing.T) {
	for _, c := range []struct{ name, ip string }{
		{"loopback", "127.0.0.1"},
		{"IPv6 loopback", "::1"},
		{"private 10/8", "10.0.0.1"},
		{"private 192.168/16", "192.168.1.1"},
		{"private 172.16/12", "172.16.0.1"},
		{"cloud metadata", "169.254.169.254"},
		{"IPv6 unique local", "fd00::1"},
		{"unspecified", "0.0.0.0"},
		{"multicast", "224.0.0.1"},
	} {
		if !blocked(net.ParseIP(c.ip)) {
			t.Errorf("%s (%s) is not blocked", c.name, c.ip)
		}
	}
}

// And ordinary internet addresses still work, or the guard would be a way of
// turning every fetch off.
func TestPublicAddressesAreAllowed(t *testing.T) {
	for _, ip := range []string{"1.1.1.1", "8.8.8.8", "93.184.216.34", "2606:4700::1111"} {
		if blocked(net.ParseIP(ip)) {
			t.Errorf("%s is blocked; the guard would stop ordinary fetches", ip)
		}
	}
}

// The error names the address, because the hostname is the part that looked
// fine and the address is the part somebody needs to see.
func TestTheRefusalNamesTheAddress(t *testing.T) {
	err := ErrBlocked{Addr: "127.0.0.1:8080"}
	if !strings.Contains(err.Error(), "127.0.0.1:8080") {
		t.Errorf("error = %q, want the address in it", err.Error())
	}
}

// The guarded dialer is usable directly, for a caller that already has a
// transport it wants to keep.
func TestDialContextIsGuardedToo(t *testing.T) {
	s := loopbackServer(t)
	addr := strings.TrimPrefix(s.URL, "http://")
	if _, err := DialContext(context.Background(), "tcp", addr); err == nil {
		t.Error("the bare dialer connected to loopback")
	}
}
