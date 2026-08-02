//go:build windows

package main

import (
	"fyne.io/systray"

	"lancast/internal/branding"
	"lancast/internal/desktop"
)

// runLauncherTray shows the launcher's tray: open the UI, or quit (which stops a
// server the launcher started). It blocks until Quit.
func runLauncherTray(l *launcher) {
	onReady := func() {
		systray.SetIcon(branding.IconICO)
		systray.SetTitle("LANcast")
		systray.SetTooltip("LANcast")
		mOpen := systray.AddMenuItem("Open LANcast", "Open the LANcast web UI")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit", "Close LANcast")

		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					_ = desktop.OpenBrowser(desktop.ResolvedURL(l.addr))
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()
	}
	systray.Run(onReady, l.stopStartedServer)
}
