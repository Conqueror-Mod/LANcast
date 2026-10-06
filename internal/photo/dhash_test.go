package photo

import (
	"image"
	"image/color"
	"math"
	"math/bits"
	"testing"

	xdraw "golang.org/x/image/draw"
)

// gradient is brightest at the left edge and darkest at the right.
func gradient(w, h int, falling bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(255 * x / (w - 1))
			if falling {
				v = 255 - v
			}
			img.Set(x, y, color.RGBA{v, v, v, 255})
		}
	}
	return img
}

// scene is smooth structure in both directions, like a photograph at the
// scale the hash sees it: a hash of it has bits of both values. (A pattern that
// repeats every few pixels would alias in the sampled cells; the 8-bit
// threshold was measured on real photos, which do not.)
func scene(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx, fy := float64(x)/float64(w), float64(y)/float64(h)
			v := uint8(128 + 100*math.Sin(fx*9)*math.Cos(fy*5))
			img.Set(x, y, color.RGBA{v, uint8(255 - v/2), uint8(255 * fy), 255})
		}
	}
	return img
}

func TestDHashReadsBrightnessLeftToRight(t *testing.T) {
	if got := DHash(gradient(90, 80, true)); got != ^uint64(0) {
		t.Errorf("falling gradient = %064b, want every bit set", got)
	}
	if got := DHash(gradient(90, 80, false)); got != 0 {
		t.Errorf("rising gradient = %064b, want no bit set", got)
	}
}

// What the near-copy rule relies on: the same picture at another size hashes
// within its 8-bit threshold, and a different picture does not.
func TestDHashSurvivesAResize(t *testing.T) {
	big := scene(1600, 1200)
	small := image.NewRGBA(image.Rect(0, 0, 320, 240))
	xdraw.ApproxBiLinear.Scale(small, small.Bounds(), big, big.Bounds(), xdraw.Src, nil)
	if d := bits.OnesCount64(DHash(big) ^ DHash(small)); d > 8 {
		t.Errorf("resized copy differs by %d bits, want <= 8", d)
	}
	if d := bits.OnesCount64(DHash(big) ^ DHash(gradient(1600, 1200, true))); d <= 8 {
		t.Errorf("a different picture differs by only %d bits", d)
	}
}

func TestDHashOfATinyPictureIsZero(t *testing.T) {
	if got := DHash(image.NewRGBA(image.Rect(0, 0, 5, 5))); got != 0 {
		t.Errorf("DHash of 5x5 = %d, want 0", got)
	}
}
