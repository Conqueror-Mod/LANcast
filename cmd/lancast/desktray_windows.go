//go:build windows

package main

import (
	"log/slog"
	"runtime"

	"fyne.io/systray"

	"lancast/internal/autostart"
	"lancast/internal/branding"
)

// hasTray says whether this platform gives the window a tray to hide into.
const hasTray = true

/*
 * startDesktopTray shows LANcast's one icon (ADR 0080), on its own OS thread.
 *
 * The window keeps the main thread; the tray gets a goroutine that locks a
 * thread of its own, because each needs its own message loop and a Windows
 * message queue belongs to a thread. That is the arrangement 991875f8 shipped,
 * and the only reason it was ever thought impossible was trying to run both
 * from one goroutine.
 *
 * The menu is modelled on Plex's. Every item that changes something on the
 * server — a scan — goes through the signed-in page rather than holding any
 * authority of its own (ADR 0080 §3): it opens Settings at the libraries pane
 * with a request the page carries out as the signed-in person, or asks them to
 * sign in.
 */
func startDesktopTray(d *desk, log *slog.Logger) (stop func()) {
	go func() {
		// Without this the goroutine can move threads between creating the
		// icon's window and pumping its messages, and the menu never answers.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		systray.Run(func() { buildTrayMenu(d, log) }, func() {})
	}()
	return systray.Quit
}

func buildTrayMenu(d *desk, log *slog.Logger) {
	systray.SetIcon(branding.IconICO)
	systray.SetTitle("LANcast")
	systray.SetTooltip("LANcast")

	mOpen := systray.AddMenuItem("Open LANcast", "Show the LANcast window")
	systray.AddSeparator()
	mLogin := systray.AddMenuItemCheckbox("Start LANcast at login",
		"Put LANcast in the notification area when you sign in", false)
	systray.AddSeparator()
	mScan := systray.AddMenuItem("Update libraries", "Scan every library for changes")
	/*
	 * Greyed except while a scan is running, which only the page can say
	 * (desk.setScanning). Cancelling keeps what was found and marks nothing
	 * the scan had not reached as missing (#847).
	 */
	mCancel := systray.AddMenuItem("Cancel library update", "Stop the scans that are running")
	mCancel.Disable()
	d.mu.Lock()
	d.onScanning = func(on bool) {
		if on {
			mCancel.Enable()
		} else {
			mCancel.Disable()
		}
	}
	d.mu.Unlock()
	systray.AddSeparator()
	mUpdates := systray.AddMenuItem("Check for updates…", "Open the update settings")
	mAbout := systray.AddMenuItem("About LANcast…", "Version and licence")
	systray.AddSeparator()
	/*
	 * Exit closes the window and this icon. It never stops the service: that
	 * is an administrator action behind a UAC prompt, and an item called Exit
	 * must not be what raises one. A server this process started for itself
	 * (no service installed) does stop, because nothing else owns it.
	 */
	mExit := systray.AddMenuItem("Exit", "Close LANcast. A LANcast service keeps running.")

	if on, err := autostart.Enabled(autostart.Client); err == nil && on {
		mLogin.Check()
	}

	/*
	 * One goroutine per item, not one select over all of them.
	 *
	 * systray delivers a click with a non-blocking send on an unbuffered
	 * channel: if nothing is waiting on that exact channel at that instant,
	 * the click is dropped, silently. One select over every item made that a
	 * shared fate — a slow handler dropped every other item's clicks. Learned
	 * on the server's tray, where "Start at login" moved its tick and did
	 * nothing.
	 */
	watch := func(ch <-chan struct{}, fn func()) {
		go func() {
			for range ch {
				fn()
			}
		}()
	}
	watch(mOpen.ClickedCh, func() { d.open("") })
	watch(mLogin.ClickedCh, func() { toggleLogin(mLogin, log) })
	watch(mScan.ClickedCh, func() { d.open(d.base + "/settings?pane=libraries&tray=scan") })
	watch(mCancel.ClickedCh, func() { d.cancelScan() })
	watch(mUpdates.ClickedCh, func() { d.open(d.base + "/settings?pane=general") })
	watch(mAbout.ClickedCh, func() { d.open(d.base + "/settings?pane=about") })
	watch(mExit.ClickedCh, func() { d.exit() })
}

/*
 * toggleLogin flips the run key and then shows what it actually is.
 *
 * The intent comes from the registry, never from the widget: Windows ticks a
 * checkbox menu item itself on click, so systray's own idea of it is stale the
 * moment it is clicked, and an intent computed from it is the opposite of what
 * was meant. And the tick is set from a read-back, so it can never claim a run
 * key that was not written. Both were watched failing on the server's tray.
 */
func toggleLogin(item *systray.MenuItem, log *slog.Logger) {
	was, err := autostart.Enabled(autostart.Client)
	if err != nil {
		log.Warn("could not read autostart", "error", err)
		return
	}
	if err := applyAutostart(!was); err != nil {
		log.Warn("could not change autostart", "wanted", !was, "error", err)
	}
	on, err := autostart.Enabled(autostart.Client)
	if err != nil {
		log.Warn("could not read autostart back", "error", err)
		return
	}
	if on {
		item.Check()
	} else {
		item.Uncheck()
	}
}

// waitForExit blocks until Exit is chosen, for the launches that have no
// window to block on (the browser).
func waitForExit(d *desk, _ *launcher) { <-d.exitReq }
