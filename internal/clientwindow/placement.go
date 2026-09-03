package clientwindow

/*
 * Which screen the window opens on, and where on it.
 *
 * The decision is pure and the Win32 is thin, the same split probe makes
 * between ParseJSON and the process: choosing a monitor is where the judgement
 * is, and it is testable with no window, no display and no Windows.
 *
 * The hard part is not moving a window, it is deciding *where* when the desk
 * has changed since last time. A monitor can be unplugged, rearranged, or have
 * its resolution changed between one launch and the next, and a remembered
 * position that ignores that opens the window somewhere nobody can see — which
 * is worse than not remembering at all, because at least the default is on
 * screen.
 */

// Rect is a screen rectangle in virtual-desktop coordinates.
//
// Left and Top may be negative: a monitor placed to the left of the primary
// one starts at a negative X, and treating the desktop as beginning at zero is
// the classic way a window ends up on the wrong screen.
type Rect struct {
	Left, Top, Right, Bottom int
}

func (r Rect) Width() int  { return r.Right - r.Left }
func (r Rect) Height() int { return r.Bottom - r.Top }
func (r Rect) empty() bool { return r.Width() <= 0 || r.Height() <= 0 }

// Monitor is one display as the system reports it.
type Monitor struct {
	// Device is the stable identity — `\\.\DISPLAY2` and friends.
	//
	// Coordinates are not identity. Two monitors swap positions and their
	// rectangles swap with them, so a window remembered by position alone
	// follows the geometry rather than the screen. The device name is what
	// survives a rearrangement.
	Device string
	// Work is the usable area, taskbar excluded. A window restored into the
	// full monitor rectangle sits under the taskbar.
	Work    Rect
	Primary bool
}

/*
 * Placement is where a window was, in terms that survive the desk changing.
 *
 * Position is stored **relative to its monitor's work area**, not in desktop
 * coordinates. A monitor that moves from the right of the primary to the left
 * changes every absolute coordinate on it while the window's position *on that
 * screen* has not changed at all, and relative coordinates are what make the
 * difference invisible.
 */
type Placement struct {
	Monitor   string `json:"monitor,omitempty"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Maximized bool   `json:"maximized,omitempty"`
}

// Valid reports whether a placement says anything worth acting on.
func (p Placement) Valid() bool {
	return p.Width > 0 && p.Height > 0
}

// minSize is the smallest window worth restoring to. A remembered size below
// this is a window somebody had collapsed, or a bad read, and opening at
// 40×30 looks like a crash.
const minSize = 320

/*
 * Resolve turns a remembered placement into a rectangle to open at.
 *
 * Returns ok=false when there is nothing sensible to say, which the caller
 * must treat as "let the system decide" rather than as an error — the default
 * position is a perfectly good answer and is always on screen.
 *
 * The monitor is matched by device name and never by coordinates. When it is
 * gone the window moves to the primary display rather than to whatever now
 * occupies those coordinates: an unplugged laptop dock should put the window
 * somewhere obvious, not somewhere arithmetically similar.
 */
func Resolve(p Placement, monitors []Monitor) (Rect, bool) {
	if !p.Valid() || len(monitors) == 0 {
		return Rect{}, false
	}

	target, ok := findMonitor(p.Monitor, monitors)
	if !ok {
		return Rect{}, false
	}

	w, h := p.Width, p.Height
	if w < minSize {
		w = minSize
	}
	if h < minSize {
		h = minSize
	}
	// Never larger than the screen it is going onto. A window remembered from a
	// 4K monitor and restored onto a laptop panel would otherwise open with its
	// controls past both edges.
	if aw := target.Work.Width(); w > aw {
		w = aw
	}
	if ah := target.Work.Height(); h > ah {
		h = ah
	}

	left := target.Work.Left + p.X
	top := target.Work.Top + p.Y

	// Clamp so the whole window is on the screen. Off-screen by a few pixels is
	// tidy; off-screen entirely is a window nobody can reach, and it is what a
	// smaller monitor or a changed resolution produces from an honest saved
	// position.
	if right := left + w; right > target.Work.Right {
		left = target.Work.Right - w
	}
	if left < target.Work.Left {
		left = target.Work.Left
	}
	if bottom := top + h; bottom > target.Work.Bottom {
		top = target.Work.Bottom - h
	}
	if top < target.Work.Top {
		top = target.Work.Top
	}

	return Rect{Left: left, Top: top, Right: left + w, Bottom: top + h}, true
}

/*
 * findMonitor picks the screen a placement refers to.
 *
 * A remembered device that is still present wins. Otherwise the primary, which
 * is the one certain to exist and certain to be somewhere the person is
 * looking. An empty remembered device — a placement written before this
 * existed, or by a build that could not name the monitor — also lands on the
 * primary rather than being refused: the size is still worth honouring.
 */
func findMonitor(device string, monitors []Monitor) (Monitor, bool) {
	if device != "" {
		for _, m := range monitors {
			if m.Device == device && !m.Work.empty() {
				return m, true
			}
		}
	}
	for _, m := range monitors {
		if m.Primary && !m.Work.empty() {
			return m, true
		}
	}
	for _, m := range monitors {
		if !m.Work.empty() {
			return m, true
		}
	}
	return Monitor{}, false
}

/*
 * Capture turns a window rectangle and the monitors into a saveable placement.
 *
 * The monitor chosen is the one holding most of the window, not the one under
 * its top-left corner: a window straddling two screens belongs to the screen
 * showing most of it, and its corner is frequently on the other one.
 */
func Capture(win Rect, monitors []Monitor, maximized bool) Placement {
	p := Placement{
		Width:     win.Width(),
		Height:    win.Height(),
		Maximized: maximized,
	}
	best, bestArea := Monitor{}, 0
	for _, m := range monitors {
		if a := overlap(win, m.Work); a > bestArea {
			best, bestArea = m, a
		}
	}
	if bestArea == 0 {
		// Off every screen — a monitor removed while the window sat on it.
		// Recording the size alone lets the next launch keep it and choose the
		// screen itself.
		return p
	}
	p.Monitor = best.Device
	p.X = win.Left - best.Work.Left
	p.Y = win.Top - best.Work.Top
	return p
}

// overlap is the area two rectangles share, or zero.
func overlap(a, b Rect) int {
	w := min(a.Right, b.Right) - max(a.Left, b.Left)
	h := min(a.Bottom, b.Bottom) - max(a.Top, b.Top)
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

/*
 * closeHandler wraps a caller's close callback so the window's position is
 * recorded first.
 *
 * Extracted so the ordering can be tested. The first version of this was
 * installed *after* the window was constructed with the original callback,
 * which takes it by value — so the wrapper never ran and the position was
 * never saved. Nothing failed, nothing logged, and the fifteen tests covering
 * the placement rules all passed, because the fault was in reaching them.
 *
 * capture runs even when the caller keeps the window alive: a close-to-tray
 * hide is not a close, but the window is at a real position at that moment,
 * and the alternative is remembering wherever it was last genuinely closed.
 */
func closeHandler(capture func(), userClose func() bool) func() bool {
	return func() bool {
		if capture != nil {
			capture()
		}
		if userClose == nil {
			return true
		}
		return userClose()
	}
}
