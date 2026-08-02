package desktop

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUIURL(t *testing.T) {
	cases := map[string]string{
		":8080":            "http://localhost:8080",
		"0.0.0.0:8080":     "http://localhost:8080",
		"[::]:8080":        "http://localhost:8080",
		"192.168.1.5:9000": "http://192.168.1.5:9000",
	}
	for addr, want := range cases {
		if got := UIURL(addr); got != want {
			t.Errorf("UIURL(%q) = %q, want %q", addr, got, want)
		}
	}
	if !strings.HasSuffix(HealthURL(":8080"), "/api/health") {
		t.Errorf("HealthURL = %q", HealthURL(":8080"))
	}
}

func TestBrowserCommand(t *testing.T) {
	url := "http://localhost:8080/x?a=1&b=2"
	if got := BrowserCommand("windows", url); got[0] != "rundll32" || got[len(got)-1] != url {
		t.Errorf("windows = %v", got)
	}
	if got := BrowserCommand("darwin", url); got[0] != "open" {
		t.Errorf("darwin = %v", got)
	}
	if got := BrowserCommand("linux", url); got[0] != "xdg-open" {
		t.Errorf("linux = %v", got)
	}
}

func TestServerRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	if !ServerRunning(addr) {
		t.Errorf("ServerRunning(%q) = false, want true (health is up)", addr)
	}
	// A port nothing listens on is not running.
	if ServerRunning("127.0.0.1:1") {
		t.Error("ServerRunning on a dead port = true, want false")
	}
}

// The bug this guards: LANcast serves HTTPS with a self-signed certificate once
// an account exists (ADR 0014). A probe that follows the http->https redirect
// and verifies the certificate fails, and the launcher read that failure as "the
// server is down" — then tried to start a second one and gave up.
func TestServerRunningAcceptsSelfSignedHTTPS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "https://")
	if !ServerRunning(addr) {
		t.Error("a self-signed HTTPS server was reported as not running")
	}
	if got := ResolvedURL(addr); !strings.HasPrefix(got, "https://") {
		t.Errorf("ResolvedURL = %q, want the https scheme the server actually serves", got)
	}
}

// A plain-http server still resolves to http — the probe must not upgrade a
// server that is not serving TLS.
func TestResolvedURLKeepsHTTPWhenThatIsWhatServes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	if got := ResolvedURL(addr); !strings.HasPrefix(got, "http://") {
		t.Errorf("ResolvedURL = %q, want http", got)
	}
}
