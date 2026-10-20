package main

import (
	"strings"
	"testing"

	"lancast/internal/clientwindow"
)

// fakeWindow records what the tray asked of the window. Embedding the
// interface keeps it to the four calls desk makes; anything else panics,
// which is the point — desk must not reach past them.
type fakeWindow struct {
	clientwindow.Controller
	shown, hidden, closed int
	navigated             []string
	evals                 []string
}

func (f *fakeWindow) Show()               { f.shown++ }
func (f *fakeWindow) Hide()               { f.hidden++ }
func (f *fakeWindow) Close()              { f.closed++ }
func (f *fakeWindow) Navigate(url string) { f.navigated = append(f.navigated, url) }
func (f *fakeWindow) Eval(js string)      { f.evals = append(f.evals, js) }

func requested(d *desk) bool {
	select {
	case <-d.openReq:
		return true
	default:
		return false
	}
}

// At login there is a tray and no window. Opening from the tray must wake
// the launch waiting to create one, and a destination asked for meanwhile
// must not be lost.
func TestOpeningBeforeTheWindowExistsCreatesIt(t *testing.T) {
	d := newDesk("https://127.0.0.1:8080", false)
	d.open("https://127.0.0.1:8080/settings?pane=updates")
	if !requested(d) {
		t.Fatal("opening from the tray did not ask for a window")
	}
	w := &fakeWindow{}
	d.attach(w)
	if len(w.navigated) != 1 || w.navigated[0] != "https://127.0.0.1:8080/settings?pane=updates" {
		t.Fatalf("navigated %v; the destination asked for before the window existed was lost", w.navigated)
	}
}

// Pressing Open twice while the window is being made asks for one window.
func TestOneWindowIsAskedForAtATime(t *testing.T) {
	d := newDesk("x", false)
	d.open("")
	d.open("")
	if !requested(d) || requested(d) {
		t.Fatal("two opens before the window existed queued two windows")
	}
}

func TestOpeningAnExistingWindowShowsIt(t *testing.T) {
	d := newDesk("x", false)
	w := &fakeWindow{}
	d.attach(w)
	d.open("")
	d.open("x/settings?pane=libraries&tray=scan")
	if w.shown != 2 || len(w.evals) != 1 {
		t.Fatalf("shown %d, evals %v", w.shown, w.evals)
	}
	if requested(d) {
		t.Fatal("an existing window asked for another")
	}
}

/*
 * An open window routes; it is not reloaded.
 *
 * Reported: "Update libraries" from the tray reloaded the open window. A
 * Navigate is a page load, so the destination is handed to the page's router
 * instead, and loaded only if the page has no router to take it.
 */
func TestAnOpenWindowRoutesRatherThanReloads(t *testing.T) {
	d := newDesk("https://127.0.0.1:8080", false)
	w := &fakeWindow{}
	d.attach(w)
	d.open("https://127.0.0.1:8080/settings?pane=libraries&tray=scan")
	if len(w.navigated) != 0 {
		t.Fatalf("the open window was reloaded: %v", w.navigated)
	}
	if len(w.evals) != 1 {
		t.Fatalf("evals %v", w.evals)
	}
	js := w.evals[0]
	for _, want := range []string{
		`window.__lancastNavigate(p)`,
		`"/settings?pane=libraries\u0026tray=scan"`,
		`location.assign(u)`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("script lacks %s:\n%s", want, js)
		}
	}
}

// Somewhere that is not this server is not a route, and is loaded.
func TestAForeignAddressIsStillLoaded(t *testing.T) {
	w := &fakeWindow{}
	goInPage(w, "https://127.0.0.1:8080", "https://example.test/x")
	if len(w.navigated) != 1 || len(w.evals) != 0 {
		t.Fatalf("navigated %v, evals %v", w.navigated, w.evals)
	}
	// A base that merely prefixes another host is not this server either.
	if _, ok := inPageScript("https://127.0.0.1:8080", "https://127.0.0.1:80801/x"); ok {
		t.Fatal("a different port was treated as this server")
	}
}

// ✕ hides into the tray; Exit closes. The tray is in this process, so on
// Windows there is always something to bring the window back.
func TestCloseHidesAndExitCloses(t *testing.T) {
	d := newDesk("x", false)
	w := &fakeWindow{}
	d.attach(w)

	destroy := d.closing()
	if hasTray && (destroy || w.hidden != 1) {
		t.Fatalf("✕ with a tray: destroy=%v hidden=%d; want hidden", destroy, w.hidden)
	}
	// And the page is told, so a film does not play on in a hidden window.
	if hasTray && (len(w.evals) != 1 || w.evals[0] != `window.__lancastTray && window.__lancastTray("window-hidden")`) {
		t.Fatalf("✕ did not tell the page the window was hidden: %v", w.evals)
	}
	if !hasTray && !destroy {
		t.Fatal("✕ with no tray must close: a hidden window with nothing to restore it is a process with no way in")
	}

	d.exit()
	if w.closed != 1 {
		t.Fatalf("Exit closed the window %d times", w.closed)
	}
	if !d.closing() {
		t.Fatal("after Exit, the close it causes was intercepted and hidden")
	}
	select {
	case <-d.exitReq:
	default:
		t.Fatal("Exit did not release the launch waiting on it")
	}
	d.exit() // a second Exit must not panic on a closed channel
}

// Exit with no window yet — chosen from the tray at login, before anything
// was opened — ends the launch without creating a window.
func TestExitBeforeAnyWindow(t *testing.T) {
	d := newDesk("x", false)
	d.exit()
	select {
	case <-d.exitReq:
	default:
		t.Fatal("Exit at login did not end the launch")
	}
	if requested(d) {
		t.Fatal("Exit asked for a window")
	}
}

// The page reports scanning, and the tray hears it: Cancel is enabled
// exactly while the page says a scan is running.
func TestThePageTellsTheTrayAboutScanning(t *testing.T) {
	d := newDesk("x", false)
	var got []bool
	d.onScanning = func(on bool) { got = append(got, on) }
	report := trayBindings(d)["lancastTrayScanning"].(func(bool))
	report(true)
	report(false)
	if len(got) != 2 || !got[0] || got[1] {
		t.Fatalf("tray heard %v, want [true false]", got)
	}
}

// Cancel asks the page, and does not bring the window forward to do it.
func TestCancelAsksThePageWithoutShowingTheWindow(t *testing.T) {
	d := newDesk("x", false)
	d.cancelScan() // no window yet: nothing to ask, and nothing must break
	w := &fakeWindow{}
	d.attach(w)
	d.cancelScan()
	if len(w.evals) != 1 || w.evals[0] != `window.__lancastTray && window.__lancastTray("cancel-scan")` {
		t.Fatalf("evals %q", w.evals)
	}
	if w.shown != 0 || len(w.navigated) != 0 {
		t.Fatalf("cancel moved the window: shown %d, navigated %v", w.shown, w.navigated)
	}
}
