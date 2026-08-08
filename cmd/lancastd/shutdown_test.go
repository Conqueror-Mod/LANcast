package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

// Windows reported event 7031 — "the service terminated unexpectedly" — when the
// service was asked to stop, and the recovery policy then restarted it. The
// practical effect is that Stop-Service does not keep LANcast stopped.
//
// A service that does not report SERVICE_STOPPED in time is killed, so the
// first question is whether shutdown returns promptly at all. These tests
// measure the real path: run(ctx) with a cancelled context, timed.
//
// The ruling being pinned: when the server is closed it must fully close, and
// the process must not hang.

// The bound the fix guarantees: graceful up to shutdownGrace, then forced.
// Derived from the constant rather than restated, so the test tracks the code.
var stopBudget = shutdownGrace + 2*time.Second

func waitHealthy(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/api/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("server never became healthy")
}

// startServer runs the real run() on a free port and returns its base URL and a
// stop function that cancels and reports how long run took to return.
func startServer(t *testing.T) (base string, stop func() (time.Duration, error)) {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() { errc <- run(ctx, addr, t.TempDir(), log) }()

	base = "http://" + addr
	waitHealthy(t, base)

	return base, func() (time.Duration, error) {
		start := time.Now()
		cancel()
		select {
		case err := <-errc:
			return time.Since(start), err
		case <-time.After(60 * time.Second):
			return 60 * time.Second, fmt.Errorf("run did not return within 60s")
		}
	}
}

// The baseline: an idle server must shut down immediately.
func TestShutdownIsPromptWhenIdle(t *testing.T) {
	_, stop := startServer(t)
	took, err := stop()
	if err != nil {
		t.Fatalf("run returned an error on clean shutdown: %v", err)
	}
	t.Logf("idle shutdown took %s", took)
	// Nothing to wait for, so this must not consume any of the grace at all.
	if took > time.Second {
		t.Errorf("idle shutdown took %s; with no connections it should be immediate", took)
	}
}

// The case that matters. A media server's connections are long-lived by nature,
// and http.Server.Shutdown waits for in-flight requests. If a client is holding
// one open — a stream, or a browser that keeps a connection alive — shutdown
// must not sit on it until the service control manager loses patience and kills
// the process.
func TestShutdownIsPromptWithAnOpenConnection(t *testing.T) {
	base, stop := startServer(t)

	// A keep-alive connection that has completed a request and is idle. This is
	// what every browser tab leaves behind.
	client := &http.Client{}
	resp, err := client.Get(base + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	// And a raw socket that connected but never sent a request, which is what a
	// half-open client or a port scanner leaves.
	conn, err := net.Dial("tcp", base[len("http://"):])
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	took, err := stop()
	if err != nil {
		t.Fatalf("run returned an error on shutdown: %v", err)
	}
	t.Logf("shutdown with open connections took %s", took)
	if took > stopBudget {
		t.Errorf("shutdown took %s with idle connections open, over the %s budget.\n\n"+
			"A service that does not stop promptly is killed by the service "+
			"control manager, which reports it as an unexpected termination and "+
			"then restarts it.", took, stopBudget)
	}
}
