// Package desktop holds the small cross-platform helpers the server tray and the
// client launcher both need (ADR 0022): turning a listen address into a URL,
// opening the default browser, and probing whether the server is up.
package desktop

import (
	"crypto/tls"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"
)

// hostPort normalizes a listen address for use in a URL. A wildcard or empty
// host becomes localhost, since "http://:8080" is not something a browser opens.
func hostPort(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = "", addr
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "localhost"
	}
	return host + ":" + port
}

// UIURL is the browsable URL for a listen address. Pure: it assumes http and
// does no I/O, so it stays usable for display and as a fallback. Use ResolvedURL
// when actually opening a browser.
func UIURL(addr string) string {
	return "http://" + hostPort(addr)
}

// ResolvedURL is the URL a browser should actually be sent to: whichever scheme
// the server answers on. LANcast serves HTTPS once an account exists (ADR 0014),
// so assuming http lands the browser on a redirect. Falls back to http when
// nothing answers, which is the right guess for a server still starting.
func ResolvedURL(addr string) string {
	if scheme, ok := probe(addr); ok {
		return scheme + "://" + hostPort(addr)
	}
	return UIURL(addr)
}

// HealthURL is the health endpoint for a listen address.
func HealthURL(addr string) string { return UIURL(addr) + "/api/health" }

// probeClient does not follow redirects and does not verify the certificate.
// Both are deliberate: the http->https redirect is itself proof the server is
// up, and LANcast's certificate is self-signed by design, so verifying it would
// fail on exactly the setup this check exists to detect. Nothing is sent — this
// only asks whether something is listening.
var probeClient = &http.Client{
	Timeout: 1500 * time.Millisecond,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

// probe reports which scheme a LANcast server is answering on, if any.
func probe(addr string) (scheme string, ok bool) {
	hp := hostPort(addr)
	for _, s := range []string{"http", "https"} {
		req, err := http.NewRequest(http.MethodGet, s+"://"+hp+"/api/health", nil)
		if err != nil {
			continue
		}
		resp, err := probeClient.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		// A redirect means it is up but speaking the other scheme; keep looking
		// so the caller learns which one actually serves.
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			continue
		}
		if resp.StatusCode == http.StatusOK {
			return s, true
		}
	}
	return "", false
}

// BrowserCommand is the platform command that opens a URL in the default
// browser. Kept pure (no exec) so it is unit-tested per GOOS.
func BrowserCommand(goos, url string) []string {
	switch goos {
	case "windows":
		// rundll32 avoids cmd's quoting pitfalls with URLs containing &.
		return []string{"rundll32", "url.dll,FileProtocolHandler", url}
	case "darwin":
		return []string{"open", url}
	default:
		return []string{"xdg-open", url}
	}
}

// OpenBrowser opens url in the default browser, fire-and-forget.
func OpenBrowser(url string) error {
	c := BrowserCommand(runtime.GOOS, url)
	return exec.Command(c[0], c[1:]...).Start()
}

// ServerRunning reports whether a LANcast server is answering at addr, on
// either scheme.
func ServerRunning(addr string) bool {
	_, ok := probe(addr)
	return ok
}

// WaitForServer polls until the server answers or timeout elapses.
func WaitForServer(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if ServerRunning(addr) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}
