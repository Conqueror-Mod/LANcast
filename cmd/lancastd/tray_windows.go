//go:build windows

package main

import (
	"context"
	"time"

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

		// Open the UI on the launch that starts the server, not just on the
		// next one. Someone who double-clicks LANcast asked for LANcast, not
		// for a tray icon: without this the first click appears to do nothing
		// at all — a process starts in the background, no window opens — and
		// they click the executable a second time, which lands on the
		// already-running path above and is what finally shows them the app.
		//
		// Waits for the server to answer first. Opening a browser at a port
		// nothing is listening on yet produces a connection error, which reads
		// as a worse failure than the silence it replaced.
		go func() {
			if !desktop.WaitForServer(addr, 30*time.Second) {
				// run() reports its own failure through alert(), so staying
				// quiet here avoids a second dialog about the same problem.
				log.Warn("server did not come up in time; not opening the browser")
				return
			}
			if err := desktop.OpenBrowser(desktop.ResolvedURL(addr)); err != nil {
				log.Warn("could not open browser", "error", err)
			}
		}()

		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					if err := desktop.OpenBrowser(desktop.ResolvedURL(addr)); err != nil {
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
	return desktop.OpenBrowser(desktop.ResolvedURL(addr))
}
