package host

import (
	"time"

	"lancast/internal/retro/libretro"
)

/*
 * Input: whatever the person holds, as libretro's RetroPad.
 *
 * Pure. The Windows side reads XInput and the keyboard and hands their raw
 * state to the functions here, so the mapping — which is the part people
 * notice when it is wrong — is tested without a controller.
 */

// Pad is one player's RetroPad: a bit per button, and two sticks.
type Pad struct {
	Buttons uint16      // bit n is RetroPad button n (libretro.JoypadB …)
	Analog  [2][2]int16 // [stick][axis], stick 0 left, 1 right; axis 0 x, 1 y
}

func (p *Pad) set(button int, down bool) {
	if down {
		p.Buttons |= 1 << button
	}
}

// Pressed reports whether a RetroPad button is down.
func (p Pad) Pressed(button int) bool { return p.Buttons&(1<<button) != 0 }

// State answers a core's input_state question for this pad.
func (p Pad) State(device, index, id uint32) int16 {
	switch device {
	case libretro.DeviceJoypad:
		if id == libretro.DeviceIDJoypadMask {
			return int16(p.Buttons)
		}
		if id < libretro.JoypadButtons && p.Pressed(int(id)) {
			return 1
		}
	case libretro.DeviceAnalog:
		if index < 2 && id < 2 {
			return p.Analog[index][id]
		}
	}
	return 0
}

// XInput's gamepad, as XInputGetState reports it.
type XInputGamepad struct {
	Buttons      uint16
	LeftTrigger  uint8
	RightTrigger uint8
	ThumbLX      int16
	ThumbLY      int16
	ThumbRX      int16
	ThumbRY      int16
}

// XInput button bits.
const (
	XInputDPadUp    = 0x0001
	XInputDPadDown  = 0x0002
	XInputDPadLeft  = 0x0004
	XInputDPadRight = 0x0008
	XInputStart     = 0x0010
	XInputBack      = 0x0020
	XInputLeftThumb = 0x0040
	XInputRightThmb = 0x0080
	XInputLeftShldr = 0x0100
	XInputRightShdr = 0x0200
	XInputGuide     = 0x0400 // only from XInputGetStateEx
	XInputA         = 0x1000
	XInputB         = 0x2000
	XInputX         = 0x4000
	XInputY         = 0x8000
)

// Dead zones Microsoft recommends for the two sticks, and the trigger
// threshold: below these a resting stick or trigger reads as untouched.
const (
	leftDeadZone     = 7849
	rightDeadZone    = 8689
	triggerThreshold = 30
)

/*
 * FromXInput maps an Xbox pad onto the RetroPad by position, which is how
 * RetroArch maps it and what a Nintendo game expects: the bottom face button
 * is RetroPad B and the right one RetroPad A. Mapping by the letters printed
 * on the buttons instead would swap confirm and cancel in every SNES game.
 *
 * Y on the stick axis is inverted: XInput's up is positive, libretro's up is
 * negative, as on screen.
 */
func FromXInput(g XInputGamepad) Pad {
	var p Pad
	b := g.Buttons
	p.set(libretro.JoypadUp, b&XInputDPadUp != 0)
	p.set(libretro.JoypadDown, b&XInputDPadDown != 0)
	p.set(libretro.JoypadLeft, b&XInputDPadLeft != 0)
	p.set(libretro.JoypadRight, b&XInputDPadRight != 0)
	p.set(libretro.JoypadStart, b&XInputStart != 0)
	p.set(libretro.JoypadSelect, b&XInputBack != 0)
	p.set(libretro.JoypadL3, b&XInputLeftThumb != 0)
	p.set(libretro.JoypadR3, b&XInputRightThmb != 0)
	p.set(libretro.JoypadL, b&XInputLeftShldr != 0)
	p.set(libretro.JoypadR, b&XInputRightShdr != 0)
	p.set(libretro.JoypadB, b&XInputA != 0)
	p.set(libretro.JoypadA, b&XInputB != 0)
	p.set(libretro.JoypadY, b&XInputX != 0)
	p.set(libretro.JoypadX, b&XInputY != 0)
	p.set(libretro.JoypadL2, g.LeftTrigger > triggerThreshold)
	p.set(libretro.JoypadR2, g.RightTrigger > triggerThreshold)
	p.Analog[0][0] = deadZone(g.ThumbLX, leftDeadZone)
	p.Analog[0][1] = invert(deadZone(g.ThumbLY, leftDeadZone))
	p.Analog[1][0] = deadZone(g.ThumbRX, rightDeadZone)
	p.Analog[1][1] = invert(deadZone(g.ThumbRY, rightDeadZone))
	return p
}

/*
 * deadZone zeroes a stick inside its dead zone and rescales the rest to the
 * full range, so the first movement past the zone is a small value rather
 * than a jump to a third of full deflection. An N64 game that walks at a
 * light touch needs that.
 */
func deadZone(v int16, zone int32) int16 {
	x := int32(v)
	switch {
	case x > zone:
		return int16((x - zone) * 32767 / (32767 - zone))
	case x < -zone:
		return int16((x + zone) * 32768 / (32768 - zone))
	}
	return 0
}

func invert(v int16) int16 {
	if v == -32768 {
		return 32767
	}
	return -v
}

// Virtual key codes used by the keyboard fallback.
const (
	VKBack   = 0x08
	VKReturn = 0x0D
	VKShift  = 0x10
	VKEscape = 0x1B
	VKLeft   = 0x25
	VKUp     = 0x26
	VKRight  = 0x27
	VKDown   = 0x28
	VKA      = 0x41
	VKQ      = 0x51
	VKS      = 0x53
	VKW      = 0x57
	VKX      = 0x58
	VKZ      = 0x5A
)

