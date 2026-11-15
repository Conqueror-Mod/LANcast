package clientwindow

import "testing"

/*
 * R11-2. The window's bindings belong to the documents it was opened on or
 * that this program sent it to. These are the rules that decide which.
 */
func TestTheWindowKeepsToItsOwnOrigins(t *testing.T) {
	p := newOriginPolicy("https://192.168.1.21:8080/", "https://media.example.test")

	for uri, want := range map[string]bool{
		// The server's own pages, however addressed.
		"https://192.168.1.21:8080/settings?pane=desktop": true,
		"https://MEDIA.example.test/":                     true,
		"https://media.example.test:443/films":            true,
		// The picker and what a permitted page makes for itself.
		"about:blank": true,
		"data:text/html;charset=utf-8,<p>picker</p>": true,
		// Anywhere else on the web, or the same host on another port or scheme.
		"https://evil.example.org/":                    false,
		"https://192.168.1.21:8443/":                   false,
		"http://192.168.1.21:8080/":                    false,
		"https://192.168.1.22:8080/":                   false,
		"https://media.example.test.evil.example.org/": false,
		"http://media.example.test/":                   false,
		"https://":                                     false,
	} {
		if got := p.allows(uri); got != want {
			t.Errorf("allows(%q) = %v, want %v", uri, got, want)
		}
	}
}

// This program may always send the window somewhere: a server that moved
// port, the public name. Navigate adds the origin before it goes.
func TestAHostNavigationAddsItsOrigin(t *testing.T) {
	p := newOriginPolicy("https://127.0.0.1:8080")
	if p.allows("https://127.0.0.1:8081/") {
		t.Fatal("a new port was allowed before anything sent the window there")
	}
	p.add("https://127.0.0.1:8081/")
	if !p.allows("https://127.0.0.1:8081/films") {
		t.Error("the port the window was sent to is not allowed")
	}
}

func TestOriginsAreComparedWhole(t *testing.T) {
	for raw, want := range map[string]string{
		"https://A.example:8080/x": "https://a.example:8080",
		"https://a.example/x":      "https://a.example:443",
		"http://a.example":         "http://a.example:80",
		"https://[::1]:8080/":      "https://[::1]:8080",
	} {
		if got, ok := originOf(raw); !ok || got != want {
			t.Errorf("originOf(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
	if _, ok := originOf("about:blank"); ok {
		t.Error("about:blank has an origin")
	}
}
