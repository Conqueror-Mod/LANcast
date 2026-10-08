package libretro

import "testing"

func TestParseVariable(t *testing.T) {
	v := ParseVariable("mupen64plus-43screensize", "Resolution; 640x480|960x720|1280x960")
	if v.Description != "Resolution" || len(v.Values) != 3 || v.Default() != "640x480" {
		t.Errorf("got %+v", v)
	}
	// A value with no separator is a description with no choices, and has no
	// default rather than a made-up one.
	v = ParseVariable("k", "Just words")
	if v.Description != "Just words" || v.Default() != "" {
		t.Errorf("got %+v", v)
	}
	// Empty alternatives are dropped; a value may itself contain spaces.
	v = ParseVariable("k", "Mode;  on||off (slow)")
	if len(v.Values) != 2 || v.Values[1] != "off (slow)" {
		t.Errorf("got %+v", v)
	}
}

func TestDisplayAspect(t *testing.T) {
	if a := (AVInfo{BaseWidth: 256, BaseHeight: 224, AspectRatio: 4.0 / 3}).DisplayAspect(); a < 1.333 || a > 1.334 {
		t.Errorf("declared aspect = %v", a)
	}
	// Zero means width over height — GBA's 3:2 is not 4:3.
	if a := (AVInfo{BaseWidth: 240, BaseHeight: 160}).DisplayAspect(); a != 1.5 {
		t.Errorf("implied aspect = %v", a)
	}
	if a := (AVInfo{}).DisplayAspect(); a < 1.33 || a > 1.34 {
		t.Errorf("no geometry at all = %v, want a 4:3 fallback", a)
	}
}

func TestSupportedFormats(t *testing.T) {
	for _, f := range []PixelFormat{Format0RGB1555, FormatXRGB8888, FormatRGB565} {
		if !f.Supported() {
			t.Errorf("%d unsupported", f)
		}
	}
	if PixelFormat(3).Supported() || PixelFormat(4).Supported() {
		t.Error("a 10-bit format was accepted; the core should be told no and fall back")
	}
}
