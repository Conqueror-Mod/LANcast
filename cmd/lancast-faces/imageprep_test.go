package main

import (
	"image"
	"image/color"
	"math"
	"testing"
)

/*
 * Preparing pixels, tested without a model.
 *
 * Every failure guarded against here is silent. A stretched photograph, the
 * wrong channel order, or an unaligned crop all produce a pipeline that runs,
 * returns plausible numbers, and is quietly much worse than it should be —
 * which is indistinguishable from "face recognition is just a bit rubbish".
 */

func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

// A wide photograph is fitted and padded, not stretched. A 16:9 image squashed
// into a square makes every face 44% narrower than any the model saw in
// training.
func TestLetterboxFitsWithoutDistorting(t *testing.T) {
	src := solid(320, 180, color.RGBA{10, 20, 30, 255})
	dst, scale, padX, padY := Letterbox(src, 320)

	if dst.Bounds().Dx() != 320 || dst.Bounds().Dy() != 320 {
		t.Fatalf("output is %v, want 320x320", dst.Bounds())
	}
	if math.Abs(float64(scale)-1) > 1e-6 {
		t.Errorf("scale = %v, want 1 — the width already fits", scale)
	}
	if padX != 0 {
		t.Errorf("padX = %v, want 0 — the width fills the square", padX)
	}
	// 320x180 into 320x320 leaves 140 rows, 70 above and 70 below.
	if padY != 70 {
		t.Errorf("padY = %v, want 70", padY)
	}
}

// A tall photograph is padded on the other axis, and scaled down to fit.
func TestLetterboxScalesDownAndPadsHorizontally(t *testing.T) {
	src := solid(200, 400, color.RGBA{1, 2, 3, 255})
	_, scale, padX, padY := Letterbox(src, 200)
	if math.Abs(float64(scale)-0.5) > 1e-6 {
		t.Errorf("scale = %v, want 0.5", scale)
	}
	// 200x400 at 0.5 is 100x200: 100 columns spare, 50 each side.
	if padX != 50 {
		t.Errorf("padX = %v, want 50", padX)
	}
	if padY != 0 {
		t.Errorf("padY = %v, want 0", padY)
	}
}

/*
 * The letterbox and the un-letterbox are inverses, which is the property that
 * actually matters — they are written in different files and it would be easy
 * for one to drift.
 */
func TestLetterboxAndUnletterboxRoundTrip(t *testing.T) {
	src := solid(640, 360, color.RGBA{})
	_, scale, padX, padY := Letterbox(src, 320)

	// A face at (100, 50) 80x80 in the original photograph.
	original := Detection{X: 100, Y: 50, W: 80, H: 80}
	inModel := Detection{
		X: original.X*scale + padX,
		Y: original.Y*scale + padY,
		W: original.W * scale,
		H: original.H * scale,
	}
	back := Unletterbox(inModel, scale, padX, padY)

	for _, c := range []struct {
		name      string
		got, want float32
	}{
		{"x", back.X, original.X},
		{"y", back.Y, original.Y},
		{"w", back.W, original.W},
		{"h", back.H, original.H},
	} {
		if math.Abs(float64(c.got-c.want)) > 1e-3 {
			t.Errorf("%s came back as %v, want %v", c.name, c.got, c.want)
		}
	}
}

// An empty image is a truncated file, not a crash. The worker's whole job is to
// survive a library that contains one.
func TestLetterboxSurvivesAnEmptyImage(t *testing.T) {
	dst, scale, _, _ := Letterbox(image.NewRGBA(image.Rect(0, 0, 0, 0)), 64)
	if dst.Bounds().Dx() != 64 || scale != 1 {
		t.Errorf("an empty image gave %v at scale %v", dst.Bounds(), scale)
	}
}

/*
 * The channel order, which is the single most common way to build a vision
 * pipeline that runs and is quietly worse than it should be.
 *
 * Both models come from the OpenCV world and expect BGR. A pure red pixel must
 * therefore land in the *third* plane, not the first.
 */
func TestTensorIsBGRNotRGB(t *testing.T) {
	img := solid(2, 2, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	tensor := Tensor(img)
	plane := 4

	if tensor[0] != 0 {
		t.Errorf("blue plane holds %v for a red pixel; the order looks like RGB", tensor[0])
	}
	if tensor[2*plane] != 255 {
		t.Errorf("red plane holds %v, want 255 in the third plane", tensor[2*plane])
	}
}

func TestTensorIsChannelsThenPixels(t *testing.T) {
	img := solid(4, 3, color.RGBA{1, 2, 3, 255})
	if got, want := len(Tensor(img)), 3*4*3; got != want {
		t.Errorf("tensor length %d, want %d", got, want)
	}
}

/*
 * Alignment rotates a tilted face upright.
 *
 * A head tilted thirty degrees is not unusual — it is most holiday
 * photographs — and an unaligned crop costs accuracy in the way hardest to
 * notice: the embeddings are still valid vectors that still cluster into
 * something, and are simply less able to tell two people apart.
 *
 * Asserted through what the transform does to a known pixel rather than by
 * eyeballing an image: a marker placed on the eye line must land on the
 * horizontal centre line of the crop once the rotation is undone.
 */
func TestAlignedCropUndoesRotation(t *testing.T) {
	// A 200x200 canvas, dark, with a bright marker at the right eye position of
	// a face tilted 45 degrees.
	src := solid(200, 200, color.RGBA{0, 0, 0, 255})
	// Face centred at (100,100), 60 across. Eyes on a 45-degree line.
	d := Detection{X: 70, Y: 70, W: 60, H: 60, Score: 0.9}
	d.Landmarks[0] = [2]float32{85, 85}   // right eye, up-left
	d.Landmarks[1] = [2]float32{115, 115} // left eye, down-right → 45 degrees

	// Mark the right eye so it can be found in the output.
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			src.Set(85+dx, 85+dy, color.RGBA{255, 255, 255, 255})
		}
	}

	out := AlignedCrop(src, d, 112)

	// Find the marker's centre of mass in the crop.
	var sx, sy, n float64
	for y := 0; y < 112; y++ {
		for x := 0; x < 112; x++ {
			r, _, _, _ := out.At(x, y).RGBA()
			if r > 0x8000 {
				sx += float64(x)
				sy += float64(y)
				n++
			}
		}
	}
	if n == 0 {
		t.Fatal("the marker did not appear in the aligned crop at all")
	}
	cy := sy / n
	// Undoing a 45-degree tilt puts both eyes on the same horizontal line,
	// through the middle of the crop.
	if math.Abs(cy-56) > 12 {
		t.Errorf("the eye marker sits at y=%.1f in the crop; want it near the "+
			"centre line (56) once the tilt is undone", cy)
	}
	// And it should be left of centre, since it is the right eye.
	if sx/n >= 56 {
		t.Errorf("the right eye landed at x=%.1f, on the wrong side of centre", sx/n)
	}
}

// A degenerate box produces an empty crop rather than dividing by zero.
func TestAlignedCropSurvivesAZeroSizedFace(t *testing.T) {
	src := solid(50, 50, color.RGBA{0, 0, 0, 255})
	out := AlignedCrop(src, Detection{}, 112)
	if out.Bounds().Dx() != 112 {
		t.Errorf("got %v, want a 112x112 crop", out.Bounds())
	}
}