/*
 * keyboardMap is the fallback for someone without a pad: arrows to move, Z
 * and X for the two main buttons in the positions they have on a pad (Z is
 * the bottom one), A and S for the other two, Q and W for the shoulders,
 * Enter and Backspace for Start and Select. Escape is the menu, not a button,
 * so it never reaches the game.
 */
var keyboardMap = map[int]int{
	VKUp: libretro.JoypadUp, VKDown: libretro.JoypadDown,
	VKLeft: libretro.JoypadLeft, VKRight: libretro.JoypadRight,
	VKZ: libretro.JoypadB, VKX: libretro.JoypadA,
	VKA: libretro.JoypadY, VKS: libretro.JoypadX,
	VKQ: libretro.JoypadL, VKW: libretro.JoypadR,
	VKReturn: libretro.JoypadStart, VKBack: libretro.JoypadSelect,
}

// FromKeyboard maps the keys held down onto the RetroPad.
func FromKeyboard(down func(vk int) bool) Pad {
	var p Pad
	for vk, button := range keyboardMap {
		p.set(button, down(vk))
	}
	return p
}

// Merge combines a pad and the keyboard into one player: either can press a
// button, and a stick comes from the pad.
func Merge(a, b Pad) Pad {
	out := a
	out.Buttons |= b.Buttons
	return out
}

/*
 * MenuRequest decides when the person has asked for the pause menu.
 *
 * Escape on the keyboard. On a pad, the Guide button where the driver reports
 * it; otherwise Select and Start held together for a second. Held, because a
 * few games use Select+Start themselves — as a soft reset — and a tap must
 * still reach the game.
 */
type MenuRequest struct {
	comboFrames int
	latched     bool
}

// Update is called once per frame and reports true on the frame the menu
// should open. fps is the core's frame rate.
func (m *MenuRequest) Update(pad Pad, guide, escape bool, fps float64) bool {
	combo := pad.Pressed(libretro.JoypadSelect) && pad.Pressed(libretro.JoypadStart)
	if combo {
		m.comboFrames++
	} else {
		m.comboFrames = 0
	}
	held := fps > 0 && float64(m.comboFrames) >= fps
	want := guide || escape || held
	fire := want && !m.latched
	m.latched = want
	return fire
}

/*
 * MenuNav turns a pad into menu moves while the game is paused.
 *
 * The game loop stops reading the pad when the menu opens, because the core
 * is not running, so before this the pad that opened the menu with Guide
 * could do nothing else in it. These are the moves the page's focus
 * controller already understands as keys: the D-pad or the left stick to
 * move, the bottom face button to choose, and the right face button, Start
 * or Guide to go back, which closes the menu and resumes.
 *
 * Presses, not states: a button held when the menu opened (the Guide that
 * opened it, or Select+Start) is ignored until it is let go, or the menu
 * would close the instant it appeared. A held direction repeats, slowly at
 * first, the way a held key does.
 */
type MenuNav struct {
	ignore, held uint16
	dir          uint16
	next         time.Time
}

const (
	navUp uint16 = 1 << iota
	navDown
	navLeft
	navRight
	navSelect
	navBack

	navDirs = navUp | navDown | navLeft | navRight

	// navStick is how far the left stick must lean to count as a direction:
	// about half way, so a resting or brushed stick does not wander the menu.
	navStick = 16000
	// navFirstRepeat and navRepeat pace a held direction.
	navFirstRepeat = 400 * time.Millisecond
	navRepeat      = 130 * time.Millisecond
)

var navNames = []struct {
	bit  uint16
	name string
}{{navUp, "up"}, {navDown, "down"}, {navLeft, "left"}, {navRight, "right"}, {navSelect, "select"}, {navBack, "back"}}

func navBits(p Pad, guide bool) uint16 {
	var b uint16
	x, y := p.Analog[0][0], p.Analog[0][1]
	if p.Pressed(libretro.JoypadUp) || y < -navStick {
		b |= navUp
	}
	if p.Pressed(libretro.JoypadDown) || y > navStick {
		b |= navDown
	}
	if p.Pressed(libretro.JoypadLeft) || x < -navStick {
		b |= navLeft
	}
	if p.Pressed(libretro.JoypadRight) || x > navStick {
		b |= navRight
	}
	if p.Pressed(libretro.JoypadB) {
		b |= navSelect
	}
	if p.Pressed(libretro.JoypadA) || p.Pressed(libretro.JoypadStart) || guide {
		b |= navBack
	}
	return b
}

// Reset starts a menu: whatever is held now is ignored until released.
func (m *MenuNav) Reset(p Pad, guide bool) {
	m.ignore = navBits(p, guide)
	m.held, m.dir = 0, 0
}

// Update reports the moves since the last call: "up", "down", "left",
// "right", "select" or "back".
func (m *MenuNav) Update(p Pad, guide bool, now time.Time) []string {
	cur := navBits(p, guide)
	m.ignore &= cur
	live := cur &^ m.ignore
	pressed := live &^ m.held
	m.held = live

	var fire uint16
	if d := pressed & navDirs; d != 0 {
		// A new direction wins over one still held, and only one fires.
		for _, n := range navNames[:4] {
			if d&n.bit != 0 {
				m.dir, m.next = n.bit, now.Add(navFirstRepeat)
				fire |= n.bit
				break
			}
		}
	} else if m.dir != 0 {
		switch {
		case live&m.dir == 0:
			m.dir = 0
		case !now.Before(m.next):
			fire |= m.dir
			m.next = now.Add(navRepeat)
		}
	}
	fire |= pressed & (navSelect | navBack)

	var out []string
	for _, n := range navNames {
		if fire&n.bit != 0 {
			out = append(out, n.name)
		}
	}
	return out
}
