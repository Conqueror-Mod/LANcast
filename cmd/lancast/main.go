// Command lancast is the LANcast client launcher (ADR 0022): a tiny, no-console
// executable a user double-clicks. It finds or starts the server, opens the UI
// in the default browser, and sits in the system tray. Pure Go, no CGO.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"lancast/internal/autostart"
	"lancast/internal/certpin"
	"lancast/internal/childproc"
	"lancast/internal/clientwindow"
	"lancast/internal/config"
	"lancast/internal/desktop"
	"lancast/internal/desktopprefs"
	"lancast/internal/service"
	"lancast/internal/singleton"
)

// Version is the client build, injected at release time exactly as the server's
// is. It exists so the page can notice that this window is older than the
// server it is talking to — which is an ordinary state, not an exotic one: the
// in-app updater replaces the server and the web assets it serves, and cannot
// replace a running client. Until somebody restarts the app, the window is the
// previous release and nothing said so.
var Version = "dev"

// browserMode records whether this client was launched with -browser, so the
// autostart entry reproduces the same mode rather than quietly changing it.
var browserMode bool

func main() {
	addr := flag.String("addr", ":8080", "server listen address")
	// LANcast's own window is the front door (ADR 0023). The browser is the
	// fallback, not the destination: it cannot be told what its close button
	// means, it cannot pin a certificate, and against a LAN-bound server with a
	// self-signed certificate it shows a warning the window does not need.
	//
	// -browser forces it anyway, and a machine with no WebView2 runtime falls
	// back on its own — so the browser stays reachable without being the
	// default. -window is kept as a no-op alias so existing shortcuts, the run
	// key written by "open at login", and anything in a script keep working.
	browser := flag.Bool("browser", false,
		"show the UI in your default browser instead of the LANcast window")
	window := flag.Bool("window", false,
		"deprecated: the LANcast window is the default; kept so existing shortcuts still work")
	// Mirrors lancastd's own flag. Without it the client guesses — machine-wide
	// if a database is already there, per-user otherwise — which is right for
	// every normal install and wrong for one run with a custom directory. It
	// matters more than it used to: the window reads the server's certificate
	// from this directory to know what to trust, so guessing wrong now means a
	// window that will not load rather than merely a second database.
	dataDir := flag.String("data", "",
		"server data directory (default: the machine-wide one if present, else per-user)")
	flag.Parse()
	// Remembered because "open at login" has to reproduce this launch: someone
	// who runs in the browser should get the browser at login, not a window.
	_ = *window // accepted and ignored; the window is the default now
	browserMode = *browser

	// One client at a time. Launching again — a second double-click of the
	// shortcut — reopens the UI rather than adding a duplicate process and a
	// second tray icon.
	release, held, err := singleton.Acquire(singleton.Client)
	if err == nil && !held {
		_ = desktop.OpenBrowser(desktop.ResolvedURL(*addr))
		return
	}
	defer release()

	l := &launcher{addr: *addr, dataDir: *dataDir}
	if err := l.ensureServer(); err != nil {
		alert("LANcast", err.Error())
		os.Exit(1)
	}

	if !*browser {
		runWindow(l)
		return
	}

	if err := desktop.OpenBrowser(desktop.ResolvedURL(l.addr)); err != nil {
		alert("LANcast", "could not open the browser: "+err.Error())
	}
	runLauncherTray(l)
}

