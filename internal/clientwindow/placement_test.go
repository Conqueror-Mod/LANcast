package clientwindow

import "testing"

/*
 * Where the window opens when the desk has changed since last time.
 *
 * Every case here is a desk that moved: a monitor unplugged, two swapped, a
 * resolution lowered. The window being remembered is easy; the window being
 * remembered onto a screen that is no longer there is the whole problem, and
 * getting it wrong opens it where nobody can see it — worse than not
 * remembering, because the default is at least visible.
 */

func laptop() Monitor {
	return Monitor{Device: `\.\DISPLAY1`, Primary: true,
		Work: Rect{0, 0, 1920, 1040}}
}

// A second screen to the right, taller.
func right() Monitor {
	return Monitor{Device: `\.\DISPLAY2`,
		Work: Rect{1920, 0, 4480, 1400}}
}

// A screen to the *left* of the primary, so its coordinates are negative —
// the case that catches anything assuming the desktop starts at zero.
func left() Monitor {
	return Monitor{Device: `\.\DISPLAY3`,
		Work: Rect{-1920, 0, 0, 1040}}
}

func TestTheWindowGoesBackToTheSameScreen(t *testing.T) {
	p := Placement{Monitor: `\.\DISPLAY2`, X: 100, Y: 50, Width: 1280, Height: 720}
	got, ok := Resolve(p, []Monitor{laptop(), right()})
	if !ok {
		t.Fatal("ok = false, want a position")
	}
	if got.Left != 2020 || got.Top != 50 {
		t.Errorf("got %+v, want left 2020 top 50 — 100,50 into DISPLAY2's work area", got)
	}
	if got.Width() != 1280 || got.Height() != 720 {
		t.Errorf("size = %dx%d, want 1280x720", got.Width(), got.Height())
	}
}

// The point of storing a *relative* position: the monitor moved, the window's
// place on it did not.
func TestARearrangedDeskKeepsTheWindowWhereItWasOnItsScreen(t *testing.T) {
	p := Placement{Monitor: `\.\DISPLAY2`, X: 100, Y: 50, Width: 1280, Height: 720}

	// The same monitor, now to the left of the primary instead of the right.
	moved := Monitor{Device: `\.\DISPLAY2`, Work: Rect{-2560, 0, 0, 1400}}
	got, ok := Resolve(p, []Monitor{laptop(), moved})
	if !ok {
		t.Fatal("ok = false")
	}
	if got.Left != -2460 || got.Top != 50 {
		t.Errorf("got %+v, want left -2460 — the same spot on the same screen", got)
	}
}

func TestAnUnpluggedMonitorSendsTheWindowToThePrimary(t *testing.T) {
	p := Placement{Monitor: `\.\DISPLAY2`, X: 100, Y: 50, Width: 1280, Height: 720}
	got, ok := Resolve(p, []Monitor{laptop()})
	if !ok {
		t.Fatal("ok = false, want the primary")
	}
	if got.Left < 0 || got.Right > 1920 {
		t.Errorf("got %+v, want it inside the primary's work area", got)
	}
}

// Not "whatever now occupies those coordinates". A dock unplugged should put
// the window somewhere obvious, not somewhere arithmetically similar.
func TestAMissingMonitorIsNotReplacedByGeometry(t *testing.T) {
	p := Placement{Monitor: `\.\DISPLAY2`, X: 100, Y: 50, Width: 1280, Height: 720}
	// DISPLAY4 now sits exactly where DISPLAY2 used to.
	impostor := Monitor{Device: `\.\DISPLAY4`, Work: Rect{1920, 0, 4480, 1400}}
	got, _ := Resolve(p, []Monitor{laptop(), impostor})
	if got.Left >= 1920 {
		t.Errorf("got %+v — the window went to a different monitor that happened "+
			"to be at the old coordinates", got)
	}
}

func TestAWindowLargerThanItsNewScreenIsShrunkToFit(t *testing.T) {
	// Remembered from the 2560x1400 screen, restored onto the 1920x1040 one.
	p := Placement{Monitor: `\.\DISPLAY1`, X: 0, Y: 0, Width: 2560, Height: 1400}
	got, ok := Resolve(p, []Monitor{laptop()})
	if !ok {
		t.Fatal("ok = false")
	}
	if got.Width() > 1920 || got.Height() > 1040 {
		t.Errorf("got %dx%d, want it to fit inside 1920x1040", got.Width(), got.Height())
	}
}

func TestAWindowRememberedPastTheEdgeIsPulledBackOn(t *testing.T) {
	// The screen got smaller, or the position was near the right edge.
	p := Placement{Monitor: `\.\DISPLAY1`, X: 1800, Y: 900, Width: 1280, Height: 720}
	got, ok := Resolve(p, []Monitor{laptop()})
	if !ok {
		t.Fatal("ok = false")
	}
	if got.Right > 1920 || got.Bottom > 1040 || got.Left < 0 || got.Top < 0 {
		t.Errorf("got %+v, want the whole window inside 0,0-1920,1040", got)
	}
}

func TestNegativeCoordinatesAreHandled(t *testing.T) {
	p := Placement{Monitor: `\.\DISPLAY3`, X: 40, Y: 40, Width: 800, Height: 600}
	got, ok := Resolve(p, []Monitor{laptop(), left()})
	if !ok {
		t.Fatal("ok = false")
	}
	if got.Left != -1880 {
		t.Errorf("got %+v, want left -1880 — a monitor left of the primary", got)
	}
}

