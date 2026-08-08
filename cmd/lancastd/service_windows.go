//go:build windows

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"golang.org/x/sys/windows/svc"

	"lancast/internal/applog"
	"lancast/internal/service"
	"lancast/internal/singleton"
)

// serviceRun hosts the server under the Windows service control manager. When
// invoked by hand (not the SCM) it falls back to foreground, so `lancastd
// service run` is debuggable.
func serviceRun(dataDir, addr string) error {
	log := newLogger(false)

	// Under the SCM there is no console and no inherited stderr, so everything
	// this process logs is discarded — including whatever it would say on the
	// way down. Send it to a file in the data directory instead, and keep
	// stderr too so `lancastd service run` by hand still prints.
	//
	// A logging failure is reported and then ignored: not being able to write a
	// log is not a reason to refuse to serve media.
	if lf, err := applog.Open(dataDir); err != nil {
		log.Warn("could not open the log file; logging to stderr only", "error", err)
	} else {
		defer lf.Close()
		// applog.Tee, never io.MultiWriter: the file comes first and neither
		// destination can stop the other. MultiWriter aborts on the first
		// error, and under the SCM there is no stderr — so it wrote nothing to
		// the log at all, in exactly the situation the log exists for.
		log = slog.New(slog.NewTextHandler(applog.Tee(lf, os.Stderr), nil))
		log.Info("logging to file", "path", lf.Path())
	}

	// Every exit goes through the log on its way out.
	//
	// Returning the reason as an error alone is not enough: main prints it to a
	// stderr the SCM discards, so the single most valuable line — why the
	// service refused to start, or what stopped it — never reaches the file
	// that exists to hold exactly that. Caught by running this and watching
	// "another LANcast server is already running" appear on the terminal and
	// not in the log.
	err := serviceServe(dataDir, addr, log)
	if err != nil {
		log.Error("service stopped", "error", err)
	} else {
		log.Info("service stopped")
	}
	return err
}

// serviceServe is serviceRun's body, split out so every one of its exits is
// logged by the caller rather than each return having to remember.
func serviceServe(dataDir, addr string, log *slog.Logger) error {
	// One server at a time — the service holds the name so an interactive launch
	// finds it and opens the UI instead of starting a duplicate.
	release, held, err := singleton.Acquire(singleton.Server)
	if err == nil && !held {
		// Names the holder for the same reason main does — and it matters more
		// here, because this line goes to lancastd.log, which is the only record
		// anyone gets of a service that refused to start (v0.4.2).
		if running, ok := service.RunningServer(); ok {
			return fmt.Errorf("another LANcast server is already running: %s", running.Describe())
		}
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
