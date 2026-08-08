// Command lancast is the LANcast client launcher (ADR 0022): a tiny, no-console
// executable a user double-clicks. It finds or starts the server, opens the UI
// in the default browser, and sits in the system tray. Pure Go, no CGO.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"lancast/internal/childproc"
	"lancast/internal/clientwindow"
	"lancast/internal/desktop"
	"lancast/internal/service"
	"lancast/internal/singleton"
)

func main() {
	addr := flag.String("addr", ":8080", "server listen address")
	// Opt-in for now (ADR 0023 stage 1). The browser stays the default until the
	// window has been lived with, so the two can be compared on one machine on
	// one day and switching back is a flag rather than a rebuild.
	window := flag.Bool("window", false,
		"show the UI in a LANcast window instead of the default browser (Windows)")
	flag.Parse()

	// One client at a time. Launching again — a second double-click of the
	// shortcut — reopens the UI rather than adding a duplicate process and a
	// second tray icon.
	release, held, err := singleton.Acquire(singleton.Client)
	if err == nil && !held {
		_ = desktop.OpenBrowser(desktop.ResolvedURL(*addr))
		return
	}
	defer release()

	l := &launcher{addr: *addr}
	if err := l.ensureServer(); err != nil {
		alert("LANcast", err.Error())
		os.Exit(1)
	}

	if *window {
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

	err := clientwindow.Open(clientwindow.Options{
		URL:    desktop.ResolvedURL(l.addr),
		Title:  "LANcast",
		Width:  1280,
		Height: 800,
		// Beside the client's own config, not the server's data directory: this
		// is one person's session and cache on one machine, and the server's
		// directory is machine-wide and may belong to a service account.
		DataDir: clientDataDir(),
	})
	if err != nil {
		alert("LANcast", err.Error())
	}
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
	addr    string
	started *exec.Cmd
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
	if dir, ok := sharedDataDir(); ok {
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

// stopStartedServer stops the server only if this launcher started it.
func (l *launcher) stopStartedServer() {
	if l.started != nil && l.started.Process != nil {
		_ = l.started.Process.Kill()
	}
}

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
