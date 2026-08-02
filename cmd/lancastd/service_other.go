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
	log := newLogger(false)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return run(ctx, addr, dataDir, log)
}
