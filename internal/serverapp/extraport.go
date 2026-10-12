package serverapp

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
)

/*
 * extraPort is a second listening port that opens and closes while the server
 * runs. It exists for one reason: port 443 while the public certificate is on
 * (ADR 0081, amended 2026-10-11).
 *
 * IPv6 has no port translation. The router's "443 → 8080" forward only works
 * for IPv4, so a browser or a certificate authority reaching the server over
 * IPv6 at port 443 finds nothing unless the server answers there itself. A
 * port proxy in front (netsh portproxy) would answer, but every connection
 * would then come from the proxy: the per-account backoff would see every
 * caller as local and exempt, and every log line would name the wrong client.
 * So LANcast listens itself, and the same handlers, throttles and logging see
 * the real address.
 *
 * Tied to the certificate, not a switch of its own: it is opened when the
 * public certificate is turned on and closed when it is turned off, so 443 is
 * only ever open on a server whose admin opted in, and never without the
 * certificate that makes it worth anything.
 */
type extraPort struct {
	addr  string
	serve func(net.Listener) // runs the listener until it is closed
	log   *slog.Logger

	mu sync.Mutex
	ln net.Listener
}

// newExtraPort prepares port 443 on the same host the main listener uses, or
// returns nil when the main listener already is 443 and there is nothing to
// add.
func newExtraPort(listenAddr string, serve func(net.Listener), log *slog.Logger) *extraPort {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil || port == "443" {
		return nil
	}
	return &extraPort{addr: net.JoinHostPort(host, "443"), serve: serve, log: log}
}

// Set opens or closes the port. Opening one that is open, or closing one that
// is closed, does nothing. A port that cannot be opened — something else holds
// it, or the process may not bind below 1024 — is an error the caller reports,
// never a reason to stop the server.
func (e *extraPort) Set(on bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if on == (e.ln != nil) {
		return nil
	}
	if !on {
		err := e.ln.Close()
		e.ln = nil
		e.log.Info("listening stopped", "addr", e.addr)
		return err
	}
	ln, err := net.Listen("tcp", e.addr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w", e.addr, err)
	}
	e.ln = ln
	e.log.Info("listening", "addr", e.addr, "scheme", "https", "why", "public certificate")
	go e.serve(ln)
	return nil
}

// Close shuts the port on the way out.
func (e *extraPort) Close() {
	if e == nil {
		return
	}
	_ = e.Set(false)
}

// closedListener reports the error a Serve returns once its listener has been
// closed on purpose, which is not worth a warning.
func closedListener(err error) bool {
	return errors.Is(err, net.ErrClosed)
}
