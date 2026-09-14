package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"lancast/internal/desktop"
	"lancast/internal/service"
)

/*
 * Waiting for a server this process must not try to start.
 *
 * The client's fallback — nothing is answering, so start a server — cannot work
 * on a machine where LANcast is installed as a service, and it fails in the
 * most expensive way available. The machine-wide data directory belongs to the
 * system account, so a server started as the signed-in user gets
 *
 *     apply schema: attempt to write a readonly database (8)
 *
 * and exits before it can write that line to the log. Measured on a real
 * install: `BUILTIN\Users` holds (RX) on lancast.db and nothing more, and the
 * spawned server was gone inside a second with lancastd.log showing no trace of
 * it having existed.
 *
 * What that looked like from the front is the bug this fixes: a login that
 * opened nothing. The client spawned a server that died instantly, waited the
 * full twenty seconds for it, and then put up a message box saying the server
 * had not come up. Every cold boot, because at login the service is still
 * starting and the client is racing it — and the client could never win that
 * race, because the thing it was racing with is the only process allowed to
 * write the database.
 *
 * So when a service is installed the client waits for *it* and never starts a
 * rival. Ninety seconds rather than twenty: the number has to cover a cold boot
 * with a delayed-auto-start service, on a disk doing everything else Windows
 * does at login.
 */

const (
	// serviceWait is how long to give an installed service to answer.
	serviceWait = 90 * time.Second
	// servicePoll is how often to ask. Short enough that a service arriving
	// early is not made to wait for the client.
	servicePoll = 500 * time.Millisecond
	/*
	 * stoppedGrace is how long a *stopped* service gets before it is reported
	 * as stopped.
	 *
	 * Waiting the full ninety seconds for one is worse than the bug this file
	 * fixes: a stopped service is not going to start itself, so every second
	 * after the first few is spent confirming something already known. The
	 * grace exists only because "stopped" is also what a service looks like in
	 * the instant before something else starts it — the tray, an administrator,
	 * or the machine still working through login.
	 */
	stoppedGrace = 5 * time.Second
)

/*
 * installedServiceState is the service's own state, and whether one is
 * installed at all.
 *
 * service.RunningServer cannot answer this: it reports a stopped service the
 * same way it reports no service, and those two need different words said to
 * the person looking at the window.
 */
func installedServiceState() (string, bool) {
	mgr, err := service.NewManager()
	if err != nil {
		return "", false
	}
	state, err := mgr.Status()
	if err != nil {
		// Not installed, or not ours to query. Either way there is no service
		// to wait for and the ordinary path applies.
		return "", false
	}
	return state, true
}

/*
 * usesSharedDataDir reports whether the server this client would start would be
 * writing the machine-wide directory.
 *
 * This is the condition, rather than "is a service installed at all". The
 * read-only failure is a property of *that directory* — the service owns it as
 * the system account — so it is the only case where starting a server is
 * futile. Somebody who points the client at their own data directory with
 * -data is doing something legitimate, and refusing to start a server for them
 * because a service happens to exist elsewhere on the machine would be a new
 * bug traded for the old one.
 */
func (l *launcher) usesSharedDataDir() bool {
	dir, ok := l.serverDataDir()
	if !ok {
		return false
	}
	shared := service.DefaultDataDir(runtime.GOOS)
	if shared == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(dir), filepath.Clean(shared))
}

// waitForInstalledService waits for the installed service to start answering,
// and explains itself in the terms of whatever it was doing when it gave up.
func waitForInstalledService(addr, state string) error {
	start := time.Now()
	deadline := start.Add(serviceWait)
	for time.Now().Before(deadline) {
		if desktop.ServerRunning(addr) {
			return nil
		}
		// A service that has been stopped since the client started is not
		// coming, and making somebody watch a frozen window for a minute and a
		// half to be told so is its own bug.
		if state == "stopped" && time.Since(start) > stoppedGrace {
			break
		}
		time.Sleep(servicePoll)
		// Re-read as we go: a service that starts and then fails should be
		// reported as stopped rather than as whatever it was a minute ago.
		if s, ok := installedServiceState(); ok {
			state = s
		}
	}
	if desktop.ServerRunning(addr) {
		return nil
	}
	return fmt.Errorf("%s", serviceWaitMessage(addr, state))
}

/*
 * serviceWaitMessage is what to say when the wait runs out.
 *
 * Pure, and separated from the waiting, because the words are the part worth
 * testing and the part that was wrong before: one sentence — "the server did
 * not come up" — was shown for a service that was still starting and for one
 * that was not running at all, which are different problems with different
 * answers, and neither of them is anything the person can do about a server
 * this client was never going to be able to start.
 */
func serviceWaitMessage(addr, state string) string {
	if state == "stopped" {
		return "The LANcast service is installed but is not running.\n\n" +
			"Start it from Windows Services, or restart the computer.\n\n" +
			"LANcast cannot start it for you: the service runs as the system " +
			"account, and starting one needs an administrator."
	}
	return fmt.Sprintf("The LANcast service is %s and has not answered at %s "+
		"after %s.\n\nIt is most likely still starting. Opening LANcast again "+
		"in a moment usually finds it.", state, desktop.UIURL(addr), serviceWait)
}