// runWindow shows the UI in a window this program owns, and exits when it
// closes — stopping a server this launcher started, exactly as Quit does.
//
// **No tray in this mode, deliberately.** The web view and the tray each want
// to own the main thread's message loop, and more importantly "what should
// closing the window do" is the question docs/desktop-lifecycle-plan.md exists
// to answer (close-to-tray, open-on-start). Answering it here by accident,
// before that plan is built, is how a default nobody chose becomes permanent.
// The window is the app; closing it closes the app.
//
// A machine with no WebView2 runtime falls back to the browser rather than
// failing: it is a supported configuration, and refusing to start would trade
// a working app for a purer one.
func runWindow(l *launcher) {
	defer l.stopStartedServer()

	// Say which of the two things is missing. "Install the WebView2 runtime" is
	// the wrong instruction when the runtime is fine and the shipped DLL is not.
	if err := clientwindow.Check(); err != nil {
		alert("LANcast", "LANcast opened in your browser instead.\n\n"+err.Error())
		if err := desktop.OpenBrowser(desktop.ResolvedURL(l.addr)); err != nil {
			alert("LANcast", "could not open the browser: "+err.Error())
		}
		runLauncherTray(l)
		return
	}

	// Read once, at open. Changing the preference mid-session and expecting the
	// current window's close button to change under you is a worse surprise
	// than the setting taking effect next launch.
	prefs, err := desktopprefs.Load(clientDataDir())
	if err != nil {
		// Not fatal: the default is off, which is the safe behaviour.
		prefs = desktopprefs.Prefs{}
	}

	// quitting distinguishes "the user chose Quit" from "the user clicked the
	// window's X". Without it, Quit would be intercepted by close-to-tray and
	// the app could never be closed at all — a tray whose Quit hides the window
	// is the worst version of this feature.
	var quitting bool
	var tray clientwindow.Controller

	err = clientwindow.Open(clientwindow.Options{
		URL:    desktop.ResolvedURL(l.addr),
		Title:  "LANcast",
		Width:  1280,
		Height: 800,
		OnClose: func() bool {
			if quitting || !prefs.CloseToTray || tray == nil {
				return true
			}
			tray.Hide()
			return false
		},
		OnReady: func(c clientwindow.Controller) {
			if !prefs.CloseToTray {
				return
			}
			// The tray exists only when close-to-tray is on. An icon that
			// appears for everyone would be a second thing to explain, and
			// without the preference there is nothing for it to do.
			tray = c
			runWindowTray(c, &quitting)
		},
		// Beside the client's own config, not the server's data directory: this
		// is one person's session and cache on one machine, and the server's
		// directory is machine-wide and may belong to a service account.
		DataDir:  clientDataDir(),
		CertPin:  l.serverCertPin(),
		DevTools: devToolsWanted(),
		Bindings: l.desktopBindings(),
	})
	if err != nil {
		alert("LANcast", err.Error())
	}
}

// desktopBindings exposes the lifecycle preferences to the page.
//
// Two functions and one fact. The fact is the important one: only this process
// knows whether it started the server or attached to one that was already
// running, and that is what decides whether closing the window may stop
// anything at all. The server cannot answer it — from its side both look
// identical — and the page cannot infer it.
func (l *launcher) desktopBindings() map[string]any {
	dir := clientDataDir()
	return map[string]any{
		// lancastDesktopState reports the current preferences and how this
		// window relates to its server.
		"lancastDesktopState": func() map[string]any {
			prefs, err := desktopprefs.Load(dir)
			// Read from the registry rather than the preferences file. They can
			// disagree — an uninstall clears the run key, another tool removes
			// it, a profile is copied between machines — and the registry is the
			// one that decides what actually happens at login. Showing the file
			// would be showing an intention rather than a fact.
			atLogin, autoErr := autostart.Enabled()
			state := map[string]any{
				// What this window is, so the page can compare it with what the
				// server says it is.
				"client_version": Version,
				"close_to_tray":  prefs.CloseToTray,
				// Reported from the file rather than from the running window,
				// because the window cannot change it after launch: this is
				// what the *next* start will do, which is what the toggle is
				// promising.
				"devtools":      prefs.DevTools,
				"open_at_login": atLogin,
				// True when this launcher started the server, so closing the
				// window ends it. False when a service or an earlier launch
				// owns it, in which case closing this window stops nothing —
				// and the page should say so rather than let the user assume.
				"owns_server": l.started != nil,
				// Who holds it when this window does not. Asserting "the
				// service" for anything this client did not start would be a
				// claim about the user's machine that nothing checked — and it
				// is wrong for a server run from a terminal, which is exactly
				// how this got caught.
				"holder": l.serverHolder(),
			}
			if err == nil {
				err = autoErr
			}
			if err != nil {
				// Surfaced rather than swallowed: the user is looking at
				// controls that will not reflect their machine.
				state["error"] = err.Error()
			}
			return state
		},
		// lancastDesktopSet writes both preferences. Whole-value rather than
		// per-field so the page cannot half-apply a change.
		/*
		 * A third argument rather than a second function.
		 *
		 * The page sends the whole preference set every time, so adding one
		 * means one more parameter here and nothing at the call site has to
		 * know whether it changed. A separate lancastDevTools() would be a
		 * second way to write the same file, free to race the first.
		 */
		"lancastDesktopSet": func(closeToTray, openAtLogin, devTools bool) map[string]any {
			// The registry is the thing that actually starts LANcast at login,
			// so it goes first: a preference file saying "on" over a run key
			// that was never written is a setting that lies. If this fails the
			// preference is not saved either, and the two stay in agreement.
			if err := applyAutostart(openAtLogin); err != nil {
				return map[string]any{"ok": false, "error": err.Error()}
			}
			p := desktopprefs.Prefs{CloseToTray: closeToTray, OpenAtLogin: openAtLogin, DevTools: devTools}
			if err := desktopprefs.Save(dir, p); err != nil {
				return map[string]any{"ok": false, "error": err.Error()}
			}
			return map[string]any{"ok": true}
		},
	}
}

