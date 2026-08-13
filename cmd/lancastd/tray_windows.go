//go:build windows

package main

import (
	"context"
	"errors"
	"strings"
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
	/*
	 * The installed service comes first.
	 *
	 * Without this, a double-click on a machine where LANcast is installed as a
	 * service (and stopped) started a *second* server as the logged-in user,
	 * against the machine-wide data directory the service account owns — which
	 * fails with "attempt to write a readonly database (8)". The user's only
	 * route through was an elevated Start-Service: a terminal, which is what a
	 * double-click exists to avoid.
	 *
	 * So: if a service is installed, this launch is a request to start *it*, not
	 * to run a rival copy. One UAC prompt, then the UI opens when the service
	 * answers.
	 */
	if svc := installedService(); svc.Installed {
		return startServiceAndOpen(addr, svc)
	}

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
		// Settings, straight to the pane a person opening a tray menu is looking
		// for: which build is this, and is there a newer one. It is a deep link
		// rather than a dialog because the answer lives in the application and
		// duplicating it in a message box would give it two places to be wrong.
		mUpdates := systray.AddMenuItem("Check for updates", "Open the update settings")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Stop server and quit", "Stop the LANcast server and remove this icon")

		go func() {
			err := run(ctx, addr, dataDir, log)
			if err != nil && err != context.Canceled {
				log.Error("server exited", "error", err)
				// windowsgui has no console, so a bind clash or a bad data dir
				// would be silent without this — and the raw error is often
				// SQLite's, which explains nothing to the person reading it.
				alert("LANcast could not start", explainStartupFailure(err))
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
				case <-mUpdates.ClickedCh:
					url := desktop.ResolvedURL(addr) + "/settings?pane=updates"
					if err := desktop.OpenBrowser(url); err != nil {
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

// startServiceAndOpen is the launch path on a machine with the service
// installed: make sure it is running, then open the UI.
//
// No tray icon of its own. The service has no session to put one in and this
// process is about to exit — an icon that outlived the launch would be a second
// thing claiming to be the server, which is the confusion this whole path is
// removing.
func startServiceAndOpen(addr string, svc serviceState) error {
	if !svc.Running {
		if err := startInstalledService(); err != nil {
			if errors.Is(err, errCancelled) {
				// A refused prompt is a decision, and the user knows they made
				// it. Saying what did not happen is enough.
				alert("LANcast", "The LANcast service was not started, so the server is not running.")
				return nil
			}
			alert("LANcast could not start the service", err.Error()+
				"\n\nThe LANcast service is installed but could not be started.")
			return nil
		}
	}
	// Watching the port rather than the service state: "running" from the
	// service manager means the process started, not that it is listening, and
	// the browser only cares about the second one.
	if !desktop.WaitForServer(addr, 45*time.Second) {
		alert("LANcast", "The LANcast service was started but is not answering yet.\n\n"+
			"Give it a moment and open LANcast again.")
		return nil
	}
	return desktop.OpenBrowser(desktop.ResolvedURL(addr))
}

// explainStartupFailure turns the errors that actually happen into sentences
// with an action in them.
//
// The readonly-database case is the one that matters: it is what a launch used
// to do on a machine with a service installed, and "attempt to write a readonly
// database (8)" is a sentence about SQLite rather than about anything the user
// can do. That launch no longer happens — the service path above intercepts it —
// but the same error arrives whenever a data directory belongs to somebody else,
// and it should still say so.
func explainStartupFailure(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "readonly database") || strings.Contains(msg, "attempt to write a readonly") {
		return "LANcast could not write to its data folder.\n\n" +
			"This usually means the data belongs to the LANcast service account. " +
			"Start the LANcast service instead of running the server directly, " +
			"or launch LANcast with -data pointing at a folder you own.\n\n" +
			"Details: " + msg
	}
	if strings.Contains(msg, "address already in use") || strings.Contains(msg, "Only one usage of each socket address") {
		return "Something is already listening on LANcast's port.\n\n" +
			"Another LANcast server, or another program using the same port.\n\n" +
			"Details: " + msg
	}
	return msg
}
