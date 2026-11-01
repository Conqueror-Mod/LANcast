//go:build windows

package webview2

import "lancast/internal/webview2/w32"

/*
 * What wakes the page while its overlay is off the screen (ADR 0111), as pure
 * functions so the rules are tested without a window.
 */

// nudge is how far, in physical pixels, the pointer must travel over the
// picture before it counts as somebody reaching for the controls: about half
// a centimetre on a 55" 4K TV.
//
// Every wake costs two visible stutters, one as Windows starts composing the
// film again and one as it stops (2-3 frames each, measured), so a bump of
// the desk should not buy them.
const nudge = 24

// pointerWakes reports whether the pointer, now at now, should bring the page
// back, and where it rests from here on. rest is where it last rested.
//
// Over the picture, not anywhere. The first version woke on any movement at
// all, so reaching for another window on another monitor (to type a message,
// say) brought the controls up over the film every time. Off the picture the
// rest follows the pointer; over it, the rest stays put, so a slow
// deliberate move adds up to a wake and a nudge does not.
func pointerWakes(rest, now w32.Point, picture w32.Rect) (wake bool, newRest w32.Point) {
	over := now.X >= picture.Left && now.X < picture.Right &&
		now.Y >= picture.Top && now.Y < picture.Bottom
	if !over {
		return false, now
	}
	if abs(now.X-rest.X) >= nudge || abs(now.Y-rest.Y) >= nudge {
		return true, now
	}
	return false, rest
}

func abs(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// wakeKey is a key, as the page's keydown handler reads one.
type wakeKey struct {
	Key   string `json:"key"`
	Code  string `json:"code"`
	Shift bool   `json:"shiftKey"`
}

// keyFor names a virtual-key code the way a browser's KeyboardEvent would, for
// the keys the player answers to. ok is false for any other key, and for any
// key with Ctrl or Alt held: those only wake the page, since the page would
// not act on them over a film and a guessed name is worse than none.
func keyFor(vk uint32, shift, ctrlOrAlt bool) (wakeKey, bool) {
	if ctrlOrAlt {
		return wakeKey{}, false
	}
	switch {
	case vk >= 'A' && vk <= 'Z':
		c := string(rune(vk))
		k := c
		if !shift {
			k = string(rune(vk + 'a' - 'A'))
		}
		return wakeKey{Key: k, Code: "Key" + c, Shift: shift}, true
	case vk >= '0' && vk <= '9' && !shift:
		d := string(rune(vk))
		return wakeKey{Key: d, Code: "Digit" + d}, true
	}
	if k, ok := namedKeys[vk]; ok {
		k.Shift = shift
		if shift && k.shifted != "" {
			k.Key = k.shifted
		}
		return k.wakeKey, true
	}
	return wakeKey{}, false
}

type namedKey struct {
	wakeKey
	shifted string
}

// namedKeys are the non-letter keys the player handles (Player.tsx onKey).
var namedKeys = map[uint32]namedKey{
	0x20: {wakeKey: wakeKey{Key: " ", Code: "Space"}},
	0x0D: {wakeKey: wakeKey{Key: "Enter", Code: "Enter"}},
	0x1B: {wakeKey: wakeKey{Key: "Escape", Code: "Escape"}},
	0x25: {wakeKey: wakeKey{Key: "ArrowLeft", Code: "ArrowLeft"}},
	0x26: {wakeKey: wakeKey{Key: "ArrowUp", Code: "ArrowUp"}},
	0x27: {wakeKey: wakeKey{Key: "ArrowRight", Code: "ArrowRight"}},
	0x28: {wakeKey: wakeKey{Key: "ArrowDown", Code: "ArrowDown"}},
	0x21: {wakeKey: wakeKey{Key: "PageUp", Code: "PageUp"}},
	0x22: {wakeKey: wakeKey{Key: "PageDown", Code: "PageDown"}},
	0x24: {wakeKey: wakeKey{Key: "Home", Code: "Home"}},
	0x23: {wakeKey: wakeKey{Key: "End", Code: "End"}},
	0xBC: {wakeKey: wakeKey{Key: ",", Code: "Comma"}, shifted: "<"},
	0xBE: {wakeKey: wakeKey{Key: ".", Code: "Period"}, shifted: ">"},
	0xBD: {wakeKey: wakeKey{Key: "-", Code: "Minus"}, shifted: "_"},
	0xBB: {wakeKey: wakeKey{Key: "=", Code: "Equal"}, shifted: "+"},
	0xDB: {wakeKey: wakeKey{Key: "[", Code: "BracketLeft"}, shifted: "{"},
	0xDD: {wakeKey: wakeKey{Key: "]", Code: "BracketRight"}, shifted: "}"},
	0xBF: {wakeKey: wakeKey{Key: "/", Code: "Slash"}, shifted: "?"},
}
