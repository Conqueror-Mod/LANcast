package games

import (
	"fmt"
	"strings"
)

/*
 * Which display a game opens on (ADR 0066, amended).
 *
 * The mechanics live in the client, because moving a window is a Win32 call on
 * one machine. What lives here is everything that can be decided without one:
 * where a window should end up, what a screen is called, and which games have
 * been answered for. That split is the same one the rest of this package keeps
 * — parsing pure, the operating system at arm's length — and it is what lets
 * the geometry be tested against a three-monitor desk that the test machine
 * does not have.
 */

// DisplayDefault is a stored answer meaning "wherever it would have opened".
//
// Deliberately a value rather than an absent entry. "I chose to leave this one
// alone" and "nobody has asked me yet" are different facts, and only the second
// should raise the picker again.
const DisplayDefault = "default"

/*
 * devicePrefix is what Windows calls a display: \\.\DISPLAY1, \\.\DISPLAY2.
 *
 * Named rather than written inline because it is almost entirely backslashes,
 * and the first version of this shipped with one of them missing. The label
 * then never matched a real device, so every screen in the picker was offered
 * as "\\.\DISPLAY1" instead of "Display 1" — and the test agreed with it,
 * because the same wrong spelling had been written into both files at once.
 * Only looking at the picker found it. See the count in display_test.go.
 */
const devicePrefix = `\\.\DISPLAY`

/*
 * Rect is a rectangle in desktop coordinates.
 *
 * A copy of the one in clientwindow rather than the type itself, for the reason
 * desktopprefs keeps its own placement record: this package is read by the
 * client but owns nothing of the window's behaviour, and importing the window
 * package to borrow four ints would tie a pure parser to a Win32 refactor.
 *
 * Left and Top may be negative. A monitor to the left of the primary one starts
 * at a negative X — on the desk this was written for, the third screen begins
 * at x = -2560 — and code that assumes the desktop starts at zero is the
 * classic way a window lands somewhere nobody can see.
 */
type Rect struct {
	Left, Top, Right, Bottom int
}

func (r Rect) Width() int  { return r.Right - r.Left }
func (r Rect) Height() int { return r.Bottom - r.Top }

// Display is one screen, as offered to the person choosing.
type Display struct {
	// Device is the identity: \.\DISPLAY2 and friends. Stored, compared and
	// remembered by this, never by position — two monitors swapped in Windows'
	// display settings swap their rectangles with them, and a remembered
	// position would follow the geometry rather than the screen.
	Device string `json:"device"`
	// Label is what the picker shows.
	Label string `json:"label"`
	// Primary is Windows' main display, and Current is the one LANcast's own
	// window is on — "not this one" is the most common thing somebody wants.
	Primary bool `json:"primary"`
	Current bool `json:"current"`
	Width   int  `json:"width"`
	Height  int  `json:"height"`
}

// DisplayLabel names a screen for a person rather than for Windows.
//
// The device name is the identity and is unreadable; the number in it is the
// only part anybody recognises, and it matches what Windows' own display
// settings show, which is where somebody will go to check.
func DisplayLabel(device string, width, height int, primary bool) string {
	name := device
	if n := strings.TrimPrefix(device, devicePrefix); n != device && n != "" {
		name = "Display " + n
	}
	label := fmt.Sprintf("%s — %d x %d", name, width, height)
	if primary {
		label += " (main)"
	}
	return label
}

/*
 * MoveTarget is where a window should be put to sit on a display.
 *
 * Centred in the work area, which is the taskbar-excluded part: a window
 * restored into the full monitor rectangle sits under the taskbar.
 *
 * It returns a position and never a size. A game sized its own window to the
 * render target it chose, and resizing that behind its back invites a stretched
 * picture or a confused renderer — the request was "open it over there", not
 * "open it over there and make it fit". A window larger than the screen is
 * pinned to the top-left corner instead, so that its own top-left — where every
 * menu it has starts — is the part that stays reachable.
 */
func MoveTarget(win, work Rect) (x, y int) {
	w, h := win.Width(), win.Height()
	x = work.Left + (work.Width()-w)/2
	y = work.Top + (work.Height()-h)/2
	if w >= work.Width() {
		x = work.Left
	}
	if h >= work.Height() {
		y = work.Top
	}
	return x, y
}

/*
 * LooksLikeGameWindow is the guess at the far end of a launch.
 *
 * It is a guess because nothing tells us which window is the game: Steam starts
 * it detached, so the client never learns a process to watch, and all it can do
 * is notice a window that was not there a moment ago.
 *
 * Untitled and tiny are what rule things out. A splash bitmap, a tooltip, an
 * off-screen message sink and an anti-cheat's hidden window are all real
 * top-level windows that appear in the same second a game starts, and moving
 * one of those instead would do nothing visible while consuming the one move
 * this feature gets.
 */
func LooksLikeGameWindow(title string, width, height int) bool {
	if strings.TrimSpace(title) == "" {
		return false
	}
	return width >= 320 && height >= 240
}

// DisplayFor is the stored answer for a game, empty when it has never been
// asked about.
func (p Prefs) DisplayFor(id string) string { return p.Displays[id] }

// SetDisplay records an answer. An empty device forgets it, which is what the
// detail page's "ask me again" does.
func (p *Prefs) SetDisplay(id, device string) {
	if device == "" {
		delete(p.Displays, id)
		if len(p.Displays) == 0 {
			p.Displays = nil
		}
		return
	}
	if p.Displays == nil {
		p.Displays = map[string]string{}
	}
	p.Displays[id] = device
}
