package host

import "testing"

// The zero value is how the picture always looked.
func TestTheDefaultDisplayIsTheOldLayout(t *testing.T) {
	if LayoutFor(1920, 1080, 256, 224, 4.0/3, Display{}) != Layout(1920, 1080, 256, 224, 4.0/3) {
		t.Error("an untouched menu changed the picture")
	}
}

func TestDisplayLayouts(t *testing.T) {
	// Integer scaling leaves 1080 at 4×224 = 896 tall; filling uses it all.
	if got := LayoutFor(1920, 1080, 256, 224, 4.0/3, Display{}); got.H != 896 {
		t.Errorf("integer: %+v", got)
	}
	if got := LayoutFor(1920, 1080, 256, 224, 4.0/3, Display{Fill: true}); got.H != 1080 || got.W != 1440 {
		t.Errorf("fill: %+v", got)
	}
	if got := LayoutFor(1920, 1080, 256, 224, 4.0/3, Display{Aspect: "stretch"}); got != (Rect{W: 1920, H: 1080}) {
		t.Errorf("stretch: %+v", got)
	}
}

func TestAspectChoices(t *testing.T) {
	core := 4.0 / 3
	if a := (Display{}).aspectFor(core, 256, 224); a != core {
		t.Errorf("core: %v", a)
	}
	if a := (Display{Aspect: "square"}).aspectFor(core, 256, 224); a != 256.0/224 {
		t.Errorf("square: %v", a)
	}
	if a := (Display{Aspect: "4:3"}).aspectFor(1.5, 240, 160); a != 4.0/3 {
		t.Errorf("4:3: %v", a)
	}
	if (Display{Aspect: "16:9"}).Valid() == nil || (Display{Aspect: "square"}).Valid() != nil {
		t.Error("Valid accepts the wrong aspects")
	}
}

// Every source row is followed by a darker copy of itself.
func TestScanlinesDoubleTheRowsAndDimEveryOther(t *testing.T) {
	src := []byte{200, 100, 50, 255, 10, 20, 30, 255} // 2×1
	out := Scanlines(src, 2, 1, nil)
	if len(out) != 16 {
		t.Fatalf("len %d, want two rows of two pixels", len(out))
	}
	if out[0] != 200 || out[4] != 10 {
		t.Errorf("the lit row is not the source: %v", out[:8])
	}
	if out[8] != byte(200*scanlineLevel>>8) || out[8] >= out[0] || out[11] != 0xff {
		t.Errorf("the dim row is not darker: %v", out[8:])
	}
}