// serverCertPin is the public key of the server's own certificate, or empty
// when there is nothing to pin.
//
// Beyond loopback the server serves a self-signed certificate (ADR 0014), and
// the web view refuses it outright — no warning, no way through, just a window
// that never loads. Reading the key off local disk is what lets the client
// trust that one server and still reject everything else.
//
// Empty on any doubt, and deliberately quiet about it: a loopback-only server
// has no certificate and needs none, so the common case would otherwise warn
// about nothing. The cost of being wrong is the window failing to load, which
// is loud on its own.
func (l *launcher) serverCertPin() string {
	dir, ok := l.serverDataDir()
	if !ok {
		var err error
		if dir, err = config.DefaultDataDir(); err != nil {
			return ""
		}
	}
	pin, err := certpin.SPKI(dir)
	if err != nil {
		return ""
	}
	return pin
}

// clientDataDir is where the window keeps its profile — the session cookie
// lives here, so it has to be the same directory on every launch or signing in
// would not stick.
func clientDataDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		// No config dir is survivable: the web view falls back to its own
		// default, and the cost is a sign-in that may not persist — worth a
		// worse session, not worth refusing to open.
		return ""
	}
	return filepath.Join(dir, "LANcast", "client")
}

// launcher coordinates the server for the client. If it starts the server it
// keeps the handle, so quitting the launcher stops the server it spawned — a
// server the user did not know was running should not outlive the app that
// started it. A server already running (a service, or a prior launch) is left
// alone.
type launcher struct {
	addr string
	// dataDir is the operator's explicit choice, empty when they made none.
	dataDir string
	started *exec.Cmd
}

// serverDataDir is the directory the server uses: what was asked for, or the
// machine-wide one when a database already lives there, or nothing — in which
// case the server applies its own per-user default and the client must not
// pretend to know better.
func (l *launcher) serverDataDir() (string, bool) {
	if l.dataDir != "" {
		return l.dataDir, true
	}
	return sharedDataDir()
}

// ensureServer opens the app to a running server, starting the sibling lancastd
// (windowless) first if nothing is answering.
func (l *launcher) ensureServer() error {
	if desktop.ServerRunning(l.addr) {
		return nil
	}
	exe, err := serverExePath()
	if err != nil {
		return err
	}
	args := []string{"-addr", l.addr}
	// Pin the data directory when a machine-wide one already exists.
	//
	// Without this the spawned server takes the per-user default while the
	// installed service uses the machine-wide one — two servers, two databases,
	// same port. Whichever won the port decided which library you saw, and
	// launching the app after the service failed to start silently created an
	// empty second database rather than showing the real one.
	if dir, ok := l.serverDataDir(); ok {
		args = append(args, "-data", dir)
	}
	cmd := exec.Command(exe, args...)
	childproc.Hide(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start server %s: %w", exe, err)
	}
	l.started = cmd
	if !desktop.WaitForServer(l.addr, 20*time.Second) {
		return fmt.Errorf("the server did not come up at %s", desktop.UIURL(l.addr))
	}
	return nil
}

