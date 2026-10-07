package clientwindow

import "testing"

// Three screens left to right, the middle one primary, as the desk this was
// reported on: a monitor either side and a television.
var desk = []Monitor{
	{Device: `\\.\DISPLAY2`, Work: Rect{Left: 1920, Top: 0, Right: 3840, Bottom: 1040}},
	{Device: `\\.\DISPLAY1`, Work: Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1040}, Primary: true},
	{Device: `\\.\DISPLAY3`, Work: Rect{Left: -1920, Top: 0, Right: 0, Bottom: 1040}},
}

func TestWinUpMaximizesAndWinDownUndoesIt(t *testing.T) {
	win := Rect{Left: 100, Top: 100, Right: 900, Bottom: 700}
	if got := PlanWinKey(WinUp, false, WinState{Window: win, Monitors: desk}); got.Kind != MoveMaximize {
		t.Errorf("Win+Up on a window = %+v, want maximize", got)
	}
	max := WinState{Window: desk[1].Work, Normal: win, Maximized: true, Monitors: desk}
	if got := PlanWinKey(WinDown, false, max); got.Kind != MoveRestore {
		t.Errorf("Win+Down on a maximized window = %+v, want restore", got)
	}
	if got := PlanWinKey(WinDown, false, WinState{Window: win, Monitors: desk}); got.Kind != MoveMinimize {
		t.Errorf("Win+Down on a restored window = %+v, want minimize", got)
	}
}

func TestWinLeftSnapsToHalfThenOnToTheNextScreen(t *testing.T) {
	win := Rect{Left: 100, Top: 100, Right: 900, Bottom: 700}
	got := PlanWinKey(WinLeft, false, WinState{Window: win, Monitors: desk})
	want := Rect{Left: 0, Top: 0, Right: 960, Bottom: 1040}
	if got.Kind != MovePlace || got.Rect != want {
		t.Fatalf("Win+Left = %+v, want the left half %+v", got, want)
	}
	// Pressed again from the left half: the right half of the screen to the left.
	again := PlanWinKey(WinLeft, false, WinState{Window: want, Monitors: desk})
	if again.Kind != MovePlace || again.Rect != (Rect{Left: -960, Top: 0, Right: 0, Bottom: 1040}) {
		t.Errorf("Win+Left from the left half = %+v, want the right half of the left screen", again)
	}
	// At the left edge of the desk it stops rather than wrapping, as Windows does.
	edge := Rect{Left: -1920, Top: 0, Right: -960, Bottom: 1040}
	if got := PlanWinKey(WinLeft, false, WinState{Window: edge, Monitors: desk}); got.Kind != MoveNone {
		t.Errorf("Win+Left at the edge of the desk = %+v, want nothing", got)
	}
}

func TestWinShiftArrowCarriesTheWindowToTheNextScreen(t *testing.T) {
	win := Rect{Left: 100, Top: 100, Right: 900, Bottom: 700}
	got := PlanWinKey(WinRight, true, WinState{Window: win, Monitors: desk})
	want := Rect{Left: 2020, Top: 100, Right: 2820, Bottom: 700}
	if got.Kind != MovePlace || got.Rect != want || got.Maximize {
		t.Errorf("Win+Shift+Right = %+v, want %+v unmaximized", got, want)
	}
	// From the rightmost screen it wraps to the leftmost.
	far := Rect{Left: 2020, Top: 100, Right: 2820, Bottom: 700}
	wrap := PlanWinKey(WinRight, true, WinState{Window: far, Monitors: desk})
	if wrap.Monitor.Device != `\\.\DISPLAY3` {
		t.Errorf("Win+Shift+Right from the last screen landed on %q, want the first", wrap.Monitor.Device)
	}
}

// A maximized window is carried by its restored rectangle and maximized again
// on arrival, or it would land the size of the screen it left.
func TestAMaximizedWindowArrivesMaximized(t *testing.T) {
	normal := Rect{Left: 100, Top: 100, Right: 900, Bottom: 700}
	got := PlanWinKey(WinRight, true, WinState{
		Window: desk[1].Work, Normal: normal, Maximized: true, Monitors: desk,
	})
	if got.Kind != MovePlace || !got.Maximize || got.Rect != (Rect{Left: 2020, Top: 100, Right: 2820, Bottom: 700}) {
		t.Errorf("maximized Win+Shift+Right = %+v", got)
	}
}

// A window larger than the screen it is carried to is shrunk onto it.
func TestACarriedWindowFitsItsNewScreen(t *testing.T) {
	small := []Monitor{
		{Work: Rect{Left: 0, Top: 0, Right: 3840, Bottom: 2080}, Primary: true},
		{Work: Rect{Left: 3840, Top: 0, Right: 5120, Bottom: 760}},
	}
	win := Rect{Left: 2000, Top: 900, Right: 3800, Bottom: 2000}
	got := PlanWinKey(WinRight, true, WinState{Window: win, Monitors: small})
	if got.Rect.Width() > 1280 || got.Rect.Height() > 760 ||
		got.Rect.Left < 3840 || got.Rect.Right > 5120 || got.Rect.Bottom > 760 {
		t.Errorf("carried window %+v is not inside its new screen", got.Rect)
	}
}

// Fullscreen moves between screens and ignores everything else.
func TestFullscreenOnlyMovesBetweenScreens(t *testing.T) {
	fs := WinState{Window: Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}, Fullscreen: true, Monitors: desk}
	got := PlanWinKey(WinLeft, true, fs)
	if got.Kind != MoveFullscreenTo || got.Monitor.Device != `\\.\DISPLAY3` {
		t.Errorf("Win+Shift+Left in fullscreen = %+v, want fullscreen on the left screen", got)
	}
	for _, k := range []WinKey{WinUp, WinDown, WinLeft, WinRight} {
		if got := PlanWinKey(k, false, fs); got.Kind != MoveNone {
			t.Errorf("key %d in fullscreen = %+v, want nothing", k, got)
		}
	}
}

func TestOneScreenHasNowhereToGo(t *testing.T) {
	one := []Monitor{{Work: Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1040}, Primary: true}}
	win := Rect{Left: 100, Top: 100, Right: 900, Bottom: 700}
	if got := PlanWinKey(WinRight, true, WinState{Window: win, Monitors: one}); got.Kind != MoveNone {
		t.Errorf("Win+Shift+Right with one screen = %+v, want nothing", got)
	}
}

func TestMediaKeysAreNamedAndNothingElseIs(t *testing.T) {
	for vk, want := range map[uint32]string{0xB3: "playpause", 0xB0: "next", 0xB1: "previous", 0xB2: "stop", 0x20: "", 0xAF: ""} {
		if got := MediaCommand(vk); got != want {
			t.Errorf("MediaCommand(%#x) = %q, want %q", vk, got, want)
		}
	}
}
