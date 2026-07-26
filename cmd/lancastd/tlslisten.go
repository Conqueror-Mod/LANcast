package main

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// This file lets a single listening port serve HTTPS while still answering
// plaintext HTTP with a redirect, so enabling TLS does not silently break a
// bookmarked http:// address (ADR 0014). It peeks the first byte of each
// connection — a TLS handshake begins with 0x16 — and routes accordingly.

const tlsRecordHandshake = 0x16

// splitTLS demultiplexes base into two listeners: one yielding TLS connections,
// one yielding plaintext connections. It returns immediately; a background
// goroutine dispatches accepted connections until base is closed.
func splitTLS(base net.Listener, log *slog.Logger) (tlsLn, plainLn net.Listener) {
	tl := newChanListener(base.Addr())
	pl := newChanListener(base.Addr())

	go func() {
		defer tl.Close()
		defer pl.Close()
		for {
			c, err := base.Accept()
			if err != nil {
				return // base closed on shutdown; both sub-listeners drain and stop
			}
			go dispatch(c, tl, pl, log)
		}
	}()

	return tl, pl
}

// dispatch peeks one byte to classify c, then hands it to the matching
// sub-listener with the peeked byte pushed back so the eventual reader sees the
// whole stream. A connection that sends nothing within the deadline is dropped
// rather than parked forever holding a goroutine.
func dispatch(c net.Conn, tlsLn, plainLn *chanListener, log *slog.Logger) {
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	b := make([]byte, 1)
	n, err := c.Read(b)
	_ = c.SetReadDeadline(time.Time{})
	if err != nil || n == 0 {
		_ = c.Close()
		return
	}

	pc := &peekedConn{Conn: c, prefix: b[:n]}
	target := plainLn
	if b[0] == tlsRecordHandshake {
		target = tlsLn
	}
	if !target.deliver(pc) {
		_ = pc.Close() // listener already closed (shutdown in progress)
	}
}

// httpsRedirect answers a plaintext request with a permanent redirect to the
// same host and path over HTTPS. Host already carries the port, so only the
// scheme changes.
func httpsRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://"+r.Host+r.URL.RequestURI(), http.StatusMovedPermanently)
}

// peekedConn re-yields bytes consumed during classification before reading from
// the underlying connection.
type peekedConn struct {
	net.Conn
	prefix []byte
}

func (c *peekedConn) Read(p []byte) (int, error) {
	if len(c.prefix) > 0 {
		n := copy(p, c.prefix)
		c.prefix = c.prefix[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}

// chanListener is a net.Listener fed connections over a channel by the demux
// goroutine rather than accepting them itself.
type chanListener struct {
	conns     chan net.Conn
	addr      net.Addr
	done      chan struct{}
	closeOnce sync.Once
}

func newChanListener(addr net.Addr) *chanListener {
	return &chanListener{conns: make(chan net.Conn), addr: addr, done: make(chan struct{})}
}

func (l *chanListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, errListenerClosed
	}
}

func (l *chanListener) Close() error {
	l.closeOnce.Do(func() { close(l.done) })
	return nil
}

func (l *chanListener) Addr() net.Addr { return l.addr }

// deliver hands c to a waiting Accept, or reports false if the listener has
// closed so the caller can drop the connection instead of leaking it.
func (l *chanListener) deliver(c net.Conn) bool {
	select {
	case l.conns <- c:
		return true
	case <-l.done:
		return false
	}
}

var errListenerClosed = errors.New("listener closed")
