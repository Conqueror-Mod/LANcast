//go:build windows

package webview2

import (
	"testing"

	"lancast/internal/webview2/w32"
)

// The TV, as the primary display at 3840x2160, and a desk monitor to its left.
var tv = w32.Rect{Left: 0, Top: 0, Right: 3840, Bottom: 2160}

func TestPointerWakesOnlyForMovementOverThePicture(t *testing.T) {
	for _, c := range []struct {
		name     string
		was, now w32.Point
		want     bool
	}{
		{"moved over the film", w32.Point{X: 100, Y: 100}, w32.Point{X: 120, Y: 100}, true},
		{"still", w32.Point{X: 100, Y: 100}, w32.Point{X: 100, Y: 100}, false},
		// Reaching for another window on another screen: the reason this rule
		// exists. The first version brought the controls up for this.
		{"moved on the desk monitor", w32.Point{X: -900, Y: 400}, w32.Point{X: -700, Y: 420}, false},
		{"from the desk onto the film", w32.Point{X: -10, Y: 400}, w32.Point{X: 10, Y: 400}, true},
		{"from the film onto the desk", w32.Point{X: 10, Y: 400}, w32.Point{X: -10, Y: 400}, false},
		// Right and Bottom are outside, as everywhere in Win32.
		{"right edge is outside", w32.Point{X: 0, Y: 0}, w32.Point{X: 3840, Y: 0}, false},
		{"last pixel is inside", w32.Point{X: 0, Y: 0}, w32.Point{X: 3839, Y: 2159}, true},
	} {
		if got := pointerWakes(c.was, c.now, tv); got != c.want {
			t.Errorf("%s: pointerWakes = %v, want %v", c.name, got, c.want)
		}
	}
}

// A key that wakes the page is handed on as the page's keydown handler reads it,
// so Space pauses rather than only waking.
func TestKeyForNamesThePlayersKeys(t *testing.T) {
	for _, c := range []struct {
		name             string
		vk               uint32
		shift, ctrlOrAlt bool
		want             wakeKey
		ok               bool
	}{
		{"space", 0x20, false, false, wakeKey{Key: " ", Code: "Space"}, true},
		{"left", 0x25, false, false, wakeKey{Key: "ArrowLeft", Code: "ArrowLeft"}, true},
		{"escape", 0x1B, false, false, wakeKey{Key: "Escape", Code: "Escape"}, true},
		{"f", 'F', false, false, wakeKey{Key: "f", Code: "KeyF"}, true},
		{"shift f", 'F', true, false, wakeKey{Key: "F", Code: "KeyF", Shift: true}, true},
		{"5", '5', false, false, wakeKey{Key: "5", Code: "Digit5"}, true},
		{"comma", 0xBC, false, false, wakeKey{Key: ",", Code: "Comma"}, true},
		{"shift period", 0xBE, true, false, wakeKey{Key: ">", Code: "Period", Shift: true}, true},
		// Not guessed: these only wake.
		{"shift 5 is a symbol on some layouts", '5', true, false, wakeKey{}, false},
		{"ctrl f", 'F', false, true, wakeKey{}, false},
		{"f5", 0x74, false, false, wakeKey{}, false},
	} {
		got, ok := keyFor(c.vk, c.shift, c.ctrlOrAlt)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: keyFor = %+v, %v; want %+v, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}
