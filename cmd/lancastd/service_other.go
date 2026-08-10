//go:build !windows

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// serviceRun on non-Windows just runs the server foreground under a signal
// context. Linux services are systemd units that run the plain binary, so this
// path is only reached if `service run` is invoked directly.
func serviceRun(dataDir, addr string) error {
	// systemd sets INVOCATION_ID for every unit it starts, and nothing else
	// does. It is the only reliable way to know we are under a service manager
	// here, where there is no service host to be entered through — systemd runs
	// the plain binary (ADR 0016).
	serviceManaged = os.Getenv("INVOCATION_ID") != ""

	log := newLogger(false)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return run(ctx, addr, dataDir, log)
}
