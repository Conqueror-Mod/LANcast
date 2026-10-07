package clientwindow

import "sort"

/*
 * Win+arrow for a window Windows cannot snap itself.
 *
 * Windows applies Win+arrow to the foreground window. While a film plays, that
 * is not the LANcast window: the page has moved into the overlay popup drawn
 * over the picture (ADR 0067), so the keyboard — and the foreground — belong
 * to a borderless tool window that Windows will not maximize, snap or move to
 * another screen. The same is true of the main window in fullscreen, which has
 * had its frame removed. So in exactly those two states the shortcuts did
 * nothing, and everywhere else they worked.
 *
 * In those states, and only those, the host does what Windows would have done,
 * to the main window. Everywhere else the keys are left to Windows, whose own
 * snapping (layouts, assist, cycling) is better than any copy of it.
 *
 * This file is the decision, with no Win32 in it, the same split placement.go
 * makes: what a key should do to a window on a desk of monitors is testable
 * with no window and no Windows.
 */

// WinKey is an arrow pressed with the Windows key held.
type WinKey int

const (
	WinUp WinKey = iota + 1
	WinDown
	WinLeft
	WinRight
)

// WinState is what deciding a Win+arrow needs.
type WinState struct {
	// Window is the window's rectangle now; Normal is its restored rectangle,
	// which differs from Window while it is maximized.
	Window, Normal Rect
	Maximized      bool
	Fullscreen     bool
	Monitors       []Monitor
}

// MoveKind is what to do to the window.
type MoveKind int

const (
	MoveNone MoveKind = iota
	MoveMaximize
	MoveRestore
	MoveMinimize
	// MovePlace puts the restored window at Rect, maximizing it afterwards
	// when Maximize is set.
	MovePlace
	// MoveFullscreenTo moves a fullscreen window onto Monitor.
	MoveFullscreenTo
)

// WinMove is the decision.
type WinMove struct {
	Kind     MoveKind
	Rect     Rect
	Maximize bool
	// Monitor is the screen a MoveFullscreenTo or MovePlace lands on.
	Monitor Monitor
}

/*
 * PlanWinKey decides what Win+arrow (with Shift, the move-to-monitor form)
 * does, following what Windows does for an ordinary window:
 *
 *   Win+Up          maximize
 *   Win+Down        restore a maximized window, minimize any other
 *   Win+Left/Right  snap to that half of the screen; pressed again from that
 *                   half, on to the facing half of the next screen
 *   Win+Shift+L/R   the same window on the next screen that way, wrapping
 *
 * Fullscreen answers only to Shift: a film moved to the television stays a
 * film filling the television. Up, Down and the halves have no meaning for a
 * window with no frame, and leaving fullscreen is the page's to do (Escape),
 * because the page keeps its own record of whether it is fullscreen.
 */
func PlanWinKey(key WinKey, shift bool, s WinState) WinMove {
	mons := screens(s.Monitors)
	cur, ok := monitorHolding(s.Window, mons)
	if !ok {
		return WinMove{}
	}

	if shift {
		if key != WinLeft && key != WinRight {
			return WinMove{}
		}
		next, ok := neighbour(cur, mons, key == WinRight, true)
		if !ok {
			return WinMove{}
		}
		if s.Fullscreen {
			return WinMove{Kind: MoveFullscreenTo, Monitor: next}
		}
		from := s.Window
		if s.Maximized {
			from = s.Normal
		}
		return WinMove{
			Kind:     MovePlace,
			Rect:     carry(from, cur.Work, next.Work),
			Maximize: s.Maximized,
			Monitor:  next,
		}
	}

	if s.Fullscreen {
		return WinMove{}
	}
	switch key {
	case WinUp:
		if s.Maximized {
			return WinMove{}
		}
		return WinMove{Kind: MoveMaximize}
	case WinDown:
		if s.Maximized {
			return WinMove{Kind: MoveRestore}
		}
		return WinMove{Kind: MoveMinimize}
	case WinLeft, WinRight:
		right := key == WinRight
		target := half(cur.Work, right)
		// Already in that half: on to the facing half of the next screen,
		// without wrapping — which is where Windows stops too.
		if !s.Maximized && s.Window == target {
			next, ok := neighbour(cur, mons, right, false)
			if !ok {
				return WinMove{}
			}
			return WinMove{Kind: MovePlace, Rect: half(next.Work, !right), Monitor: next}
		}
		return WinMove{Kind: MovePlace, Rect: target, Monitor: cur}
	}
	return WinMove{}
}

