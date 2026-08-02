package main

import (
	"fmt"
	"net"
	"os/exec"
	"runtime"
)

// uiURL turns a listen address into a browsable URL. A wildcard or empty host
// becomes localhost, since "http://:8080" and "http://0.0.0.0:8080" are not
// something a browser opens.
func uiURL(addr string) string {
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

// browserCommand is the platform command that opens a URL in the default
// browser. Kept pure (no exec) so it is unit-tested per GOOS.
func browserCommand(goos, url string) []string {
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

// openBrowser opens url in the default browser, fire-and-forget.
func openBrowser(url string) error {
	c := browserCommand(runtime.GOOS, url)
	return exec.Command(c[0], c[1:]...).Start()
}
