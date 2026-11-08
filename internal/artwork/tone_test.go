package artwork

import (
	"image"
	"image/color"
	"testing"
)

func flat(c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 64, 36))
	for y := 0; y < 36; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

// Each wisp is reached by the colours it stands for, and only by them.
func TestToneLeansTowardTheMatchingWisp(t *testing.T) {
	cases := []struct {
		name string
		c    color.Color
		want func(Tone) float64
	}{
		{"desert orange is ember", color.RGBA{220, 130, 40, 255}, func(t Tone) float64 { return t.Ember }},
		{"sea green is teal", color.RGBA{30, 160, 140, 255}, func(t Tone) float64 { return t.Teal }},
		{"night blue is blue", color.RGBA{30, 60, 200, 255}, func(t Tone) float64 { return t.Blue }},
		{"purple is violet", color.RGBA{140, 50, 190, 255}, func(t Tone) float64 { return t.Violet }},
	}
	for _, c := range cases {
		got := ToneOf(flat(c.c))
		if c.want(got) != 1 {
			t.Errorf("%s: tone = %+v, want all of the weight on one wisp", c.name, got)
		}
	}
}

// A picture with no colour leaves the field alone: all four weights zero, so
// the client falls back to the field it already has.
func TestToneOfGreyHasNoLean(t *testing.T) {
	got := ToneOf(flat(color.RGBA{120, 120, 120, 255}))
	if got.Violet+got.Blue+got.Teal+got.Ember != 0 {
		t.Errorf("grey tone = %+v, want no lean", got)
	}
	if got.Light < 0.4 || got.Light > 0.55 {
		t.Errorf("grey light = %v, want about 0.47", got.Light)
	}
}

// Half warm, half cool: the weights share the picture, and sum to one.
func TestToneSharesBetweenWisps(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 96, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 96; x++ {
			c := color.RGBA{220, 130, 40, 255}
			if x >= 48 {
				c = color.RGBA{30, 60, 200, 255}
			}
			img.Set(x, y, c)
		}
	}
	got := ToneOf(img)
	if got.Ember < 0.3 || got.Blue < 0.3 {
		t.Errorf("split tone = %+v, want ember and blue both well represented", got)
	}
	if sum := got.Violet + got.Blue + got.Teal + got.Ember; sum < 0.98 || sum > 1.02 {
		t.Errorf("weights sum to %v, want 1", sum)
	}
}
