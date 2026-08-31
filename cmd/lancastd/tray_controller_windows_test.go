//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/*
 * The tray on a machine with the service installed.
 *
 * It used to start the service, open a browser and exit — so on every machine
 * with a normal install the executable flashed a browser and vanished, and
 * there was nothing to click a second time. Reported as the server exe being
 * more or less useless.
 *
 * The reasoning for that was sound about a process that *is* a server: "an icon
 * that outlived the launch would be a second thing claiming to be the server".
 * It is wrong about this one, which holds no port, no database and no lock. The
 * distinction has to survive in the words on the menu, which is what these
 * assert — a source read rather than a running tray, because systray owns the
 * message loop and there is no way to drive one from a test.
 */

func traySource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("tray_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

/*
 * The load-bearing one. Stopping a Windows service needs elevation, so an Exit
 * that tried would either fail silently or raise a UAC prompt for something
 * labelled "Exit" — the confusion the old comment was guarding against,
 * arriving from the other side.
 */
func TestExitSaysTheServerKeepsRunning(t *testing.T) {
	src := traySource(t)
	i := strings.Index(src, "func runServiceTray")
	if i < 0 {
		t.Fatal("the controller tray is gone")
	}
	body := src[i:]

	if !strings.Contains(body, `systray.AddMenuItem("Exit"`) {
		t.Error("no Exit item")
	}
	if !strings.Contains(body, "The LANcast server keeps running.") {
		t.Error("Exit does not say the server survives it, which is the whole distinction")
	}
	// And it must not reach for the service control manager.
	for _, forbidden := range []string{"stopInstalledService", "svc.Control", "StopService"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the controller tray tries to stop the service (%s)", forbidden)
		}
	}
}

/*
 * Two items open a page rather than doing the thing, and both must say so.
 *
 * A scan is an admin action on an authenticated API and this process holds no
 * session. A menu item that quietly failed, or one that invented a local back
 * door into an authenticated endpoint, would be worse than one that takes you
 * to where the button is — but only if it does not pretend.
 */
func TestThePageOpenersAdmitTheyOpenPages(t *testing.T) {
	src := traySource(t)
	for _, label := range []string{`"Update libraries…"`, `"Check for updates…"`} {
		if !strings.Contains(src, label) {
			t.Errorf("%s is missing or does not end in an ellipsis", label)
		}
	}
}

