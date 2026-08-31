package main

import (
	"math"
	"sort"
)

/*
 * Turning YuNet's raw output into faces (ADR 0052).
 *
 * Deliberately pure: these functions take float32 slices and return boxes, with
 * no reference to ONNX, cgo, or a file on disk. That is not tidiness — it is
 * the only way this code can be tested at all. The inference itself needs a C
 * toolchain and a 38MB model, and neither exists on a developer's machine by
 * default; the decode is where the bugs actually live, and it can be exercised
 * on a laptop in milliseconds with numbers somebody wrote by hand.
 *
 * A detector head is identified by the *shape* of its tensor rather than by the
 * name of its output, because names are a property of whoever exported the
 * model and shapes are a property of what it computes. YuNet emits three
 * strides (8, 16, 32), each with a 1-channel objectness, a 1-channel
 * classification, a 4-channel box and a 10-channel set of five landmarks.
 */

// Detection is one candidate face in the coordinates of the tensor grid's
// source image — that is, the letterboxed input, not the original photograph.
type Detection struct {
	X, Y, W, H float32
	Score      float32
	// Landmarks are five points: right eye, left eye, nose, right mouth
	// corner, left mouth corner, in YuNet's order. The embedder needs them to
	// align the crop, and an unaligned crop is what makes recognition quietly
	// bad rather than obviously broken.
	Landmarks [5][2]float32
}

/*
 * DecodeStride turns one stride's tensors into candidates.
 *
 * `cls` and `obj` are the two confidences YuNet produces, and the score is
 * their product — that is what OpenCV's own postprocessing does, and using
 * either alone lets through faces the model was not actually confident about.
 *
 * Coordinates come back in input-image pixels rather than grid units, because
 * every consumer wants pixels and converting once here is one place to be wrong
 * instead of three.
 */
func DecodeStride(stride, gridW, gridH int, cls, obj, box, kps []float32, minScore float32) []Detection {
	out := []Detection{}
	n := gridW * gridH
	if len(cls) < n || len(obj) < n || len(box) < 4*n || len(kps) < 10*n {
		// A short tensor is a model whose shape is not what this code believes.
		// Returning nothing is honest; indexing into it would be a crash in a
		// worker that is supposed to survive a bad photograph.
		return out
	}
	for i := 0; i < n; i++ {
		score := cls[i] * obj[i]
		if score < minScore {
			continue
		}
		col := float32(i % gridW)
		row := float32(i / gridW)
		s := float32(stride)

		// YuNet predicts a centre offset within the cell and a log-space size.
		cx := (col + box[4*i+0]) * s
		cy := (row + box[4*i+1]) * s
		w := expF(box[4*i+2]) * s
		h := expF(box[4*i+3]) * s

		d := Detection{X: cx - w/2, Y: cy - h/2, W: w, H: h, Score: score}
		for k := 0; k < 5; k++ {
			d.Landmarks[k][0] = (col + kps[10*i+2*k+0]) * s
			d.Landmarks[k][1] = (row + kps[10*i+2*k+1]) * s
		}
		out = append(out, d)
	}
	return out
}

// expF is math.Exp for float32 without the conversion noise at every call site.
func expF(x float32) float32 {
	// Guard the range: a corrupt tensor should not produce +Inf sizes that then
	// propagate into every downstream comparison as NaN.
	if x > 20 {
		x = 20
	}
	if x < -20 {
		x = -20
	}
	return float32(math.Exp(float64(x)))
}

/*
 * NMS keeps the best of each overlapping group.
 *
 * A detector fires several times on one face — that is normal and not a fault —
 * and without this a photograph of two people yields eleven "faces", each of
 * which would be embedded, stored, and clustered as though it were a separate
 * person. The threshold is 0.3, which is what OpenCV uses for YuNet.
 */
func NMS(in []Detection, iouThreshold float32) []Detection {
	if len(in) == 0 {
		return in
	}
	d := make([]Detection, len(in))
	copy(d, in)
	sort.SliceStable(d, func(i, j int) bool { return d[i].Score > d[j].Score })

	kept := make([]Detection, 0, len(d))
	suppressed := make([]bool, len(d))
	for i := range d {
		if suppressed[i] {
			continue
		}
		kept = append(kept, d[i])
		for j := i + 1; j < len(d); j++ {
			if !suppressed[j] && iou(d[i], d[j]) > iouThreshold {
				suppressed[j] = true
			}
		}
	}
	return kept
}

func iou(a, b Detection) float32 {
	x1 := maxF(a.X, b.X)
	y1 := maxF(a.Y, b.Y)
	x2 := minF(a.X+a.W, b.X+b.W)
	y2 := minF(a.Y+a.H, b.Y+b.H)
	iw, ih := x2-x1, y2-y1
	if iw <= 0 || ih <= 0 {
		return 0
	}
	inter := iw * ih
	union := a.W*a.H + b.W*b.H - inter
	if union <= 0 {
		return 0
	}
	return inter / union
}

/*
 * Unletterbox maps a detection back onto the original photograph.
 *
 * The model is fed a fixed-size image with the picture scaled to fit and the
 * remainder padded, so every coordinate it returns is in that padded space. A
 * pipeline that forgets this produces boxes that are subtly wrong on every
 * non-square photograph — offset by the padding and scaled by the fit — which
 * looks like a mediocre detector rather than like a bug.
 */
func Unletterbox(d Detection, scale, padX, padY float32) Detection {
	out := d
	out.X = (d.X - padX) / scale
	out.Y = (d.Y - padY) / scale
	out.W = d.W / scale
	out.H = d.H / scale
	for k := 0; k < 5; k++ {
		out.Landmarks[k][0] = (d.Landmarks[k][0] - padX) / scale
		out.Landmarks[k][1] = (d.Landmarks[k][1] - padY) / scale
	}
	return out
}

func maxF(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func minF(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