// stopStartedServer stops the server only if this launcher started it, and does
// not return until it is actually gone.
//
// The ownership rule (docs/desktop-lifecycle-plan.md): closing a window never
// stops a server the window does not own. A service, or a server an earlier
// launch started, keeps running — someone else may be streaming from it, and a
// media server does not stop because one person shut a window.
//
// Waiting matters as much as killing. Returning while the process is still
// exiting is how a launcher reports "closed" over a server that is still holding
// the port and the database, which is the invisible-hanging-process case this
// exists to prevent. The wait is bounded so a process that will not die cannot
// hang the close either.
func (l *launcher) stopStartedServer() {
	if l.started == nil || l.started.Process == nil {
		return
	}
	if err := l.started.Process.Kill(); err != nil {
		// Already gone is the common case and not a failure.
		if !errors.Is(err, os.ErrProcessDone) {
			alert("LANcast", "The LANcast server could not be stopped: "+err.Error()+
				"\n\nIt may still be running in the background.")
			return
		}
	}
	done := make(chan struct{})
	go func() {
		// Cmd.Wait rather than Process.Wait: it reaps the child *and* records
		// its ProcessState, so "has it actually exited" is answerable
		// afterwards instead of assumed. The error is the kill showing up as a
		// non-zero exit, which is expected.
		_ = l.started.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(stopWait):
		// Said out loud rather than left for the user to discover through a
		// port that is still in use. Silence here is exactly the failure the
		// lifecycle rule forbids.
		alert("LANcast", "The LANcast server did not exit within "+stopWait.String()+
			".\n\nIt may still be running in the background.")
	}
}

// stopWait bounds how long closing the window waits for the server it started to
// exit. Long enough for a normal exit, short enough that closing the app never
// feels like it hung.
const stopWait = 5 * time.Second

// serverExePath is the lancastd binary next to the launcher — they ship together.
func serverExePath() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe := filepath.Join(filepath.Dir(self), serverExeName())
	if _, err := os.Stat(exe); err != nil {
		return "", fmt.Errorf("lancastd not found next to the launcher (%s)", exe)
	}
	return exe, nil
}

// sharedDataDir reports the machine-wide data directory when one is already in
// use, so a client-started server opens the same database the service does.
//
// Only when it already holds a database: on a machine with no service install
// the per-user default is right, and creating a machine-wide directory here
// would need privileges the client does not have and should not want.
func sharedDataDir() (string, bool) {
	dir := service.DefaultDataDir(runtime.GOOS)
	if dir == "" {
		return "", false
	}
	if _, err := os.Stat(filepath.Join(dir, "lancast.db")); err != nil {
		return "", false
	}
	return dir, true
}

func serverExeName() string {
	if runtime.GOOS == "windows" {
		return "LANcast-Server.exe"
	}
	return "LANcast-Server"
}

// serverHolder names what is serving when this client did not start it.
//
// "self" when this window started it, "service" when the installed OS service
// holds it, and "other" for anything else — another launch, or a build someone
// is running from a terminal. The distinction matters because the advice
// differs: a service is stopped from Windows Services, and everything else is
// stopped wherever it was started.
func (l *launcher) serverHolder() string {
	if l.started != nil {
		return "self"
	}
	if running, ok := service.RunningServer(); ok && running.Service {
		return "service"
	}
	return "other"
}

// applyAutostart makes the run key match the preference.
//
// It passes the flags this client was launched with, so a window user who turns
// this on gets a window at login rather than a browser — a login that opened the
// wrong interface would look like a different bug entirely.
func applyAutostart(on bool) error {
	if !on {
		return autostart.Disable()
	}
	var args []string
	if browserMode {
		args = append(args, "-browser")
	}
	return autostart.Enable(args...)
}

/*
 * devToolsWanted reads the preference before the window exists.
 *
 * Its own function because the browser arguments are read at environment
 * creation, so this has to happen before anything else in the window setup —
 * and because a failure to read the file must not stop the app opening. A
 * missing or unreadable preference means off, which is the same answer the
 * defaults give.
 */
func devToolsWanted() bool {
	prefs, err := desktopprefs.Load(clientDataDir())
	return err == nil && prefs.DevTools
}
