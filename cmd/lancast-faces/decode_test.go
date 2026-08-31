package main

import (
	"math"
	"testing"
)

/*
 * The decode, tested with numbers written by hand.
 *
 * This is the part of face detection where bugs actually live, and it is also
 * the part that can be exercised without a C toolchain or a 38MB model —
 * neither of which exists on a developer's machine by default, and neither of
 * which this file needs. Every function here takes float32 slices.
 *
 * The failures being guarded against are all quiet ones. A wrong stride, a
 * forgotten letterbox offset or a missing NMS pass does not crash: it produces
 * boxes that are plausibly placed and slightly wrong, which reads as a mediocre
 * detector rather than as a defect, and then poisons every embedding taken from
 * those crops.
 */

// grid builds the four tensors for a stride whose cells are all empty except
// one, which holds a face at the given centre offset and log-size.
func grid(gw, gh, cell int, cls, obj, dx, dy, lw, lh float32) (c, o, b, k []float32) {
	n := gw * gh
	c = make([]float32, n)
	o = make([]float32, n)
	b = make([]float32, 4*n)
	k = make([]float32, 10*n)
	c[cell], o[cell] = cls, obj
	b[4*cell+0], b[4*cell+1] = dx, dy
	b[4*cell+2], b[4*cell+3] = lw, lh
	return
}

func TestDecodePlacesABoxInPixels(t *testing.T) {
	// A 4x4 grid at stride 8 covers a 32x32 image. Cell 5 is column 1, row 1.
	// Centre offset 0.5 puts the face at ((1+0.5)*8, (1+0.5)*8) = (12, 12), and
	// a log-size of 0 gives exactly one stride of width: 8.
	c, o, b, k := grid(4, 4, 5, 1.0, 1.0, 0.5, 0.5, 0, 0)
	got := DecodeStride(8, 4, 4, c, o, b, k, 0.5)
	if len(got) != 1 {
		t.Fatalf("decoded %d faces, want 1", len(got))
	}
	d := got[0]
	// Centre 12, size 8 → top-left at 8.
	if math.Abs(float64(d.X)-8) > 1e-4 || math.Abs(float64(d.Y)-8) > 1e-4 {
		t.Errorf("top-left = (%v, %v), want (8, 8)", d.X, d.Y)
	}
	if math.Abs(float64(d.W)-8) > 1e-4 || math.Abs(float64(d.H)-8) > 1e-4 {
		t.Errorf("size = (%v, %v), want (8, 8)", d.W, d.H)
	}
}

// The score is the product of the two confidences, which is what OpenCV's own
// postprocessing does. Using either alone lets through faces the model was not
// actually confident about.
func TestScoreIsTheProductOfBothConfidences(t *testing.T) {
	c, o, b, k := grid(4, 4, 0, 0.8, 0.5, 0, 0, 0, 0)
	got := DecodeStride(8, 4, 4, c, o, b, k, 0.1)
	if len(got) != 1 {
		t.Fatalf("decoded %d, want 1", len(got))
	}
	if math.Abs(float64(got[0].Score)-0.4) > 1e-6 {
		t.Errorf("score = %v, want 0.4", got[0].Score)
	}
	// And a cell that clears one confidence but not their product is dropped.
	if out := DecodeStride(8, 4, 4, c, o, b, k, 0.6); len(out) != 0 {
		t.Errorf("a cell scoring 0.4 survived a 0.6 threshold")
	}
}

// The stride is what turns grid cells into pixels, and getting it wrong is the
// classic quiet failure: every box lands at a plausible-looking fraction or
// multiple of where it belongs.
func TestStrideScalesCoordinates(t *testing.T) {
	c, o, b, k := grid(2, 2, 3, 1, 1, 0, 0, 0, 0)
	at8 := DecodeStride(8, 2, 2, c, o, b, k, 0.5)[0]
	at32 := DecodeStride(32, 2, 2, c, o, b, k, 0.5)[0]
	if at32.X != at8.X*4 || at32.W != at8.W*4 {
		t.Errorf("stride 32 gave (%v,%v) against stride 8's (%v,%v); want 4x",
			at32.X, at32.W, at8.X, at8.W)
	}
}

// Landmarks come back in pixels too. An unaligned crop is what makes
// recognition quietly bad rather than obviously broken, so the points the
// aligner depends on have to be in the same space as the box.
func TestLandmarksAreDecodedIntoPixels(t *testing.T) {
	c, o, b, k := grid(4, 4, 5, 1, 1, 0, 0, 0, 0)
	// Right eye a quarter of a cell right and down of the cell origin.
	k[10*5+0], k[10*5+1] = 0.25, 0.25
	d := DecodeStride(8, 4, 4, c, o, b, k, 0.5)[0]
	// Cell 5 is (1,1); (1+0.25)*8 = 10.
	if math.Abs(float64(d.Landmarks[0][0])-10) > 1e-4 {
		t.Errorf("landmark x = %v, want 10", d.Landmarks[0][0])
	}
}

