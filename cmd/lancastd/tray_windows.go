//go:build windows

package main

import (
	"context"

	"fyne.io/systray"

	"lancast/internal/branding"
	"lancast/internal/desktop"
	"lancast/internal/singleton"
)

// trayRun hosts the server and a system-tray presence — the windowless desktop
// mode (ADR 0022). The tray menu opens the UI and quits; quitting cancels the
// same shutdown context the service and Ctrl-C use. Windows-only: headless
// targets have no tray.
//
// Only one server runs at a time. If another instance already holds the name —
// the installed service, or an earlier launch — this one opens the UI and exits
// rather than lingering as a duplicate process that could not bind the port
// anyway.
func trayRun(addr, dataDir string) error {
	release, held, err := singleton.Acquire(singleton.Server)
	if err == nil && !held {
		return openExisting(addr)
	}
	defer release()

	// A port already answering means a server is up even if the name check could
	// not see it (a different session, an older build without the guard).
	if desktop.ServerRunning(addr) {
		return openExisting(addr)
	}

	log := newLogger(false)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)

	onReady := func() {
		systray.SetIcon(branding.IconICO)
		systray.SetTitle("LANcast")
		systray.SetTooltip("LANcast media server")
		mOpen := systray.AddMenuItem("Open LANcast", "Open the LANcast web UI")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit", "Stop the server and quit")

		go func() {
			err := run(ctx, addr, dataDir, log)
			if err != nil && err != context.Canceled {
				log.Error("server exited", "error", err)
				// windowsgui has no console, so a bind clash or a bad data dir
				// would be silent without this.
				alert("LANcast could not start", err.Error())
				systray.Quit()
			}
			errc <- err
		}()

		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					if err := desktop.OpenBrowser(desktop.UIURL(addr)); err != nil {
						log.Warn("could not open browser", "error", err)
					}
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()
	}

	onExit := func() {
		cancel()
		<-errc
	}

	systray.Run(onReady, onExit)
	return nil
}

// openExisting hands the user to the server that is already running, which is
// what they wanted by launching this, and returns so the process exits.
func openExisting(addr string) error {
	return desktop.OpenBrowser(desktop.UIURL(addr))
}
