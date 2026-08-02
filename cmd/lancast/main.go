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

	"lancast/internal/desktop"
	"lancast/internal/singleton"
)

func main() {
	addr := flag.String("addr", ":8080", "server listen address")
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
	if err := desktop.OpenBrowser(desktop.ResolvedURL(l.addr)); err != nil {
		alert("LANcast", "could not open the browser: "+err.Error())
	}
	runLauncherTray(l)
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
	cmd := exec.Command(exe, "-addr", l.addr)
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

func serverExeName() string {
	if runtime.GOOS == "windows" {
		return "LANcast-Server.exe"
	}
	return "LANcast-Server"
}
