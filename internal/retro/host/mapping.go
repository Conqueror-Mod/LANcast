package host

import (
	"fmt"
	"sort"
	"sync/atomic"

	"lancast/internal/retro/libretro"
)

/*
 * Which control does what: the game menu's Controls (2026-10-18).
 *
 * A Mapping names, for each thing a game or the player can be asked to do (a
 * RetroPad button, or fast-forward and rewind), the pad control and the
 * keyboard key that do it. Names, not codes, so a saved file reads as what it
 * means ("b": "A", "start": "Enter"). Compiled once into bit masks for the
 * poll, which runs every frame.
 *
 * The menu's own keys are not in it and cannot be: Escape and the Guide
 * button always open the menu, whatever else is remapped, so nobody can map
 * themselves out of the way back.
 */
type Mapping struct {
	Pad  map[string]string `json:"pad"`  // target → pad control name
	Keys map[string]string `json:"keys"` // target → key name
}

// Targets, in the order the Controls page lists them.
var Targets = []string{
	"up", "down", "left", "right",
	"b", "a", "y", "x",
	"l", "r", "l2", "r2", "l3", "r3",
	"start", "select",
	"fast-forward", "rewind",
}

var targetButton = map[string]int{
	"up": libretro.JoypadUp, "down": libretro.JoypadDown,
	"left": libretro.JoypadLeft, "right": libretro.JoypadRight,
	"b": libretro.JoypadB, "a": libretro.JoypadA, "y": libretro.JoypadY, "x": libretro.JoypadX,
	"l": libretro.JoypadL, "r": libretro.JoypadR, "l2": libretro.JoypadL2, "r2": libretro.JoypadR2,
	"l3": libretro.JoypadL3, "r3": libretro.JoypadR3,
	"start": libretro.JoypadStart, "select": libretro.JoypadSelect,
}

var targetHot = map[string]uint8{"fast-forward": HotFastForward, "rewind": HotRewind}

// Triggers are not XInput button bits; they get two of their own above them.
const (
	padLT uint32 = 1 << 16
	padRT uint32 = 1 << 17
)

// PadControls are the pad's controls by name. Guide is absent on purpose.
var PadControls = map[string]uint32{
	"A": XInputA, "B": XInputB, "X": XInputX, "Y": XInputY,
	"LB": XInputLeftShldr, "RB": XInputRightShdr, "LT": padLT, "RT": padRT,
	"Start": XInputStart, "Back": XInputBack,
	"Up": XInputDPadUp, "Down": XInputDPadDown, "Left": XInputDPadLeft, "Right": XInputDPadRight,
	"LS": XInputLeftThumb, "RS": XInputRightThmb,
}

// KeyCodes are the keys that can be assigned, by name. Escape is absent on
// purpose.
var KeyCodes = func() map[string]int {
	m := map[string]int{
		"Up": VKUp, "Down": VKDown, "Left": VKLeft, "Right": VKRight,
		"Enter": VKReturn, "Backspace": VKBack, "Tab": VKTab, "Space": 0x20,
		"Shift": VKShift, "Ctrl": 0x11, "Alt": 0x12,
	}
	for c := 'A'; c <= 'Z'; c++ {
		m[string(c)] = int(c)
	}
	for c := '0'; c <= '9'; c++ {
		m[string(c)] = int(c)
	}
	return m
}()

// DefaultMapping is the layout LANcast always had: the pad by position (the
// bottom face button is RetroPad B, as a Nintendo game expects), the keyboard
// fallback of arrows, Z X A S Q W, Enter and Backspace, and the hold keys.
func DefaultMapping() Mapping {
	return Mapping{
		Pad: map[string]string{
			"up": "Up", "down": "Down", "left": "Left", "right": "Right",
			"b": "A", "a": "B", "y": "X", "x": "Y",
			"l": "LB", "r": "RB", "l2": "LT", "r2": "RT",
			"start": "Start", "select": "Back",
			"fast-forward": "RS", "rewind": "LS",
		},
		Keys: map[string]string{
			"up": "Up", "down": "Down", "left": "Left", "right": "Right",
			"b": "Z", "a": "X", "y": "A", "x": "S",
			"l": "Q", "r": "W",
			"start": "Enter", "select": "Backspace",
			"fast-forward": "Tab", "rewind": "R",
		},
	}
}

