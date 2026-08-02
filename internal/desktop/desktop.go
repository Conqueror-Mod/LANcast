// Package desktop holds the small cross-platform helpers the server tray and the
// client launcher both need (ADR 0022): turning a listen address into a URL,
// opening the default browser, and probing whether the server is up.
package desktop

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"
)

// UIURL turns a listen address into a browsable URL. A wildcard or empty host
// becomes localhost, since "http://:8080" is not something a browser opens.
func UIURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = "", addr
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "localhost"
	}
	return fmt.Sprintf("http://%s:%s", host, port)
}

// HealthURL is the health endpoint for a listen address.
func HealthURL(addr string) string { return UIURL(addr) + "/api/health" }

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

// ServerRunning reports whether a LANcast server is answering at addr.
func ServerRunning(addr string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, HealthURL(addr), nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
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
