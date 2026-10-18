package host

import (
	"testing"

	"lancast/internal/retro/libretro"
)

// Remapped, the pad does what the mapping says and nothing else.
func TestARemappedButtonDoesItsNewJob(t *testing.T) {
	m, err := DefaultMapping().Assign("b", "pad", "X")
	if err != nil {
		t.Fatal(err)
	}
	c := m.compile()
	if p := fromXInputWith(XInputGamepad{Buttons: XInputX}, c); !p.Pressed(libretro.JoypadB) || p.Pressed(libretro.JoypadY) {
		t.Errorf("X pressed: buttons %016b, want only RetroPad B", p.Buttons)
	}
	// One control does one thing: X was Y's, and Y has lost it.
	if m.Pad["y"] != "" {
		t.Errorf("Y kept X: %q", m.Pad["y"])
	}
	// And A, B's old control, now does nothing.
	if p := fromXInputWith(XInputGamepad{Buttons: XInputA}, c); p.Buttons != 0 {
		t.Errorf("A still does something: %016b", p.Buttons)
	}
}

func TestKeysAndHoldKeysRemap(t *testing.T) {
	m, err := DefaultMapping().Assign("fast-forward", "key", "F")
	if err != nil {
		t.Fatal(err)
	}
	c := m.compile()
	if p := fromKeyboardWith(func(vk int) bool { return vk == 'F' }, c); p.Hot != HotFastForward {
		t.Errorf("F: hot %b", p.Hot)
	}
	if p := fromKeyboardWith(func(vk int) bool { return vk == VKTab }, c); p.Hot != 0 {
		t.Errorf("Tab still fast-forwards")
	}
	// Triggers are controls too.
	m, _ = DefaultMapping().Assign("a", "pad", "RT")
	if p := fromXInputWith(XInputGamepad{RightTrigger: 255}, m.compile()); !p.Pressed(libretro.JoypadA) {
		t.Error("RT did not press A")
	}
}

// The menu's own controls cannot be assigned, nor can things that do not exist.
func TestTheMenuKeysAreNotAssignable(t *testing.T) {
	for _, c := range []struct{ target, kind, name string }{
		{"b", "key", "Escape"}, {"b", "pad", "Guide"}, {"b", "pad", "Z"},
		{"jump", "pad", "A"}, {"b", "mouse", "A"},
	} {
		if _, err := DefaultMapping().Assign(c.target, c.kind, c.name); err == nil {
			t.Errorf("assigned %+v", c)
		}
	}
}

// A mapping saved before a target existed gets the default for it.
func TestAnOldMappingGetsNewTargetsFromTheDefault(t *testing.T) {
	m := Mapping{Pad: map[string]string{"b": "X"}}.WithDefaults()
	if m.Pad["b"] != "X" || m.Pad["start"] != "Start" || m.Keys["rewind"] != "R" {
		t.Errorf("merged: %+v", m)
	}
}
