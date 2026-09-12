package games

import (
	"strings"
	"testing"
)

// The monitors in these tests are the real shape of a three-screen desk,
// including the one to the left of the primary that starts at a negative x.
// A fixture with three tidy rectangles all beginning at zero would pass while
// the arithmetic that matters was wrong.
var (
	primary = Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1032}     // work area, taskbar excluded
	right   = Rect{Left: 1920, Top: 0, Right: 3360, Bottom: 960}   //
	left    = Rect{Left: -2560, Top: -165, Right: 0, Bottom: 1275} // starts negative
)

func TestMoveTargetCentresOnTheChosenScreen(t *testing.T) {
	win := Rect{Left: 100, Top: 100, Right: 1380, Bottom: 900} // 1280 x 800
	x, y := MoveTarget(win, right)
	if want := 1920 + (1440-1280)/2; x != want {
		t.Errorf("x = %d, want %d", x, want)
	}
	if want := 0 + (960-800)/2; y != want {
		t.Errorf("y = %d, want %d", y, want)
	}
}

func TestMoveTargetHandlesAScreenLeftOfThePrimary(t *testing.T) {
	// The case that catches "the desktop starts at zero": this monitor's
	// origin is negative on both axes, and a window centred as though it were
	// not lands on a different screen entirely.
	win := Rect{Left: 0, Top: 0, Right: 1280, Bottom: 800}
	x, y := MoveTarget(win, left)
	if want := -2560 + (2560-1280)/2; x != want {
		t.Errorf("x = %d, want %d", x, want)
	}
	if want := -165 + (1440-800)/2; y != want {
		t.Errorf("y = %d, want %d", y, want)
	}
	if x >= 0 {
		t.Errorf("x = %d, which is not on that screen at all", x)
	}
}

func TestMoveTargetPinsAWindowTooBigForTheScreen(t *testing.T) {
	// Its own top-left is where every menu it has starts, so that is the corner
	// worth keeping reachable.
	win := Rect{Left: 0, Top: 0, Right: 3840, Bottom: 2160}
	x, y := MoveTarget(win, right)
	if x != right.Left || y != right.Top {
		t.Errorf("got (%d,%d), want the top-left corner (%d,%d)", x, y, right.Left, right.Top)
	}
}

func TestMoveTargetNeverChangesTheSize(t *testing.T) {
	// Belt and braces on the signature: a size returned here would eventually
	// be a size applied, and resizing a game's window behind its back is not
	// what "open it over there" asked for.
	win := Rect{Left: 0, Top: 0, Right: 1280, Bottom: 800}
	x, y := MoveTarget(win, primary)
	moved := Rect{Left: x, Top: y, Right: x + win.Width(), Bottom: y + win.Height()}
	if moved.Width() != win.Width() || moved.Height() != win.Height() {
		t.Errorf("size changed from %dx%d to %dx%d",
			win.Width(), win.Height(), moved.Width(), moved.Height())
	}
}

/*
 * A backslash canary.
 *
 * This exists because of a real bug rather than a hypothetical one: the prefix
 * was written into the function and into this file's fixtures at the same time
 * with one backslash missing, so every test agreed with the mistake and passed
 * while the picker offered "\\.\DISPLAY1" as the name of a screen. A fixture
 * cannot catch an error it shares.
 *
 * A count can. Three backslashes is a number, and a number cannot be quietly
 * mangled by whatever mangled the string.
 */
func TestTheDevicePrefixHasAllItsBackslashes(t *testing.T) {
	if got := strings.Count(devicePrefix, `\`); got != 3 {
		t.Fatalf("devicePrefix is %q with %d backslashes, want 3 — a real device is \\\\.\\DISPLAY1", devicePrefix, got)
	}
	if !strings.HasSuffix(devicePrefix, "DISPLAY") {
		t.Fatalf("devicePrefix = %q", devicePrefix)
	}
}

func TestDisplayLabel(t *testing.T) {
	for _, tc := range []struct {
		device  string
		w, h    int
		primary bool
		want    string
	}{
		{`\\.\DISPLAY1`, 1920, 1080, true, "Display 1 — 1920 x 1080 (main)"},
		{`\\.\DISPLAY2`, 1440, 960, false, "Display 2 — 1440 x 960"},
		{`\\.\DISPLAY3`, 2560, 1440, false, "Display 3 — 2560 x 1440"},
		// Anything that is not shaped like a device name is shown as it is,
		// rather than being mangled into "Display ".
		{"HDMI-1", 1280, 720, false, "HDMI-1 — 1280 x 720"},
	} {
		if got := DisplayLabel(tc.device, tc.w, tc.h, tc.primary); got != tc.want {
			t.Errorf("DisplayLabel(%q) = %q, want %q", tc.device, got, tc.want)
		}
	}
}

func TestLooksLikeGameWindow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		title string
		w, h  int
		want  bool
	}{
		{"a game", "Palworld", 1280, 800, true},
		{"an untitled window", "", 1280, 800, false},
		{"whitespace for a title", "   ", 1280, 800, false},
		{"a tooltip", "tip", 90, 40, false},
		{"a splash", "Loading", 300, 200, false},
		{"an off-screen message sink", "", 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := LooksLikeGameWindow(tc.title, tc.w, tc.h); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDisplayAnswerIsRememberedPerGame(t *testing.T) {
	var p Prefs
	if p.DisplayFor("700010") != "" {
		t.Error("a game nobody has been asked about should have no answer")
	}

	p.SetDisplay("700010", `\\.\DISPLAY2`)
	p.SetDisplay("700011", DisplayDefault)

	if got := p.DisplayFor("700010"); got != `\\.\DISPLAY2` {
		t.Errorf("answer = %q", got)
	}
	// The distinction the whole constant exists for: "leave this one alone" is
	// an answer, and must not raise the picker again.
	if got := p.DisplayFor("700011"); got != DisplayDefault {
		t.Errorf("default answer = %q, want it recorded", got)
	}
}

func TestForgettingADisplayAsksAgain(t *testing.T) {
	var p Prefs
	p.SetDisplay("700010", `\\.\DISPLAY2`)
	p.SetDisplay("700010", "")
	if p.DisplayFor("700010") != "" {
		t.Error("forgetting should leave nothing behind")
	}
	if p.Displays != nil {
		t.Errorf("Displays = %v, want nil so the file stays short", p.Displays)
	}
}

func TestDisplayAnswersSurviveTheFile(t *testing.T) {
	dir := t.TempDir()
	var p Prefs
	p.Set("700010", false, true)
	p.SetDisplay("700010", `\\.\DISPLAY3`)
	if err := SavePrefs(dir, p); err != nil {
		t.Fatal(err)
	}
	back, err := LoadPrefs(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The backslashes are the part worth checking: a device name is the one
	// value in this file that JSON has to escape.
	if got := back.DisplayFor("700010"); got != `\\.\DISPLAY3` {
		t.Errorf("after a round trip = %q", got)
	}
	if !back.IsFavourite("700010") {
		t.Error("the other flags should be unharmed")
	}
}