func TestNothingRememberedMeansLetTheSystemDecide(t *testing.T) {
	if _, ok := Resolve(Placement{}, []Monitor{laptop()}); ok {
		t.Error("ok = true for an empty placement; the default position is a good answer")
	}
	if _, ok := Resolve(Placement{Width: 800, Height: 600}, nil); ok {
		t.Error("ok = true with no monitors at all")
	}
}

// A placement written before monitors were named still has a usable size.
func TestAPlacementWithNoMonitorNameKeepsItsSize(t *testing.T) {
	got, ok := Resolve(Placement{Width: 1000, Height: 700}, []Monitor{laptop(), right()})
	if !ok {
		t.Fatal("ok = false, want the primary and the remembered size")
	}
	if got.Width() != 1000 || got.Height() != 700 {
		t.Errorf("size = %dx%d, want 1000x700", got.Width(), got.Height())
	}
	if got.Left >= 1920 {
		t.Errorf("got %+v, want the primary", got)
	}
}

func TestATinyRememberedSizeIsNotHonoured(t *testing.T) {
	p := Placement{Monitor: `\.\DISPLAY1`, X: 0, Y: 0, Width: 40, Height: 30}
	got, _ := Resolve(p, []Monitor{laptop()})
	if got.Width() < minSize || got.Height() < minSize {
		t.Errorf("got %dx%d, want at least %d — that size looks like a crash",
			got.Width(), got.Height(), minSize)
	}
}

/*
 * Capture: which screen a window belongs to when it is on two of them.
 */

func TestCaptureRecordsThePositionOnItsOwnScreen(t *testing.T) {
	win := Rect{2020, 50, 3300, 770} // on DISPLAY2
	got := Capture(win, []Monitor{laptop(), right()}, false)
	if got.Monitor != `\.\DISPLAY2` {
		t.Errorf("Monitor = %q, want DISPLAY2", got.Monitor)
	}
	if got.X != 100 || got.Y != 50 {
		t.Errorf("X,Y = %d,%d, want 100,50 relative to that screen", got.X, got.Y)
	}
	if got.Width != 1280 || got.Height != 720 {
		t.Errorf("size = %dx%d, want 1280x720", got.Width, got.Height)
	}
}

// A window straddling two screens belongs to the one showing most of it. Its
// top-left corner is frequently on the other.
func TestCaptureChoosesTheScreenShowingMostOfTheWindow(t *testing.T) {
	// Starts 200px inside the primary and extends well into DISPLAY2.
	win := Rect{1720, 100, 3000, 820}
	got := Capture(win, []Monitor{laptop(), right()}, false)
	if got.Monitor != `\.\DISPLAY2` {
		t.Errorf("Monitor = %q, want DISPLAY2 — 1080 of its 1280 columns are there",
			got.Monitor)
	}
}

func TestCaptureOfAWindowOnNoScreenKeepsOnlyTheSize(t *testing.T) {
	// The monitor it was on has gone; the window is nowhere.
	win := Rect{5000, 5000, 6280, 5720}
	got := Capture(win, []Monitor{laptop()}, false)
	if got.Monitor != "" {
		t.Errorf("Monitor = %q, want empty — it is on no screen", got.Monitor)
	}
	if got.Width != 1280 || got.Height != 720 {
		t.Errorf("size = %dx%d, want the size kept so the next launch can use it",
			got.Width, got.Height)
	}
}

func TestCaptureAndResolveRoundTrip(t *testing.T) {
	monitors := []Monitor{laptop(), right(), left()}
	for _, win := range []Rect{
		{100, 100, 1380, 820},   // primary
		{2020, 50, 3300, 770},   // right
		{-1880, 40, -1080, 640}, // left
	} {
		p := Capture(win, monitors, false)
		got, ok := Resolve(p, monitors)
		if !ok {
			t.Fatalf("%+v did not resolve", win)
		}
		if got != win {
			t.Errorf("round trip changed %+v into %+v", win, got)
		}
	}
}

/*
 * The close handler has to record the position *and* respect the answer.
 *
 * This exists because the first version never ran at all. It was installed
 * after the window had been constructed with the original callback — which
 * takes it by value — so the webview kept the original, the wrapper was dead
 * code, and the position was silently never saved. It shipped, and the fifteen
 * tests above all passed, because they cover the rules rather than the wiring
 * that reaches them.
 */

func TestClosingRecordsThePositionFirst(t *testing.T) {
	order := []string{}
	h := closeHandler(
		func() { order = append(order, "capture") },
		func() bool { order = append(order, "user"); return true },
	)
	if !h() {
		t.Error("the caller said close and the wrapper said otherwise")
	}
	if len(order) != 2 || order[0] != "capture" || order[1] != "user" {
		t.Errorf("order = %v, want capture then user — the position must be read "+
			"before anything decides to tear the window down", order)
	}
}

// Close-to-tray keeps the window alive by answering false. The position is
// still recorded: the window is at a real place at that moment, and the
// alternative is remembering wherever it was last genuinely closed.
func TestAHideStillRecordsThePosition(t *testing.T) {
	captured := false
	h := closeHandler(func() { captured = true }, func() bool { return false })
	if h() {
		t.Error("returned true; close-to-tray must be able to keep the window")
	}
	if !captured {
		t.Error("a hide recorded nothing — a window hidden for a week would " +
			"remember wherever it was last closed")
	}
}

func TestNoCallerHandlerStillCloses(t *testing.T) {
	captured := false
	h := closeHandler(func() { captured = true }, nil)
	if !h() {
		t.Error("returned false with no caller handler; the default is to close")
	}
	if !captured {
		t.Error("nothing was captured")
	}
}