func TestTheTrayOffersTheAppAndTheBrowser(t *testing.T) {
	src := traySource(t)
	i := strings.Index(src, "func runServiceTray")
	body := src[i:]
	for _, want := range []string{`"Open LANcast"`, `"Open the LANcast app"`, `"Start LANcast at login"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the controller tray has no %s", want)
		}
	}
}

/*
 * The client is looked for beside this executable rather than on PATH, which
 * would find whatever else is called LANcast-Client.
 */
func TestTheAppIsFoundBesideTheServer(t *testing.T) {
	dir := t.TempDir()
	// Nothing there: the error has to name the file rather than being an
	// unexplained failure to launch.
	if _, err := os.Stat(filepath.Join(dir, "LANcast-Client.exe")); err == nil {
		t.Skip("unexpected client in a fresh temp dir")
	}
	src := traySource(t)
	if !strings.Contains(src, `filepath.Join(filepath.Dir(exe), "LANcast-Client.exe")`) {
		t.Error("the app is not resolved beside the server executable")
	}
}

/*
 * Exit closes the app as well as the icon.
 *
 * It used to remove only the icon, leaving this process — which runs out of the
 * install directory — resident. Reported as closing LANcast from the tray and
 * finding the processes still running, which then fouled the next update: a
 * held image in Program Files is exactly what an update has to move aside.
 *
 * The complaint underneath was a fair reading of the menu. "Quit the LANcast
 * app" and "Exit" were two partial endings, and neither was the one somebody
 * means by closing LANcast.
 */
func TestExitAlsoClosesTheApp(t *testing.T) {
	src := traySource(t)
	i := strings.Index(src, "func runServiceTray")
	if i < 0 {
		t.Fatal("the controller tray is gone")
	}
	body := src[i:]

	j := strings.Index(body, "watch(mQuit.ClickedCh,")
	if j < 0 {
		t.Fatal("no Exit handler")
	}
	handler := body[j:]
	k := strings.Index(handler, "systray.Quit()")
	if k < 0 {
		t.Fatal("Exit no longer removes the icon")
	}

	/*
	 * Before systray.Quit, not after. Once that has run this process is on its
	 * way out, and a Quit sent from a dying process is a race nobody needs to
	 * debug.
	 */
	q := strings.Index(handler, "raise.Quit()")
	if q < 0 {
		t.Error("Exit removes the icon without closing the app, which leaves " +
			"this process holding its image in the install directory")
	} else if q > k {
		t.Error("Exit asks the app to quit only after starting its own " +
			"shutdown, which is a race rather than an ordering")
	}
}

/*
 * The tick shows what the setting *is*, not what was attempted.
 *
 * It used to be set from the outcome of the call: check after Enable returned
 * no error, uncheck after Disable did. That reads as reasonable and it made the
 * menu capable of lying — watched doing exactly that, with a tick appearing
 * beside a run key that had never been written, while the only other report of
 * the failure went to a logger with nowhere to write.
 *
 * Reading the state back afterwards is what makes the menu unable to claim
 * something the machine does not agree with.
 */
func TestTheLoginTickIsReadBackFromTheRegistry(t *testing.T) {
	body := loginBody(t)

	/*
	 * The tick must come from a fresh read *after* the change.
	 *
	 * There are two reads now — one to decide what to do, one to see what
	 * actually happened — so the read-back is the last of them. Using the first
	 * would pass while the tick described the state before the change, which is
	 * the bug this is here to prevent.
	 */
	read := strings.LastIndex(body, "autostart.Enabled(")
	if read < 0 {
		t.Fatal("the tick is set without reading the setting back, so it can " +
			"show a state the registry does not have")
	}
	for _, call := range []string{"autostart.Enable(autostart", "autostart.Disable("} {
		at := strings.Index(body, call)
		if at < 0 {
			t.Errorf("toggleLogin no longer calls %s", call)
			continue
		}
		if at > read {
			t.Errorf("%s happens after the read-back, so the tick describes "+
				"the state before the change", call)
		}
	}
	for _, must := range []string{"item.Check()", "item.Uncheck()"} {
		if !strings.Contains(body, must) {
			t.Errorf("toggleLogin no longer calls %s", must)
		}
	}
}

/*
 * The tray writes its warnings somewhere they can be read.
 *
 * newLogger writes to stderr and this is a GUI process with no console, so
 * every warning it produced went into nothing — which is how a toggle that
 * silently failed could be watched failing repeatedly with no way to see why.
 *
 * Its own file, not the server's: both rotate by renaming at a size threshold,
 * and two processes doing that to one file ends with the server's log
 * truncated.
 */
func TestTheTrayLogsToAFileOfItsOwn(t *testing.T) {
	src := traySource(t)
	i := strings.Index(src, "func runServiceTray")
	if i < 0 {
		t.Fatal("the controller tray is gone")
	}
	body := src[i:]

	if !strings.Contains(body, "applog.OpenNamed(") {
		t.Error("the tray does not open a log, so its warnings go nowhere")
	}
	if !strings.Contains(body, "applog.TrayFileName") {
		t.Error("the tray does not use its own log name")
	}
	if strings.Contains(body, "applog.Open(") && !strings.Contains(body, "applog.OpenNamed(") {
		t.Error("the tray writes the server's log, which races its rotation")
	}
}

/*
 * codeOnly strips comments before a source assertion looks at the text.
 *
 * These tests read the source because systray owns the message loop and nothing
 * can drive one from a test. That works until a comment *quotes* the pattern it
 * is warning about — which is exactly what happened here: the note explaining
 * why the intent must not come from the tick contains the very expression the
 * test forbids, and a substring search cannot tell an explanation from an
 * instruction.
 */
func codeOnly(src string) string {
	var b strings.Builder
	for len(src) > 0 {
		switch {
		case strings.HasPrefix(src, "/*"):
			if e := strings.Index(src, "*/"); e >= 0 {
				src = src[e+2:]
				continue
			}
			src = ""
		case strings.HasPrefix(src, "//"):
			if e := strings.IndexByte(src, '\n'); e >= 0 {
				src = src[e:]
				continue
			}
			src = ""
		default:
			b.WriteByte(src[0])
			src = src[1:]
		}
	}
	return b.String()
}

// loginBody is toggleLogin's code, comments removed.
func loginBody(t *testing.T) string {
	t.Helper()
	src := traySource(t)
	i := strings.Index(src, "func toggleLogin")
	if i < 0 {
		t.Fatal("toggleLogin is gone")
	}
	body := codeOnly(src[i:])
	if j := strings.Index(body[1:], "\nfunc "); j >= 0 {
		body = body[:j+1]
	}
	return body
}

/*
 * What to toggle *to* is read from the registry, never from the tick.
 *
 * `!item.Checked()` is the obvious way to write this and it is wrong here.
 * Windows toggles a checkbox menu item's visible tick itself when it is
 * clicked; systray's Checked() returns its own idea, changed only by Check and
 * Uncheck. The two drift the moment the native toggle happens, so an intent
 * computed from the widget is the opposite of what the person meant.
 *
 * Watched doing exactly that: the tick moved on every click, the run key never
 * changed, and nothing was logged — because each click computed "turn it off"
 * against a setting that was already off, succeeded, and agreed with itself.
 */
func TestTheLoginIntentDoesNotComeFromTheTick(t *testing.T) {
	body := loginBody(t)

	if strings.Contains(body, "item.Checked()") {
		t.Error("toggleLogin consults the widget for the setting; Windows " +
			"toggles that tick itself, so the intent comes out backwards")
	}
	read := strings.Index(body, "autostart.Enabled(")
	if read < 0 {
		t.Fatal("toggleLogin never reads the current setting")
	}
	if act := strings.Index(body, "autostart.Enable(autostart"); act >= 0 && act < read {
		t.Error("toggleLogin changes the setting before reading what it is")
	}
	if !strings.Contains(body, "want := !was") {
		t.Error("the wanted state is not the negation of the stored one")
	}
}

/*
 * Every menu item gets its own receiver.
 *
 * systray delivers a click with a non-blocking send on an *unbuffered* channel:
 *
 *	select {
 *	case item.ClickedCh <- struct{}{}:
 *	default:
 *	}
 *
 * So a click lands only if something is blocked on that exact channel at that
 * instant. There is no queue and no error — an undelivered click never happened.
 *
 * One select over every item made that a shared fate: while the single
 * goroutine sat inside any handler, every other item's clicks were discarded.
 * That is how "Start LANcast at login" moved its tick — which Windows draws
 * itself — while its handler never ran and nothing was logged, through three
 * releases of chasing it.
 */
func TestEveryMenuItemHasItsOwnReceiver(t *testing.T) {
	src := traySource(t)
	i := strings.Index(src, "func runServiceTray")
	if i < 0 {
		t.Fatal("the controller tray is gone")
	}
	body := codeOnly(src[i:])

	/*
	 * A select over more than one ClickedCh is the shape being forbidden. One
	 * is fine — that is a dedicated receiver — so this counts.
	 */
	if at := strings.Index(body, "select {"); at >= 0 {
		window := body[at:]
		if e := strings.Index(window, "\n\t\t}"); e > 0 {
			window = window[:e]
		}
		if strings.Count(window, "ClickedCh") > 1 {
			t.Error("the tray multiplexes several ClickedCh in one select; " +
				"systray drops a click when nothing is waiting, so any slow " +
				"handler silently eats every other item's clicks")
		}
	}

	// Each item must be watched, and every watch is its own goroutine.
	for _, item := range []string{
		"mOpen.ClickedCh", "mQuitApp.ClickedCh", "mApp.ClickedCh",
		"mLogin.ClickedCh", "mLibraries.ClickedCh", "mUpdates.ClickedCh",
		"mQuit.ClickedCh",
	} {
		if !strings.Contains(body, item) {
			t.Errorf("%s is no longer handled at all", item)
		}
	}
	if !strings.Contains(body, "for range ch {") {
		t.Error("the watcher does not loop on its own channel, so an item " +
			"stops responding after its first click")
	}
}
