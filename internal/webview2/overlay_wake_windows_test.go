//go:build windows

package webview2

import (
	"testing"

	"lancast/internal/webview2/w32"
)

// The TV, as the primary display at 3840x2160, and a desk monitor to its left.
var tv = w32.Rect{Left: 0, Top: 0, Right: 3840, Bottom: 2160}

func TestPointerWakesOnlyForADeliberateMoveOverThePicture(t *testing.T) {
	for _, c := range []struct {
		name      string
		rest, now w32.Point
		want      bool
		newRest   w32.Point
	}{
		{"moved over the film", w32.Point{X: 100, Y: 100}, w32.Point{X: 140, Y: 100}, true, w32.Point{X: 140, Y: 100}},
		{"still", w32.Point{X: 100, Y: 100}, w32.Point{X: 100, Y: 100}, false, w32.Point{X: 100, Y: 100}},
		// A bump of the desk. Each wake costs two visible stutters as Windows
		// starts and stops composing the film, so this must not buy them.
		{"nudged", w32.Point{X: 100, Y: 100}, w32.Point{X: 123, Y: 77}, false, w32.Point{X: 100, Y: 100}},
		{"just far enough", w32.Point{X: 100, Y: 100}, w32.Point{X: 100, Y: 124}, true, w32.Point{X: 100, Y: 124}},
		// Reaching for another window on another screen: the reason this rule
		// exists. The first version brought the controls up for this.
		{"moved on the desk monitor", w32.Point{X: -900, Y: 400}, w32.Point{X: -700, Y: 420}, false, w32.Point{X: -700, Y: 420}},
		{"from the desk well onto the film", w32.Point{X: -10, Y: 400}, w32.Point{X: 40, Y: 400}, true, w32.Point{X: 40, Y: 400}},
		{"from the film onto the desk", w32.Point{X: 10, Y: 400}, w32.Point{X: -10, Y: 400}, false, w32.Point{X: -10, Y: 400}},
		// Right and Bottom are outside, as everywhere in Win32.
		{"right edge is outside", w32.Point{X: 3800, Y: 0}, w32.Point{X: 3840, Y: 0}, false, w32.Point{X: 3840, Y: 0}},
		{"last pixel is inside", w32.Point{X: 3800, Y: 2100}, w32.Point{X: 3839, Y: 2159}, true, w32.Point{X: 3839, Y: 2159}},
	} {
		wake, rest := pointerWakes(c.rest, c.now, tv)
		if wake != c.want || rest != c.newRest {
			t.Errorf("%s: pointerWakes = %v, %+v; want %v, %+v", c.name, wake, rest, c.want, c.newRest)
		}
	}
}

// A slow move is still a move: the rest holds while the pointer creeps over
// the picture, so small steps add up to a wake, as they would by hand.
func TestPointerWakesForASlowMoveThatAddsUp(t *testing.T) {
	rest := w32.Point{X: 1000, Y: 1000}
	for i, step := 0, int32(5); i < 4; i++ {
		var wake bool
		wake, rest = pointerWakes(rest, w32.Point{X: 1000 + step*int32(i+1), Y: 1000}, tv)
		if wake {
			t.Fatalf("woke after %d px", step*int32(i+1))
		}
	}
	if wake, _ := pointerWakes(rest, w32.Point{X: 1025, Y: 1000}, tv); !wake {
		t.Error("25 px in five steps did not wake")
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
