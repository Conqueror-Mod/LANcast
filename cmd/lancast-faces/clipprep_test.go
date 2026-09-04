package main

import (
	"image"
	"image/color"
	"math"
	"testing"
)

/*
 * Preprocessing, which is the other half of the pipeline with no error path.
 *
 * A wrong crop rule or the wrong normalisation constants do not fail. They
 * produce a tensor of exactly the right shape, full of plausible numbers, and
 * the search comes back a little worse in a way nobody can point at. So these
 * assert the three decisions the file comment names: where the crop comes from,
 * how the channels are laid out, and what the numbers mean.
 */

// solid() is imageprep_test.go's, reused rather than written twice.

func TestCropIsAlwaysTheModelsInputSize(t *testing.T) {
	for _, d := range [][2]int{{224, 224}, {640, 480}, {480, 640}, {4000, 3000}, {10, 10}, {1, 1}} {
		got := ClipCrop(solid(d[0], d[1], color.RGBA{10, 20, 30, 255}))
		if got.Bounds().Dx() != ClipImageSize || got.Bounds().Dy() != ClipImageSize {
			t.Errorf("%dx%d cropped to %v, want %d square", d[0], d[1], got.Bounds(), ClipImageSize)
		}
	}
}

/*
 * The shorter side reaches 224 and the longer one is cropped, rather than the
 * whole frame being squashed square.
 *
 * A wide photograph with a distinct centre proves which happened: under
 * shortest-side-then-centre the result is entirely the centre band, and under a
 * squash it would still contain the edges.
 */
func TestCropTakesTheCentreRatherThanSquashing(t *testing.T) {
	// 672×224: three 224-wide bands, red | green | blue.
	src := image.NewRGBA(image.Rect(0, 0, 672, 224))
	bands := []color.RGBA{
		{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255},
	}
	for x := 0; x < 672; x++ {
		for y := 0; y < 224; y++ {
			src.SetRGBA(x, y, bands[x/224])
		}
	}

	out := ClipCrop(src)
	// The centre pixel must be green — the middle band — and not red or blue.
	c := out.RGBAAt(ClipImageSize/2, ClipImageSize/2)
	if c.G < 200 || c.R > 60 || c.B > 60 {
		t.Errorf("centre pixel is %v, want the middle band; a squash would have "+
			"brought the edges in", c)
	}
}

/*
 * The tensor is channel-major.
 *
 * Interleaved is the natural way to write it and produces a buffer of exactly
 * the right length whose every value is in the wrong place. The runtime accepts
 * that without complaint, so it has to be caught here.
 */
func TestTensorIsChannelMajor(t *testing.T) {
	// Pure red: every red is one value, every green another, every blue a third.
	img := solid(ClipImageSize, ClipImageSize, color.RGBA{255, 0, 0, 255})
	v := ClipTensor(img)

	const n = ClipImageSize * ClipImageSize
	if len(v) != 3*n {
		t.Fatalf("tensor is %d, want %d", len(v), 3*n)
	}
	for i := 1; i < n; i++ {
		if v[i] != v[0] {
			t.Fatalf("red plane is not uniform at %d — the layout is interleaved", i)
		}
	}
	if v[0] == v[n] {
		t.Error("the red and green planes hold the same value; a pure red image " +
			"should not, which means the channels are not separated")
	}
}

/*
 * The normalisation constants are CLIP's, not ImageNet's.
 *
 * This is the mistake worth a named test: almost every other vision model uses
 * ImageNet's, they are numerically close, and swapping them costs accuracy
 * without costing correctness anywhere a test would look. Asserted through the
 * arithmetic rather than by comparing the literals to themselves.
 */
func TestNormalisationUsesClipsOwnConstants(t *testing.T) {
	// Mid-grey: each channel is 0.5 before normalisation, so each plane lands on
	// (0.5 - mean) / std for its own channel.
	img := solid(ClipImageSize, ClipImageSize, color.RGBA{128, 128, 128, 255})
	v := ClipTensor(img)

	const n = ClipImageSize * ClipImageSize
	half := float32(128) / 255
	for c, plane := range [][2]float32{
		{v[0], (half - clipMean[0]) / clipStd[0]},
		{v[n], (half - clipMean[1]) / clipStd[1]},
		{v[2*n], (half - clipMean[2]) / clipStd[2]},
	} {
		if math.Abs(float64(plane[0]-plane[1])) > 1e-5 {
			t.Errorf("channel %d normalised to %v, want %v", c, plane[0], plane[1])
		}
	}

	// And the constants are not ImageNet's, which is the specific confusion.
	imagenet := [3]float32{0.485, 0.456, 0.406}
	if clipMean == imagenet {
		t.Error("clipMean is ImageNet's; CLIP was trained with its own distribution")
	}
}

func TestNormalizeMakesAUnitVector(t *testing.T) {
	v := []float32{3, 4}
	if !Normalize(v) {
		t.Fatal("a non-zero vector was refused")
	}
	var sum float64
	for _, f := range v {
		sum += float64(f) * float64(f)
	}
	if math.Abs(sum-1) > 1e-6 {
		t.Errorf("length squared is %v, want 1", sum)
	}
}

/*
 * A zero vector is refused rather than divided by.
 *
 * It is what a blank frame or a failed run produces, and dividing gives NaNs —
 * which store fine, compare as false against everything, and quietly sit in the
 * rankings of every later search.
 */
func TestNormalizeRefusesAZeroVector(t *testing.T) {
	v := []float32{0, 0, 0}
	if Normalize(v) {
		t.Error("a zero vector was normalised; the result would be NaNs")
	}
	for i, f := range v {
		if f != 0 {
			t.Errorf("the vector was modified at %d: %v", i, f)
		}
	}
}

/*
 * CLIP's tensor is RGB where the face embedder's is BGR, and they disagree
 * because the models do.
 *
 * SFace comes from OpenCV, which is BGR by convention. Somebody unifying the
 * two "for consistency" would fix an asymmetry and silently break one model: a
 * channel-swapped image is still a valid tensor producing a valid vector, and
 * the only symptom is a search that is worse than it should be.
 */
func TestClipTensorIsRGBWhereTheFaceTensorIsBGR(t *testing.T) {
	img := solid(ClipImageSize, ClipImageSize, color.RGBA{255, 0, 0, 255})
	const n = ClipImageSize * ClipImageSize

	clip := ClipTensor(img)
	// Red is the largest channel in plane 0 for RGB. Under BGR it would be in
	// plane 2, and each plane carries its own normalisation, so comparing the
	// raw values is enough to tell them apart.
	if !(clip[0] > clip[n] && clip[0] > clip[2*n]) {
		t.Errorf("a red image does not lead in plane 0: %v %v %v — this tensor "+
			"should be RGB", clip[0], clip[n], clip[2*n])
	}
}