// WithDefaults fills anything m does not mention from the default layout,
// so a mapping saved before a target existed still has one.
func (m Mapping) WithDefaults() Mapping {
	d := DefaultMapping()
	out := Mapping{Pad: map[string]string{}, Keys: map[string]string{}}
	for k, v := range d.Pad {
		out.Pad[k] = v
	}
	for k, v := range d.Keys {
		out.Keys[k] = v
	}
	for k, v := range m.Pad {
		out.Pad[k] = v
	}
	for k, v := range m.Keys {
		out.Keys[k] = v
	}
	return out
}

/*
 * Assign gives target a pad control (kind "pad") or a key (kind "key"),
 * taking it from whatever had it: one control does one thing. An empty name
 * clears the target.
 */
func (m Mapping) Assign(target, kind, name string) (Mapping, error) {
	if _, ok := targetButton[target]; !ok {
		if _, ok := targetHot[target]; !ok {
			return m, fmt.Errorf("nothing called %q to assign", target)
		}
	}
	out := m.WithDefaults()
	var table map[string]string
	switch kind {
	case "pad":
		if _, ok := PadControls[name]; name != "" && !ok {
			return m, fmt.Errorf("no pad control %q", name)
		}
		table = out.Pad
	case "key":
		if _, ok := KeyCodes[name]; name != "" && !ok {
			return m, fmt.Errorf("no key %q", name)
		}
		table = out.Keys
	default:
		return m, fmt.Errorf("no kind %q", kind)
	}
	if name != "" {
		for t, n := range table {
			if n == name && t != target {
				table[t] = ""
			}
		}
	}
	table[target] = name
	return out, nil
}

// compiled is a Mapping as masks, for the per-frame poll.
type compiled struct {
	pad  []bind
	keys []bind
}

type bind struct {
	control uint32 // pad bits, or a virtual-key code
	button  int    // RetroPad button, or -1
	hot     uint8
}

func (m Mapping) compile() compiled {
	m = m.WithDefaults()
	var c compiled
	targets := append([]string(nil), Targets...)
	sort.Strings(targets) // stable order; the result does not depend on it
	for _, t := range targets {
		b := bind{button: -1}
		if btn, ok := targetButton[t]; ok {
			b.button = btn
		} else {
			b.hot = targetHot[t]
		}
		if bits, ok := PadControls[m.Pad[t]]; ok {
			pb := b
			pb.control = bits
			c.pad = append(c.pad, pb)
		}
		if vk, ok := KeyCodes[m.Keys[t]]; ok {
			kb := b
			kb.control = uint32(vk)
			c.keys = append(c.keys, kb)
		}
	}
	return c
}

func (b bind) apply(p *Pad) {
	if b.button >= 0 {
		p.set(b.button, true)
	} else {
		p.Hot |= b.hot
	}
}

// padBits is the pad's buttons with the triggers as two more bits.
func padBits(g XInputGamepad) uint32 {
	bits := uint32(g.Buttons)
	if g.LeftTrigger > triggerThreshold {
		bits |= padLT
	}
	if g.RightTrigger > triggerThreshold {
		bits |= padRT
	}
	return bits
}

// fromXInputWith maps a pad through c; the sticks are not remappable.
func fromXInputWith(g XInputGamepad, c compiled) Pad {
	var p Pad
	bits := padBits(g)
	for _, b := range c.pad {
		if bits&b.control != 0 {
			b.apply(&p)
		}
	}
	p.Analog[0][0] = deadZone(g.ThumbLX, leftDeadZone)
	p.Analog[0][1] = invert(deadZone(g.ThumbLY, leftDeadZone))
	p.Analog[1][0] = deadZone(g.ThumbRX, rightDeadZone)
	p.Analog[1][1] = invert(deadZone(g.ThumbRY, rightDeadZone))
	return p
}

// fromKeyboardWith maps the keys held down through c.
func fromKeyboardWith(down func(vk int) bool, c compiled) Pad {
	var p Pad
	for _, b := range c.keys {
		if down(int(b.control)) {
			b.apply(&p)
		}
	}
	return p
}

var defaultCompiled = DefaultMapping().compile()

// Mappings holds the mapping the controllers use, swappable while a game
// runs (the Controls page applies a change at once). The zero value is the
// default layout.
type Mappings struct{ p atomic.Pointer[compiled] }

// Set makes m the mapping in use.
func (h *Mappings) Set(m Mapping) {
	c := m.compile()
	h.p.Store(&c)
}

func (h *Mappings) current() compiled {
	if h == nil {
		return defaultCompiled
	}
	if c := h.p.Load(); c != nil {
		return *c
	}
	return defaultCompiled
}