// screens is the usable monitors, left to right and then top to bottom: the
// order Win+Shift+arrow walks them in.
func screens(ms []Monitor) []Monitor {
	out := make([]Monitor, 0, len(ms))
	for _, m := range ms {
		if !m.Work.empty() {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Work.Left != out[j].Work.Left {
			return out[i].Work.Left < out[j].Work.Left
		}
		return out[i].Work.Top < out[j].Work.Top
	})
	return out
}

// monitorHolding is the screen showing most of the window, as Capture decides
// it, falling back to the primary for a window on none of them.
func monitorHolding(win Rect, mons []Monitor) (Monitor, bool) {
	best, area := Monitor{}, 0
	for _, m := range mons {
		if a := overlap(win, m.Work); a > area {
			best, area = m, a
		}
	}
	if area > 0 {
		return best, true
	}
	for _, m := range mons {
		if m.Primary {
			return m, true
		}
	}
	if len(mons) > 0 {
		return mons[0], true
	}
	return Monitor{}, false
}

// neighbour is the next screen after cur in that direction, wrapping round the
// ends when wrap is set. False when there is no other screen to go to.
func neighbour(cur Monitor, mons []Monitor, right, wrap bool) (Monitor, bool) {
	if len(mons) < 2 {
		return Monitor{}, false
	}
	at := -1
	for i, m := range mons {
		if m.Work == cur.Work {
			at = i
			break
		}
	}
	if at < 0 {
		return Monitor{}, false
	}
	step := -1
	if right {
		step = 1
	}
	i := at + step
	if i < 0 || i >= len(mons) {
		if !wrap {
			return Monitor{}, false
		}
		i = (i + len(mons)) % len(mons)
	}
	return mons[i], true
}

// half is the left or right half of a work area.
func half(work Rect, right bool) Rect {
	mid := work.Left + work.Width()/2
	if right {
		return Rect{Left: mid, Top: work.Top, Right: work.Right, Bottom: work.Bottom}
	}
	return Rect{Left: work.Left, Top: work.Top, Right: mid, Bottom: work.Bottom}
}

/*
 * carry moves a window from one work area to another, keeping its place on the
 * screen and its size where it fits — the same rule Resolve applies to a
 * remembered position, and for the same reason: a window carried from a large
 * screen to a small one must arrive on it, not past its edge.
 */
func carry(win, from, to Rect) Rect {
	w, h := min(win.Width(), to.Width()), min(win.Height(), to.Height())
	left := to.Left + (win.Left - from.Left)
	top := to.Top + (win.Top - from.Top)
	if left+w > to.Right {
		left = to.Right - w
	}
	if left < to.Left {
		left = to.Left
	}
	if top+h > to.Bottom {
		top = to.Bottom - h
	}
	if top < to.Top {
		top = to.Top
	}
	return Rect{Left: left, Top: top, Right: left + w, Bottom: top + h}
}

/*
 * MediaCommand names what a media key asks the player for, or "" for a key
 * that is not one. The names are what the page's __lancastMediaKey takes.
 */
func MediaCommand(vk uint32) string {
	switch vk {
	case 0xB3: // VK_MEDIA_PLAY_PAUSE
		return "playpause"
	case 0xB0: // VK_MEDIA_NEXT_TRACK
		return "next"
	case 0xB1: // VK_MEDIA_PREV_TRACK
		return "previous"
	case 0xB2: // VK_MEDIA_STOP
		return "stop"
	}
	return ""
}
