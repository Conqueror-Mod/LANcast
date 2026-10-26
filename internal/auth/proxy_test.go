package auth

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mustProxies(t *testing.T, entries ...string) []*net.IPNet {
	t.Helper()
	nets, _, err := ParseTrustedProxies(entries)
	if err != nil {
		t.Fatal(err)
	}
	return nets
}

func request(remote string, xff ...string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = remote
	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}
	return r
}

func TestParseTrustedProxiesNormalises(t *testing.T) {
	_, normal, err := ParseTrustedProxies([]string{" 127.0.0.1 ", "10.0.0.0/24", "::1", "10.0.0.7/24", "127.0.0.1/32", ""})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"127.0.0.1/32", "10.0.0.0/24", "::1/128"}
	if len(normal) != len(want) {
		t.Fatalf("got %v, want %v", normal, want)
	}
	for i := range want {
		if normal[i] != want[i] {
			t.Fatalf("got %v, want %v", normal, want)
		}
	}
}

func TestParseTrustedProxiesRefuses(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want error
	}{
		{"proxy.local", ErrBadProxy},
		{"10.0.0.1/33", ErrBadProxy},
		{"0.0.0.0/0", ErrProxyTooWide},
		{"10.0.0.0/7", ErrProxyTooWide},
		{"::/0", ErrProxyTooWide},
		{"2001::/15", ErrProxyTooWide},
	} {
		if _, _, err := ParseTrustedProxies([]string{tc.in}); !errors.Is(err, tc.want) {
			t.Errorf("%q: got %v, want %v", tc.in, err, tc.want)
		}
	}
	many := make([]string, MaxTrustedProxies+1)
	for i := range many {
		many[i] = "10.0.0.1"
	}
	if _, _, err := ParseTrustedProxies(many); err == nil {
		t.Error("a list past the limit was accepted")
	}
}

func TestForwardedClient(t *testing.T) {
	proxies := mustProxies(t, "127.0.0.1", "10.0.0.0/24")
	for _, tc := range []struct {
		name   string
		r      *http.Request
		want   string
		viaXFF bool
	}{
		{"no list is the old behaviour", request("127.0.0.1:5000", "203.0.113.9"), "", false},
		{"an untrusted peer's header is ignored", request("198.51.100.4:5000", "127.0.0.1"), "", false},
		{"one proxy", request("127.0.0.1:5000", "203.0.113.9"), "203.0.113.9", true},
		{"two trusted hops", request("127.0.0.1:5000", "203.0.113.9, 10.0.0.5"), "203.0.113.9", true},
		{"a forged entry left of the real one is not believed",
			request("127.0.0.1:5000", "192.168.1.10, 203.0.113.9"), "203.0.113.9", true},
		{"split across header lines", request("127.0.0.1:5000", "192.168.1.10", "203.0.113.9"), "203.0.113.9", true},
		{"a port on the hop", request("127.0.0.1:5000", "203.0.113.9:4431"), "203.0.113.9", true},
		{"bracketed IPv6", request("127.0.0.1:5000", "[2001:db8::7]:4431"), "2001:db8::7", true},
		{"garbage stops the walk at the last good hop",
			request("127.0.0.1:5000", "203.0.113.9, nonsense, 198.51.100.4"), "198.51.100.4", true},
		{"no header from a trusted proxy", request("127.0.0.1:5000"), "", false},
		{"only trusted hops is the leftmost", request("127.0.0.1:5000", "10.0.0.9, 10.0.0.5"), "10.0.0.9", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nets := proxies
			if tc.name == "no list is the old behaviour" {
				nets = nil
			}
			got, ok := ForwardedClient(tc.r, nets)
			if got != tc.want || ok != tc.viaXFF {
				t.Fatalf("got %q %v, want %q %v", got, ok, tc.want, tc.viaXFF)
			}
		})
	}
}

// The point of the whole change: behind a same-machine proxy, a caller from
// the internet must not look like it is inside the house.
func TestTrustProxiesMakesAnInternetCallerOutside(t *testing.T) {
	proxies := mustProxies(t, "127.0.0.1")
	var inside bool
	var key string
	h := TrustProxies(func() []*net.IPNet { return proxies }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inside, key = InsideNetwork(r), ClientKey(r)
	}))

	h.ServeHTTP(httptest.NewRecorder(), request("127.0.0.1:5000", "203.0.113.9"))
	if inside || key != "203.0.113.9" {
		t.Fatalf("through the proxy: inside=%v key=%q, want outside and the client's address", inside, key)
	}

	// Without the list it is exactly what it was.
	h = TrustProxies(func() []*net.IPNet { return nil }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inside, key = InsideNetwork(r), ClientKey(r)
	}))
	h.ServeHTTP(httptest.NewRecorder(), request("127.0.0.1:5000", "203.0.113.9"))
	if !inside || key != "127.0.0.1" {
		t.Fatalf("no list: inside=%v key=%q, want the unchanged loopback caller", inside, key)
	}
}
