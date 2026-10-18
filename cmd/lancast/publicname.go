package main

import (
	"encoding/json"
	"net"
	"net/http"
	"time"

	"lancast/internal/certpin"
	"lancast/internal/desktop"
)

/*
 * Opening this machine's server at its public name, so passkeys work in the
 * window (ADR 0082, decided 2026-10-17).
 *
 * A passkey belongs to the server's public name and works only there. The
 * window opened the server as localhost, so it never offered one. When the
 * server has a valid public certificate, the window now opens
 * https://<name>:<port> instead, and its own browser resolves the name to
 * 127.0.0.1 (clientwindow.Options.LoopbackName): the page is on this machine
 * exactly as before, nothing goes out to DNS or the internet, and the
 * certificate is the real public one for that name.
 *
 * Only for a server on this machine, and only for the window. The native
 * player's relay keeps fetching from localhost by the pinned key: it is not a
 * browser, and it does not need the name.
 */

// publicNameOf asks the local server, by its pinned key, what its public name
// is. Empty when it has none, or on any doubt: the window then opens
// localhost, as it always did.
func publicNameOf(origin, pin string) string {
	if pin == "" {
		return ""
	}
	client := &http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{TLSClientConfig: certpin.TLSConfig(pin)},
	}
	resp, err := client.Get(origin + "/api/auth/status")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var st struct {
		Passkeys struct {
			PublicName string `json:"public_name"`
		} `json:"passkeys"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&st) != nil {
		return ""
	}
	return st.Passkeys.PublicName
}

// windowURL is where the window opens this machine's server at addr: its
// public name when there is one, otherwise the address as always.
func (l *launcher) windowURL(addr string) string {
	if l.publicName != "" {
		if _, port, err := net.SplitHostPort(addr); err == nil && port != "" {
			return "https://" + net.JoinHostPort(l.publicName, port)
		}
	}
	return desktop.ResolvedURL(addr)
}
