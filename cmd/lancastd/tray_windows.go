//go:build windows

package main

import (
	"context"

	"fyne.io/systray"

	"lancast/internal/branding"
	"lancast/internal/desktop"
)

// trayRun hosts the server and a system-tray presence — the windowless desktop
// mode (ADR 0022). The tray menu opens the UI and quits; quitting cancels the
// same shutdown context the service and Ctrl-C use. Windows-only: headless
// targets have no tray.
//
// If a server is already answering on this address — the installed service, or an
// earlier launch — the tray attaches to it rather than starting a second one that
// could not bind the port anyway. Quitting then closes the tray and leaves that
// server alone, because this process did not start it.
func trayRun(addr, dataDir string) error {
	log := newLogger(false)
	attached := desktop.ServerRunning(addr)

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)

	onReady := func() {
		systray.SetIcon(branding.IconICO)
		systray.SetTitle("LANcast")
		if attached {
			systray.SetTooltip("LANcast (server already running)")
		} else {
			systray.SetTooltip("LANcast media server")
		}
		mOpen := systray.AddMenuItem("Open LANcast", "Open the LANcast web UI")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit", "Close LANcast")

		if !attached {
			go func() {
				err := run(ctx, addr, dataDir, log)
				if err != nil && err != context.Canceled {
					log.Error("server exited", "error", err)
					// windowsgui has no console, so a bind clash or a bad data
					// dir would be silent without this.
					alert("LANcast could not start", err.Error())
					systray.Quit()
				}
				errc <- err
			}()
		}

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

	// onExit runs when systray.Quit is called: stop the server we started (if
	// any) and let it drain. An attached tray leaves the other server running.
	onExit := func() {
		cancel()
		if !attached {
			<-errc
		}
	}

	systray.Run(onReady, onExit)
	return nil
}
