package host

import (
	"bytes"
	"encoding/binary"
	"testing"

	"lancast/internal/retro/libretro"
)

func frame16(format libretro.PixelFormat, w, h, pitch int, px ...uint16) libretro.Frame {
	data := make([]byte, pitch*h)
	for i, p := range px {
		y, x := i/w, i%w
		binary.LittleEndian.PutUint16(data[y*pitch+x*2:], p)
	}
	return libretro.Frame{Data: data, Width: uint32(w), Height: uint32(h), Pitch: uintptr(pitch), Format: format}
}

// Full intensity in every 16-bit format is 255, not 248: the low bits are
// filled from the high ones.
func TestToBGRAExpandsToFullRange(t *testing.T) {
	white565 := frame16(libretro.FormatRGB565, 1, 1, 2, 0xFFFF)
	if got := ToBGRA(white565, nil); !bytes.Equal(got, []byte{255, 255, 255, 255}) {
		t.Errorf("RGB565 white = %v", got)
	}
	white1555 := frame16(libretro.Format0RGB1555, 1, 1, 2, 0x7FFF)
	if got := ToBGRA(white1555, nil); !bytes.Equal(got, []byte{255, 255, 255, 255}) {
		t.Errorf("0RGB1555 white = %v", got)
	}
}

// Each channel lands in its own byte: blue first, as a DIB wants.
func TestToBGRAChannelOrder(t *testing.T) {
	// RGB565 pure red, green, blue.
	f := frame16(libretro.FormatRGB565, 3, 1, 6, 0xF800, 0x07E0, 0x001F)
	want := []byte{0, 0, 255, 255, 0, 255, 0, 255, 255, 0, 0, 255}
	if got := ToBGRA(f, nil); !bytes.Equal(got, want) {
		t.Errorf("RGB565 = %v, want %v", got, want)
	}
	// 0RGB1555: the top bit is ignored, not read as red.
	f = frame16(libretro.Format0RGB1555, 2, 1, 4, 0x7C00, 0x8000)
	want = []byte{0, 0, 255, 255, 0, 0, 0, 255}
	if got := ToBGRA(f, nil); !bytes.Equal(got, want) {
		t.Errorf("0RGB1555 = %v, want %v", got, want)
	}
}

// XRGB8888 is copied row by row, dropping the padding a pitch adds, and the
// unused byte is made opaque.
func TestToBGRADropsPitch(t *testing.T) {
	data := []byte{
		1, 2, 3, 0, 4, 5, 6, 0, 0xEE, 0xEE, 0xEE, 0xEE, // row 0 + 4 bytes of padding
		7, 8, 9, 0, 10, 11, 12, 0, 0xEE, 0xEE, 0xEE, 0xEE,
	}
	f := libretro.Frame{Data: data, Width: 2, Height: 2, Pitch: 12, Format: libretro.FormatXRGB8888}
	want := []byte{1, 2, 3, 255, 4, 5, 6, 255, 7, 8, 9, 255, 10, 11, 12, 255}
	if got := ToBGRA(f, nil); !bytes.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// The destination is reused when it is big enough, so a frame costs no
// allocation once the loop is running.
func TestToBGRAReusesTheBuffer(t *testing.T) {
	buf := make([]byte, 0, 64)
	f := frame16(libretro.FormatRGB565, 2, 2, 4, 1, 2, 3, 4)
	out := ToBGRA(f, buf)
	if &out[0] != &buf[:1][0] {
		t.Error("a buffer large enough was not reused")
	}
}

func TestLayoutIntegerScale(t *testing.T) {
	// GBA 240x160 at 3:2 in a 1920x1080 window: 6x is 1440x960.
	if got := Layout(1920, 1080, 240, 160, 1.5); got != (Rect{X: 240, Y: 60, W: 1440, H: 960}) {
		t.Errorf("GBA = %+v", got)
	}
	// SNES 256x224 shown at 4:3: 4x height is 896, width 1195 — fits.
	got := Layout(1920, 1080, 256, 224, 4.0/3)
	if got.H != 896 || got.W != 1195 || got.Y != 92 {
		t.Errorf("SNES = %+v", got)
	}
	// A wide window that is short: the height decides.
	if got := Layout(3000, 500, 320, 240, 4.0/3); got.H != 480 || got.W != 640 {
		t.Errorf("short window = %+v", got)
	}
}

// When not one whole multiple fits, the picture is fitted rather than cropped.
func TestLayoutFitsWhenSmallerThanTheConsole(t *testing.T) {
	got := Layout(200, 150, 320, 240, 4.0/3)
	if got.W != 200 || got.H != 150 || got.X != 0 || got.Y != 0 {
		t.Errorf("got %+v", got)
	}
	if got := Layout(0, 100, 320, 240, 0); got != (Rect{}) {
		t.Errorf("an empty window placed a picture: %+v", got)
	}
}