// A tensor shorter than the grid claims is a model whose shape is not what this
// code believes. Returning nothing beats indexing past the end in a worker
// whose whole job is to survive a bad photograph.
func TestAShortTensorDecodesToNothingRatherThanCrashing(t *testing.T) {
	got := DecodeStride(8, 4, 4, make([]float32, 2), make([]float32, 2),
		make([]float32, 2), make([]float32, 2), 0.5)
	if len(got) != 0 {
		t.Errorf("decoded %d faces from a truncated tensor", len(got))
	}
}

/*
 * NMS, and the reason it is not optional.
 *
 * A detector fires several times on one face. Without this, a photograph of two
 * people yields eleven "faces", each embedded, stored and clustered as though
 * it were a separate person — so one photograph would invent nine people.
 */
func TestNMSKeepsTheBestOfAnOverlappingGroup(t *testing.T) {
	in := []Detection{
		{X: 10, Y: 10, W: 20, H: 20, Score: 0.7},
		{X: 11, Y: 11, W: 20, H: 20, Score: 0.9}, // same face, better score
		{X: 12, Y: 9, W: 20, H: 20, Score: 0.6},  // same face again
		{X: 100, Y: 100, W: 20, H: 20, Score: 0.8},
	}
	got := NMS(in, 0.3)
	if len(got) != 2 {
		t.Fatalf("kept %d, want 2 — one per face", len(got))
	}
	if got[0].Score != 0.9 {
		t.Errorf("kept the %v box; the best of a group is the one to keep", got[0].Score)
	}
}

// Boxes that do not overlap are all kept: suppression must be about overlap
// rather than about count.
func TestNMSKeepsSeparateFaces(t *testing.T) {
	in := []Detection{
		{X: 0, Y: 0, W: 10, H: 10, Score: 0.9},
		{X: 50, Y: 50, W: 10, H: 10, Score: 0.8},
		{X: 200, Y: 5, W: 10, H: 10, Score: 0.7},
	}
	if got := NMS(in, 0.3); len(got) != 3 {
		t.Errorf("kept %d of 3 separate faces", len(got))
	}
}

func TestNMSOnNothing(t *testing.T) {
	if got := NMS(nil, 0.3); len(got) != 0 {
		t.Errorf("got %d from no detections", len(got))
	}
}

func TestIOU(t *testing.T) {
	a := Detection{X: 0, Y: 0, W: 10, H: 10}
	if got := iou(a, a); math.Abs(float64(got)-1) > 1e-6 {
		t.Errorf("a box against itself = %v, want 1", got)
	}
	if got := iou(a, Detection{X: 100, Y: 100, W: 10, H: 10}); got != 0 {
		t.Errorf("disjoint boxes = %v, want 0", got)
	}
	// Half-overlap: intersection 50, union 150.
	if got := iou(a, Detection{X: 5, Y: 0, W: 10, H: 10}); math.Abs(float64(got)-1.0/3.0) > 1e-6 {
		t.Errorf("half-overlap = %v, want 1/3", got)
	}
}

/*
 * The letterbox round trip.
 *
 * The model is fed a fixed-size image with the photograph scaled to fit and the
 * rest padded, so every coordinate it returns is in that padded space. Forget
 * it and boxes are offset by the padding and scaled by the fit on every
 * non-square photograph — which is most of them.
 */
func TestUnletterboxUndoesTheFit(t *testing.T) {
	// A face at (100,100) 50x50 in the original, seen through a 0.5 scale with
	// 20px of horizontal padding, appears at (70, 50) 25x25.
	inModelSpace := Detection{X: 70, Y: 50, W: 25, H: 25}
	inModelSpace.Landmarks[0] = [2]float32{75, 55}

	got := Unletterbox(inModelSpace, 0.5, 20, 0)
	if math.Abs(float64(got.X)-100) > 1e-4 || math.Abs(float64(got.Y)-100) > 1e-4 {
		t.Errorf("top-left = (%v, %v), want (100, 100)", got.X, got.Y)
	}
	if math.Abs(float64(got.W)-50) > 1e-4 {
		t.Errorf("width = %v, want 50", got.W)
	}
	if math.Abs(float64(got.Landmarks[0][0])-110) > 1e-4 {
		t.Errorf("landmark travelled to %v, want 110", got.Landmarks[0][0])
	}
}

// With no scaling and no padding it is the identity, which is the case a square
// photograph takes and the one most likely to hide an error in the general one.
func TestUnletterboxIsIdentityWhenNothingWasDone(t *testing.T) {
	in := Detection{X: 3, Y: 4, W: 5, H: 6, Score: 0.9}
	got := Unletterbox(in, 1, 0, 0)
	if got.X != in.X || got.Y != in.Y || got.W != in.W || got.H != in.H {
		t.Errorf("identity transform changed %+v into %+v", in, got)
	}
}
