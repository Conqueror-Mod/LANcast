//go:build windows

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"golang.org/x/sys/windows/svc"

	"lancast/internal/service"
	"lancast/internal/singleton"
)

// serviceRun hosts the server under the Windows service control manager. When
// invoked by hand (not the SCM) it falls back to foreground, so `lancastd
// service run` is debuggable.
func serviceRun(dataDir, addr string) error {
	log := newLogger(false)

	// One server at a time — the service holds the name so an interactive launch
	// finds it and opens the UI instead of starting a duplicate.
	release, held, err := singleton.Acquire(singleton.Server)
	if err == nil && !held {
		return fmt.Errorf("another LANcast server is already running")
	}
	defer release()

	isSvc, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if !isSvc {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		return run(ctx, addr, dataDir, log)
	}
	return svc.Run(service.Name, &handler{addr: addr, dataDir: dataDir, log: log})
}

type handler struct {
	addr, dataDir string
	log           *slog.Logger
}

// Execute is the SCM callback. It runs the server in a goroutine and cancels its
// context when the SCM asks the service to stop — the same shutdown path Ctrl-C
// takes foreground.
func (h *handler) Execute(_ []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- run(ctx, h.addr, h.dataDir, h.log) }()

	changes <- svc.Status{State: svc.Running, Accepts: accepted}
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				<-errc
				return false, 0
			}
		case err := <-errc:
			cancel()
			if err != nil {
				h.log.Error("service exited with error", "error", err)
				return false, 1
			}
			return false, 0
		}
	}
}
