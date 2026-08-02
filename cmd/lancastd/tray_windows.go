//go:build windows

package main

import (
	"context"

	"fyne.io/systray"

	"lancast/internal/branding"
)

// trayRun hosts the server and a system-tray presence — the windowless desktop
// mode (ADR 0022). The tray menu opens the UI and quits; quitting cancels the
// same shutdown context the service and Ctrl-C use. Windows-only: headless
// targets have no tray.
func trayRun(addr, dataDir string) error {
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
			if err := run(ctx, addr, dataDir, log); err != nil && err != context.Canceled {
				log.Error("server exited", "error", err)
			}
			errc <- nil
		}()

		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					if err := openBrowser(uiURL(addr)); err != nil {
						log.Warn("could not open browser", "error", err)
					}
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()
	}

	// onExit runs when systray.Quit is called: stop the server and let it drain.
	onExit := func() {
		cancel()
		<-errc
	}

	systray.Run(onReady, onExit)
	return nil
}
