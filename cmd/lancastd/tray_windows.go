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

	"lancast/internal/applog"
	"lancast/internal/autostart"
	"lancast/internal/branding"
	"lancast/internal/config"
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
	/*
	 * One tray icon, whichever path this launch takes.
	 *
	 * Acquired before the service check because both branches below put an icon
	 * in the notification area, and the server's own lock says nothing about
	 * the tray: on an installed machine the server is a service and the tray is
	 * a user-session process controlling it. Two launches gave two identical
	 * icons for one service, and quitting one left the other behind.
	 *
	 * A second launch is a request to see LANcast, not to add an icon — the
	 * same reading `openExisting` already gives a second server launch.
	 */
	trayRelease, trayHeld, trayErr := singleton.Acquire(singleton.Tray)
	if trayErr == nil && !trayHeld {
		return openExisting(addr)
	}
	defer trayRelease()

	if svc := installedService(); svc.Installed {
		return runServiceTray(addr, dataDir, svc)
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
func runServiceTray(addr, dataDir string, svc serviceState) error {
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

	/*
	 * A log that goes somewhere.
	 *
	 * newLogger writes to stderr, and this process is a GUI application with no
	 * console — so every warning it has ever produced went into nothing. That
	 * is how a tray toggle that silently failed could be watched failing, over
	 * and over, with no way to see why: the code reported it correctly and the
	 * report had nowhere to land.
	 *
	 * Its own file rather than the server's, because both rotate by renaming at
	 * a size threshold and two processes doing that to one file is a race that
	 * ends with the server's log truncated.
	 *
	 * A logging failure is reported to the void it replaces and then ignored:
	 * not being able to write a log is not a reason to refuse to show an icon.
	 */
	logDir := dataDir
	if logDir == "" {
		// The tray subcommand defaults its data directory to the per-user
		// config dir, and an empty string here would drop the log in whatever
		// the working directory happens to be — which for a shortcut is
		// anybody's guess.
		if d, err := config.DefaultDataDir(); err == nil {
			logDir = d
		}
	}
	if lf, err := applog.OpenNamed(logDir, applog.TrayFileName); err != nil {
		log.Warn("could not open the tray log", "error", err)
	} else {
		defer lf.Close()
		// Tee rather than io.MultiWriter, for the reason the service uses it:
		// MultiWriter stops at the first error, and with no console stderr can
		// fail — which would silence the file too, in exactly the situation the
		// file exists for.
		log = slog.New(slog.NewTextHandler(applog.Tee(lf, os.Stderr), nil))
		log.Info("tray started", "log", lf.Path())
	}

	/*
	 * Say that a tray exists, for as long as this one does.
	 *
	 * The client hides its window on close rather than closing it, and that is
	 * only a feature while something can bring it back. Since the client gave
	 * up its own icon — two LANcast icons was the complaint — this tray is that
	 * something, and nothing starts it: a machine that has only booted runs the
	 * service with no tray at all. The client asks this question before hiding,
	 * so the answer has to be published by the thing that would do the
	 * restoring.
	 *
	 * Released on the way out, and released by Windows anyway if this process
	 * dies badly, which is the reason it is a handle rather than a file.
	 */
	if release, err := raise.HoldTray(); err == nil {
		defer release()
	} else {
		// Not fatal. The tray still works; the client just falls back to
		// closing on X, which is the safe direction.
		log.Warn("could not publish the tray's presence", "error", err)
	}

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
		/*
		 * Exit ends this session's LANcast: the window and this icon.
		 *
		 * It used to remove only the icon, which left the window open and — the
		 * part that did damage — left *this* process running out of the install
		 * directory. Reported as closing LANcast from the tray and finding the
		 * processes still there, which then fouled the next update: a resident
		 * image in Program Files is exactly what an update has to move aside.
		 *
		 * The complaint underneath it was a fair reading of the menu. "Quit the
		 * LANcast app" and "Exit" were two partial endings and neither was the
		 * one somebody means by closing LANcast, so whichever they chose left
		 * something behind.
		 *
		 * The service is untouched and the wording says so. Stopping it is an
		 * administrator action with a UAC prompt behind it, and a menu item
		 * labelled Exit must not be the thing that raises one.
		 */
		mQuit := systray.AddMenuItem("Exit",
			"Close the LANcast app and remove this icon. The LANcast server keeps running.")

		if on, err := autostart.Enabled(autostart.Tray); err == nil && on {
			mLogin.Check()
		}

		/*
		 * One goroutine per item, not one select over all of them.
		 *
		 * systray delivers a click with a **non-blocking send on an unbuffered
		 * channel**: if nothing is blocked on that exact channel at that
		 * instant, the click is dropped. There is no queue and no error — an
		 * undelivered click simply never happened.
		 *
		 * One select over every item made that a shared fate. While the single
		 * goroutine was inside *any* handler — opening a browser, waiting on a
		 * modal — every other item's clicks were discarded. That is how "Start
		 * LANcast at login" could move its tick, which Windows draws itself,
		 * while its handler never ran and nothing was ever logged.
		 *
		 * A goroutine each means every channel always has a waiter, and a slow
		 * handler can only ever delay its own item.
		 */
		watch := func(ch <-chan struct{}, fn func()) {
			go func() {
				for range ch {
					fn()
				}
			}()
		}

		watch(mOpen.ClickedCh, func() {
			if err := desktop.OpenBrowser(desktop.ResolvedURL(addr)); err != nil {
				log.Warn("could not open browser", "error", err)
			}
		})
		watch(mQuitApp.ClickedCh, func() {
			// Nobody listening means no app is running, which is what somebody
			// pressing this wanted anyway.
			if err := raise.Quit(); err != nil {
				log.Warn("could not ask the app to quit", "error", err)
			}
		})
		watch(mApp.ClickedCh, func() {
			if err := openClientApp(); err != nil {
				// Said out loud: the app is the front door (ADR 0023), and a
				// menu item that does nothing is the failure this whole change
				// is about.
				alert("LANcast", "Could not open the LANcast app.\n\n"+err.Error())
			}
		})
		watch(mLogin.ClickedCh, func() { toggleLogin(mLogin, log) })
		watch(mLibraries.ClickedCh, func() { openPane(addr, "libraries", log) })
		watch(mUpdates.ClickedCh, func() { openPane(addr, "updates", log) })
		watch(mQuit.ClickedCh, func() {
			/*
			 * The window first, then the icon.
			 *
			 * Ordered so the app is asked to go while something is still here
			 * to ask it: once systray.Quit has run this process is on its way
			 * out, and a Quit sent from a dying process is a race nobody needs
			 * to debug.
			 *
			 * Nobody listening is not a failure — it means no window was open,
			 * which is what somebody pressing this wanted.
			 */
			if err := raise.Quit(); err != nil {
				log.Warn("could not ask the app to quit", "error", err)
			}
			systray.Quit()
		})
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
/*
 * toggleLogin flips the setting and then shows what the setting actually is.
 *
 * The tick used to be set from what was *attempted*: check the item after
 * Enable returned no error, uncheck it after Disable did. That reads as
 * reasonable and it made the menu capable of lying — a tick appeared beside a
 * run key that had not been written, and the only other report of the failure
 * went to a logger with nowhere to write. Watched doing exactly that: the tick
 * moved on every click and the registry never changed.
 *
 * So the state is read back from the registry afterwards and the tick is set
 * from that. It cannot then claim something the machine does not agree with,
 * and a failure shows up as a tick that does not move — which is a symptom
 * somebody can report.
 */
func toggleLogin(item *systray.MenuItem, log *slog.Logger) {
	/*
	 * What to do next comes from the registry, never from the widget.
	 *
	 * This used to read `!item.Checked()`, which is the obvious thing and is
	 * wrong here. Windows toggles a checkbox menu item's *visible* tick itself
	 * when it is clicked; systray's `Checked()` returns its own idea, which only
	 * Check and Uncheck change. The two drift apart the moment the native toggle
	 * happens, and the intent computed from the stale one is the opposite of
	 * what the person clicking meant.
	 *
	 * Watched doing exactly that: the tick moved on every click, the run key
	 * never changed, and nothing was logged — because each click was computing
	 * "turn it off" against a setting that was already off, succeeding, and
	 * agreeing with itself.
	 *
	 * The registry is the setting. Asking it removes the widget from the
	 * decision, which is the same reason the tick is set from a read-back
	 * rather than from what was attempted.
	 */
	was, err := autostart.Enabled(autostart.Tray)
	if err != nil {
		log.Warn("could not read autostart", "error", err)
		return
	}
	want := !was

	if want {
		// The same arguments this launch used, or the icon comes back at login
		// pointed at a different data directory from the one it is controlling.
		err = autostart.Enable(autostart.Tray, "tray")
	} else {
		err = autostart.Disable(autostart.Tray)
	}
	if err != nil {
		log.Warn("could not change autostart", "wanted", want, "error", err)
	}

	on, rerr := autostart.Enabled(autostart.Tray)
	if rerr != nil {
		// Nothing trustworthy to show, so show nothing new rather than guess.
		log.Warn("could not read autostart back", "error", rerr)
		return
	}
	if on {
		item.Check()
	} else {
		item.Uncheck()
	}
	if on != want {
		log.Warn("autostart did not take", "wanted", want, "is", on)
	}
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
