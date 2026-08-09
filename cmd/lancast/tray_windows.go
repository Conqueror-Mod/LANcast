//go:build windows

package main

import (
	"runtime"

	"fyne.io/systray"

	"lancast/internal/branding"
	"lancast/internal/clientwindow"
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

// runWindowTray runs a tray icon beside an open window, on its own OS thread.
//
// The two cannot share a goroutine: each needs a Windows message loop, and a
// message queue belongs to a thread. systray's init() pins the main goroutine,
// and the web view wants main as well — so the tray gets a goroutine that locks
// a thread of its own, and the queues stay separate. That was the whole of the
// supposed conflict.
//
// show and quit are called from this thread and must marshal to the window's;
// clientwindow.Controller does that.
func runWindowTray(c clientwindow.Controller, stopping *bool) {
	go func() {
		// Without this the goroutine can be rescheduled onto another thread
		// between creating the window and pumping its messages, and the icon
		// appears with a menu that never responds.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		onReady := func() {
			systray.SetIcon(branding.IconICO)
			systray.SetTitle("LANcast")
			systray.SetTooltip("LANcast — running")
			mOpen := systray.AddMenuItem("Open LANcast", "Show the LANcast window")
			systray.AddSeparator()
			mQuit := systray.AddMenuItem("Quit", "Close LANcast and stop the server it started")

			go func() {
				for {
					select {
					case <-mOpen.ClickedCh:
						c.Show()
					case <-mQuit.ClickedCh:
						// Quit means quit: the window ends, Open returns, and
						// the caller's normal shutdown stops a server this
						// client started — and only that one.
						*stopping = true
						c.Close()
						systray.Quit()
						return
					}
				}
			}()
		}
		systray.Run(onReady, func() {})
	}()
}
