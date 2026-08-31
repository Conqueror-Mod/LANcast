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
 * Alignment puts the landmarks where the embedder expects them.
 *
 * This is the assertion that matters, and the earlier version of it was much
 * weaker — it checked only that a tilted face came out level, which the
 * two-eye transform did while still producing crops the model had never seen.
 * Measured on a photograph of nine strangers, half of all pairs came back above
 * SFace's published same-person threshold. The transform was "working" and the
 * constant the clustering depends on was meaningless.
 *
 * So: a face whose landmarks are placed at known positions must come out with
 * those landmarks *on the canonical positions*. That is the property the
 * threshold is a property of.
 */
func TestAlignedCropPutsLandmarksOnTheCanonicalPositions(t *testing.T) {
	src := solid(400, 400, color.RGBA{0, 0, 0, 255})

	// A face rotated 30 degrees and scaled, with its five landmarks placed by
	// transforming the canonical set — so the correct answer is known exactly.
	const angle = 30 * math.Pi / 180
	const scale = 2.5
	const offX, offY = 120.0, 90.0
	var d Detection
	for i := 0; i < 5; i++ {
		cx := canonicalFace[i][0] - 56
		cy := canonicalFace[i][1] - 56
		x := (cx*math.Cos(angle) - cy*math.Sin(angle)) * scale
		y := (cx*math.Sin(angle) + cy*math.Cos(angle)) * scale
		d.Landmarks[i] = [2]float32{float32(x + offX), float32(y + offY)}
	}
	d.X, d.Y, d.W, d.H = 0, 0, 200, 200

	// Mark the nose — the middle landmark — so it can be found in the output.
	nx, ny := int(d.Landmarks[2][0]), int(d.Landmarks[2][1])
	for dy := -3; dy <= 3; dy++ {
		for dx := -3; dx <= 3; dx++ {
			src.Set(nx+dx, ny+dy, color.RGBA{255, 255, 255, 255})
		}
	}

	out := AlignedCrop(src, d, 112)

	var sx, sy, n float64
	for y := 0; y < 112; y++ {
		for x := 0; x < 112; x++ {
			if r, _, _, _ := out.At(x, y).RGBA(); r > 0x8000 {
				sx += float64(x)
				sy += float64(y)
				n++
			}
		}
	}
	if n == 0 {
		t.Fatal("the nose marker does not appear in the aligned crop at all")
	}
	gotX, gotY := sx/n, sy/n
	wantX, wantY := canonicalFace[2][0], canonicalFace[2][1]
	if math.Abs(gotX-wantX) > 3 || math.Abs(gotY-wantY) > 3 {
		t.Errorf("the nose landed at (%.1f, %.1f); the embedder expects it at "+
			"(%.1f, %.1f)", gotX, gotY, wantX, wantY)
	}
}

/*
 * And the transform is a *similarity* — it may rotate, scale and move a face,
 * and it may not mirror one.
 *
 * Reflection would fit some landmark sets better than the true pose, and a
 * mirrored face is not a better answer to "who is this" — it is a different
 * question. The closed form used cannot express a reflection, and this pins
 * that: the right eye must stay left of the left eye in the output.
 */
func TestAlignedCropDoesNotMirror(t *testing.T) {
	src := solid(400, 400, color.RGBA{0, 0, 0, 255})
	var d Detection
	for i := 0; i < 5; i++ {
		d.Landmarks[i] = [2]float32{
			float32(canonicalFace[i][0] + 100),
			float32(canonicalFace[i][1] + 100),
		}
	}
	d.X, d.Y, d.W, d.H = 100, 100, 112, 112

	// Mark the right eye only.
	ex, ey := int(d.Landmarks[0][0]), int(d.Landmarks[0][1])
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			src.Set(ex+dx, ey+dy, color.RGBA{255, 255, 255, 255})
		}
	}

	out := AlignedCrop(src, d, 112)
	var sx, n float64
	for y := 0; y < 112; y++ {
		for x := 0; x < 112; x++ {
			if r, _, _, _ := out.At(x, y).RGBA(); r > 0x8000 {
				sx += float64(x)
				n++
			}
		}
	}
	if n == 0 {
		t.Fatal("the eye marker vanished")
	}
	if got := sx / n; math.Abs(got-canonicalFace[0][0]) > 3 {
		t.Errorf("the right eye landed at x=%.1f, want %.1f — a mirrored fit "+
			"would put it near %.1f", got, canonicalFace[0][0], canonicalFace[1][0])
	}
}

// Degenerate landmarks — every point in the same place — produce an empty crop
// rather than a division by zero.
func TestAlignedCropSurvivesDegenerateLandmarks(t *testing.T) {
	src := solid(50, 50, color.RGBA{0, 0, 0, 255})
	out := AlignedCrop(src, Detection{}, 112)
	if out.Bounds().Dx() != 112 {
		t.Errorf("got %v, want a 112x112 crop", out.Bounds())
	}
}
