//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/systray"

	"lancast/internal/autostart"
	"lancast/internal/branding"
	"lancast/internal/desktop"
	"lancast/internal/raise"
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
		return runServiceTray(addr, svc)
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

/*
 * runServiceTray is the launch path on a machine with the service installed.
 *
 * It makes sure the service is running and then **stays**, as a controller for
 * it — a tray icon that opens LANcast, launches the app, and can be told to
 * start with Windows.
 *
 * It used to start the service, open a browser and exit, on the reasoning that
 * "an icon that outlived the launch would be a second thing claiming to be the
 * server". That reasoning was right about a process that *is* a server and
 * wrong about this one, which is not: the server is the service, and this holds
 * no port, no database and no lock. Reported as the executable being more or
 * less useless — clicking it flashed a browser and vanished, so there was
 * nothing to click a second time.
 *
 * The distinction is load-bearing enough to be in the words on the menu.
 * **Exit removes the icon and leaves the server running**, because stopping a
 * Windows service needs elevation and a tray item that silently did nothing —
 * or worse, raised a UAC prompt for something labelled "Exit" — would be the
 * confusion the old comment was guarding against, arriving from the other side.
 */
func runServiceTray(addr string, svc serviceState) error {
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

	log := newLogger(false)

	onReady := func() {
		systray.SetIcon(branding.IconICO)
		systray.SetTitle("LANcast")
		systray.SetTooltip("LANcast — the server runs as a Windows service")

		mOpen := systray.AddMenuItem("Open LANcast", "Open LANcast in your browser")
		mApp := systray.AddMenuItem("Open the LANcast app", "Open the LANcast desktop window")
		/*
		 * Quitting the app from here, because there is nowhere else.
		 *
		 * The client used to carry its own tray icon whenever close-to-tray was
		 * on, which put a second LANcast icon in the notification area beside
		 * this one — reported as exactly that. Removing it needed a
		 * replacement rather than a deletion: with close-to-tray on the
		 * window's X *hides*, so that icon was the only way to quit the app at
		 * all.
		 *
		 * Distinct from Exit below, and the wording carries the difference:
		 * this closes the window, that removes this icon, and neither stops the
		 * server.
		 */
		mQuitApp := systray.AddMenuItem("Quit the LANcast app", "Close the LANcast window")
		systray.AddSeparator()
		mLogin := systray.AddMenuItemCheckbox("Start LANcast at login",
			"Show this icon when you sign in. The server already starts on its own.", false)
		systray.AddSeparator()
		/*
		 * Both of these open a page rather than doing the thing, and both say
		 * so. A scan is an admin action on an authenticated API and this
		 * process holds no session — it is not the server and has no
		 * credentials of its own. A menu item that quietly failed, or one that
		 * invented a local back door into an authenticated endpoint, would be
		 * worse than one that takes you where the button is.
		 */
		mLibraries := systray.AddMenuItem("Update libraries…", "Open the library settings, where scanning lives")
		mUpdates := systray.AddMenuItem("Check for updates…", "Open the update settings")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Exit", "Remove this icon. The LANcast server keeps running.")

		if on, err := autostart.Enabled(); err == nil && on {
			mLogin.Check()
		}

		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					if err := desktop.OpenBrowser(desktop.ResolvedURL(addr)); err != nil {
						log.Warn("could not open browser", "error", err)
					}
				case <-mQuitApp.ClickedCh:
					// Nobody listening means no app is running, which is what
					// somebody pressing this wanted anyway.
					if err := raise.Quit(); err != nil {
						log.Warn("could not ask the app to quit", "error", err)
					}
				case <-mApp.ClickedCh:
					if err := openClientApp(); err != nil {
						// Said out loud: the app is the front door (ADR 0023),
						// and a menu item that does nothing is the failure this
						// whole change is about.
						alert("LANcast", "Could not open the LANcast app.\n\n"+err.Error())
					}
				case <-mLogin.ClickedCh:
					toggleLogin(mLogin, log)
				case <-mLibraries.ClickedCh:
					openPane(addr, "libraries", log)
				case <-mUpdates.ClickedCh:
					openPane(addr, "updates", log)
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()
	}

	// Nothing to cancel: this process owns no server. Exit is exactly what it
	// says — the icon goes and the service carries on.
	systray.Run(onReady, func() {})
	return nil
}

// openPane opens the web UI at one settings pane.
func openPane(addr, pane string, log *slog.Logger) {
	if err := desktop.OpenBrowser(desktop.ResolvedURL(addr) + "/settings?pane=" + pane); err != nil {
		log.Warn("could not open browser", "error", err)
	}
}

/*
 * toggleLogin flips the run key and the tick together.
 *
 * The tick is set from what the write actually did rather than from what was
 * asked for: a registry write that failed and a menu that ticked anyway is a
 * setting that lies about itself, which is worse than one that refuses.
 */
func toggleLogin(item *systray.MenuItem, log *slog.Logger) {
	if item.Checked() {
		if err := autostart.Disable(); err != nil {
			log.Warn("could not disable autostart", "error", err)
			return
		}
		item.Uncheck()
		return
	}
	// The same arguments this launch used, or the icon comes back at login
	// pointed at a different data directory from the one it is controlling.
	if err := autostart.Enable("tray"); err != nil {
		log.Warn("could not enable autostart", "error", err)
		return
	}
	item.Check()
}

/*
 * openClientApp launches the desktop window.
 *
 * Beside this executable, because that is where the installer puts it and
 * because a PATH lookup would find whatever else is called LANcast-Client. A
 * second launch of the client raises the window it already has rather than
 * adding a process, so pressing this twice is harmless.
 */
func openClientApp() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	client := filepath.Join(filepath.Dir(exe), "LANcast-Client.exe")
	if _, err := os.Stat(client); err != nil {
		return fmt.Errorf("LANcast-Client.exe is not beside the server: %w", err)
	}
	return exec.Command(client).Start()
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
