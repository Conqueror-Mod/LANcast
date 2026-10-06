package games

import (
	"fmt"
	"strings"
	"time"
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
	// Device is the identity: the monitor's device path where Windows gives
	// one, else the GDI name (DISPLAY2 and friends), which the driver
	// renumbers. Stored, compared and remembered by this, never by position — two monitors swapped in Windows'
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

/*
 * Screen is one monitor, as DisplayLabels reads it.
 *
 * Name is the monitor's own EDID name ("C27F398", "Roku 55R4AX"), empty when
 * Windows has none. Rect places it on the desktop, for saying where it is
 * relative to the main screen.
 */
type Screen struct {
	Name          string
	Width, Height int
	Rect          Rect
	Primary       bool
}

// genericNames are what Windows calls a monitor it knows nothing about. They
// say nothing a person can use, so the screen is called "Screen" instead and
// told apart by where it is.
var genericNames = map[string]bool{
	"": true, "display": true, "generic pnp monitor": true, "generic non-pnp monitor": true,
	"default monitor": true,
}

/*
 * DisplayLabels names every screen for a person.
 *
 * Not by the number in a device name like DISPLAY6. That number is the
 * graphics driver's, not the one Windows' display settings show, and it
 * climbs every time the driver re-enumerates: three monitors on one desk read
 * as Displays 6, 7 and 8, which matched nothing anybody could see. The earlier
 * version of this comment claimed it matched Windows' settings. It did not.
 *
 * So a screen is named by what it is and where it is: its own name when it has
 * a useful one, and, for every screen but the main one, which side of the main
 * screen it sits on. "Roku 55R4AX, left of main — 3840 x 2160". Two identical
 * monitors are told apart by position too.
 */
func DisplayLabels(screens []Screen) []string {
	var main *Screen
	for i := range screens {
		if screens[i].Primary {
			main = &screens[i]
			break
		}
	}
	out := make([]string, len(screens))
	for i, sc := range screens {
		name := strings.TrimSpace(sc.Name)
		if genericNames[strings.ToLower(name)] {
			name = "Screen"
		}
		if !sc.Primary && main != nil {
			if where := relativePosition(sc.Rect, main.Rect); where != "" {
				name += ", " + where
			}
		}
		label := fmt.Sprintf("%s — %d x %d", name, sc.Width, sc.Height)
		if sc.Primary {
			label += " (main)"
		}
		out[i] = label
	}
	return out
}

// relativePosition says which side of ref r is on, by the larger offset
// between their centres: "left of main", "above main".
func relativePosition(r, ref Rect) string {
	dx := (r.Left + r.Right - ref.Left - ref.Right) / 2
	dy := (r.Top + r.Bottom - ref.Top - ref.Bottom) / 2
	if dx == 0 && dy == 0 {
		return ""
	}
	if absInt(dx) >= absInt(dy) {
		if dx < 0 {
			return "left of main"
		}
		return "right of main"
	}
	if dy < 0 {
		return "above main"
	}
	return "below main"
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

/*
 * Plan is how to put a window on a display.
 *
 * X, Y place it. Width and Height are zero to keep the window's own size, or
 * the size to give it. Restore and Maximize bracket the move for a window that
 * was maximized: a maximized window moved by position stays the size of the
 * screen it was maximized on, which is how a game maximized on a 4K screen and
 * sent to a 1080p one ends up across two screens. It has to be restored,
 * moved, and maximized again where it now is.
 */
type Plan struct {
	X, Y          int
	Width, Height int
	Restore       bool
	Maximize      bool
}

/*
 * PlanMove decides how to put a window on a display with work area work.
 *
 *   - A maximized window: restore it, centre its normal size on the display
 *     (clamped to fit), and maximize it again there.
 *   - A window bigger than the display: give it the work area, exactly. Moving
 *     it alone pins its corner to the display's corner and leaves the rest of
 *     it on the next screen, which is what the first version did to Minecraft,
 *     twenty-eight times in twenty seconds.
 *   - Anything else: centre it and keep its size (MoveTarget). A game sized its
 *     window to its renderer, and resizing that behind its back is not asked.
 *
 * normal is the window's restored size, used for a maximized window; an empty
 * one means not known, and the work area stands in for it.
 */
func PlanMove(win Rect, maximized bool, normal Rect, work Rect) Plan {
	if maximized {
		w, h := normal.Width(), normal.Height()
		if w <= 0 || w > work.Width() {
			w = work.Width()
		}
		if h <= 0 || h > work.Height() {
			h = work.Height()
		}
		x, y := MoveTarget(Rect{Right: w, Bottom: h}, work)
		return Plan{X: x, Y: y, Width: w, Height: h, Restore: true, Maximize: true}
	}
	if win.Width() > work.Width() || win.Height() > work.Height() {
		return Plan{X: work.Left, Y: work.Top, Width: work.Width(), Height: work.Height()}
	}
	x, y := MoveTarget(win, work)
	return Plan{X: x, Y: y}
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

/*
 * WatchDeadline is when to stop watching for a game's window.
 *
 * A flat window from the launch is right for a game that opens its own window
 * and wrong for one that opens a launcher first. Zenless Zone Zero is the
 * second kind — its Steam entry starts HoYoPlay, and the game arrives only
 * after somebody has clicked through it — so a watcher that gave up ninety
 * seconds after Play was a display setting that silently did nothing.
 *
 * So the clock extends from the last window actually moved, which is evidence
 * the chain is still unfolding, and `limit` ends it regardless: a watch a busy
 * desktop can keep alive indefinitely becomes a process moving windows long
 * after anybody would connect it to having pressed Play.
 */
func WatchDeadline(start, lastMove time.Time, base, afterMove, limit time.Duration) time.Time {
	deadline := start.Add(base)
	if !lastMove.IsZero() {
		if extended := lastMove.Add(afterMove); extended.After(deadline) {
			deadline = extended
		}
	}
	if capped := start.Add(limit); deadline.After(capped) {
		return capped
	}
	return deadline
}

// DisplayFor is the stored answer for a game, empty when it has never been
// asked about.
func (p Prefs) DisplayFor(id string) string {
	if v, ok := p.Displays[id]; ok {
		return v
	}
	// The pre-namespace spelling, so a game already answered for is not asked
	// again. See legacyID.
	return p.Displays[legacyID(id)]
}

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
