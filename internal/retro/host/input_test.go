package host

import (
	"testing"

	"lancast/internal/retro/libretro"
)

// The bottom face button is RetroPad B and the right one RetroPad A — by
// position, as a Nintendo game expects — not by the letter printed on it.
func TestXInputMapsByPosition(t *testing.T) {
	cases := map[uint16]int{
		XInputA: libretro.JoypadB, XInputB: libretro.JoypadA,
		XInputX: libretro.JoypadY, XInputY: libretro.JoypadX,
		XInputStart: libretro.JoypadStart, XInputBack: libretro.JoypadSelect,
		XInputLeftShldr: libretro.JoypadL, XInputRightShdr: libretro.JoypadR,
		XInputDPadUp: libretro.JoypadUp, XInputDPadRight: libretro.JoypadRight,
		XInputLeftThumb: libretro.JoypadL3,
	}
	for xb, want := range cases {
		p := FromXInput(XInputGamepad{Buttons: xb})
		if p.Buttons != 1<<want {
			t.Errorf("xinput %#04x -> buttons %016b, want only %d", xb, p.Buttons, want)
		}
	}
}

func TestTriggersAreDigitalPastTheThreshold(t *testing.T) {
	if p := FromXInput(XInputGamepad{LeftTrigger: 20}); p.Pressed(libretro.JoypadL2) {
		t.Error("a resting trigger pressed L2")
	}
	if p := FromXInput(XInputGamepad{RightTrigger: 200}); !p.Pressed(libretro.JoypadR2) {
		t.Error("a pulled trigger did not press R2")
	}
}

// Inside the dead zone a stick is still; just past it the value is small, not
// a jump; at the edge it is full; and up is negative.
func TestSticks(t *testing.T) {
	if p := FromXInput(XInputGamepad{ThumbLX: 7000, ThumbLY: -7000}); p.Analog[0] != [2]int16{0, 0} {
		t.Errorf("a stick in its dead zone moved: %v", p.Analog[0])
	}
	p := FromXInput(XInputGamepad{ThumbLX: 8000})
	if p.Analog[0][0] <= 0 || p.Analog[0][0] > 400 {
		t.Errorf("just past the dead zone = %d, want a small positive value", p.Analog[0][0])
	}
	p = FromXInput(XInputGamepad{ThumbLX: 32767, ThumbLY: 32767, ThumbRX: -32768})
	if p.Analog[0][0] != 32767 || p.Analog[0][1] != -32767 || p.Analog[1][0] != -32768 {
		t.Errorf("full deflection = %v %v", p.Analog[0], p.Analog[1])
	}
	if p.State(libretro.DeviceAnalog, libretro.AnalogIndexLeft, libretro.AnalogIDY) != -32767 {
		t.Error("up on the stick is not negative to the core")
	}
}

func TestPadStateAnswersTheCore(t *testing.T) {
	p := FromXInput(XInputGamepad{Buttons: XInputB | XInputStart})
	if p.State(libretro.DeviceJoypad, 0, libretro.JoypadA) != 1 || p.State(libretro.DeviceJoypad, 0, libretro.JoypadB) != 0 {
		t.Error("joypad state wrong")
	}
	if got := p.State(libretro.DeviceJoypad, 0, libretro.DeviceIDJoypadMask); got != int16(1<<libretro.JoypadA|1<<libretro.JoypadStart) {
		t.Errorf("bitmask = %016b", got)
	}
	if p.State(libretro.DeviceKeyboard, 0, 1) != 0 || p.State(libretro.DeviceJoypad, 0, 99) != 0 {
		t.Error("an unsupported question was answered")
	}
}

func TestKeyboardFallback(t *testing.T) {
	held := map[int]bool{VKZ: true, VKReturn: true, VKUp: true, VKEscape: true}
	p := FromKeyboard(func(vk int) bool { return held[vk] })
	want := uint16(1<<libretro.JoypadB | 1<<libretro.JoypadStart | 1<<libretro.JoypadUp)
	if p.Buttons != want {
		t.Errorf("buttons = %016b, want %016b (Escape is the menu, not a button)", p.Buttons, want)
	}
	pad := FromXInput(XInputGamepad{Buttons: XInputA, ThumbLX: 32767})
	m := Merge(pad, p)
	if !m.Pressed(libretro.JoypadUp) || !m.Pressed(libretro.JoypadB) || m.Analog[0][0] != 32767 {
		t.Errorf("merged = %+v", m)
	}
}

// Escape or Guide open the menu at once; Select+Start only once held for a
// second, so a game's own soft-reset combination still reaches it. Holding
// does not reopen the menu every frame.
func TestMenuRequest(t *testing.T) {
	var m MenuRequest
	if !m.Update(Pad{}, false, true, 60) {
		t.Error("Escape did not open the menu")
	}
	if m.Update(Pad{}, false, true, 60) {
		t.Error("holding Escape reopened the menu")
	}
	m.Update(Pad{}, false, false, 60)

	combo := Pad{Buttons: 1<<libretro.JoypadSelect | 1<<libretro.JoypadStart}
	opened := -1
	for f := 0; f < 90; f++ {
		if m.Update(combo, false, false, 60) {
			opened = f
			break
		}
	}
	if opened != 59 {
		t.Errorf("Select+Start opened the menu on frame %d, want 59 (one second at 60fps)", opened)
	}
	var tap MenuRequest
	for f := 0; f < 10; f++ {
		if tap.Update(combo, false, false, 60) {
			t.Fatal("a tap of Select+Start opened the menu")
		}
	}
	var g MenuRequest
	if !g.Update(Pad{}, true, false, 60) {
		t.Error("Guide did not open the menu")
	}
}
