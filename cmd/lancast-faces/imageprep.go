package main

import (
	"image"
	"image/color"
	"math"

	"golang.org/x/image/draw"
)

/*
 * Getting a photograph into the shape a model expects, and a face out of it
 * into the shape the embedder expects (ADR 0052).
 *
 * Pure Go, and independent of ONNX: a photograph becomes a float32 tensor and a
 * face becomes a 112x112 aligned crop, both testable on any machine. The only
 * thing that needs a C toolchain is handing the tensor to the runtime.
 */

// Letterbox scales an image to fit a square of `size` without distorting it,
// and reports what it did so detections can be mapped back.
//
// Fit-and-pad rather than stretch, because a stretched face is a face the model
// has not been trained on: a 16:9 photograph squashed to a square makes every
// face 44% narrower than any it saw in training, and the detector's confidence
// falls off a cliff for a reason nothing reports.
func Letterbox(src image.Image, size int) (dst *image.RGBA, scale, padX, padY float32) {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 {
		return image.NewRGBA(image.Rect(0, 0, size, size)), 1, 0, 0
	}

	s := math.Min(float64(size)/float64(sw), float64(size)/float64(sh))
	w := int(math.Round(float64(sw) * s))
	h := int(math.Round(float64(sh) * s))
	px := (size - w) / 2
	py := (size - h) / 2

	dst = image.NewRGBA(image.Rect(0, 0, size, size))
	/*
	 * The padding is black, and stated rather than left to the zero value —
	 * an RGBA zero value is transparent black, and a model reading the alpha
	 * channel or a later crop compositing over white would both see something
	 * other than what was intended.
	 */
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.RGBA{0, 0, 0, 255}),
		image.Point{}, draw.Src)
	// CatmullRom rather than nearest-neighbour: this is a downscale in almost
	// every case, and aliasing a face into a model is throwing away the detail
	// the model exists to read.
	draw.CatmullRom.Scale(dst, image.Rect(px, py, px+w, py+h), src, b, draw.Over, nil)

	return dst, float32(s), float32(px), float32(py)
}

/*
 * Tensor turns an image into the CHW float32 buffer ONNX wants.
 *
 * BGR, because that is what YuNet and SFace were trained on — both come from
 * the OpenCV world, where BGR is the default channel order. Feeding RGB is the
 * single most common way to make a vision pipeline that runs, produces
 * plausible numbers, and is quietly much worse than it should be.
 *
 * No normalisation: neither of these two models expects it. That is a property
 * of the model rather than of ONNX, so it is stated here where it can be
 * changed when the model is.
 */
func Tensor(img *image.RGBA) []float32 {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]float32, 3*w*h)
	plane := w * h
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := img.PixOffset(b.Min.X+x, b.Min.Y+y)
			p := y*w + x
			out[0*plane+p] = float32(img.Pix[i+2]) // B
			out[1*plane+p] = float32(img.Pix[i+1]) // G
			out[2*plane+p] = float32(img.Pix[i+0]) // R
		}
	}
	return out
}

/*
 * The canonical face, and why it is these five numbers.
 *
 * SFace — like every ArcFace-lineage embedder — is trained on 112x112 crops in
 * which the five landmarks sit at fixed positions. These are those positions.
 * They are not a style choice: the embedder has never seen a face anywhere else
 * on the canvas, and handing it one is asking a question outside its
 * experience.
 *
 * Order matches the detector's: right eye, left eye, nose, right mouth corner,
 * left mouth corner.
 */
var canonicalFace = [5][2]float64{
	{38.2946, 51.6963},
	{73.5318, 51.5014},
	{56.0252, 71.7366},
	{41.5493, 92.3655},
	{70.7299, 92.2041},
}

/*
 * AlignedCrop warps a face onto the canonical positions above.
 *
 * The first version of this used only the two eyes — rotate to level, scale by
 * the box, done — on the reasoning that rotation and scale are the bulk of the
 * benefit and the rest is worth a few percent. **That was measured and it was
 * wrong.** On a photograph of nine different people, half of the 36 pairs came
 * back above SFace's published same-person threshold of 0.363: the embeddings
 * of nine strangers were collapsing towards each other.
 *
 * The reason is that the threshold is a property of the *training crop*. Feed
 * the model faces framed differently from the ones it learned on and the
 * numbers it returns are still vectors, still comparable, and no longer
 * separated at the distance anybody published. The two-eye version was not
 * losing a few percent of accuracy — it was invalidating the constant the
 * clustering depends on.
 *
 * So all five landmarks are used, in a least-squares similarity fit. For two
 * dimensions that has a closed form and needs no SVD: with points centred on
 * their means, `a` and `b` below are the projections of the correspondence onto
 * rotation and onto its perpendicular, and [[a, -b], [b, a]] is exactly a
 * rotation scaled uniformly — which is the transform class wanted. Reflection
 * cannot be expressed in that form, which is the point: a mirrored face is not
 * a better fit to anything.
 */
func AlignedCrop(src image.Image, d Detection, out int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, out, out))

	// The canonical positions are defined for 112; anything else scales.
	k := float64(out) / 112.0

	var mpx, mpy, mqx, mqy float64
	for i := 0; i < 5; i++ {
		mpx += float64(d.Landmarks[i][0])
		mpy += float64(d.Landmarks[i][1])
		mqx += canonicalFace[i][0] * k
		mqy += canonicalFace[i][1] * k
	}
	mpx, mpy, mqx, mqy = mpx/5, mpy/5, mqx/5, mqy/5

	var num1, num2, den float64
	for i := 0; i < 5; i++ {
		px := float64(d.Landmarks[i][0]) - mpx
		py := float64(d.Landmarks[i][1]) - mpy
		qx := canonicalFace[i][0]*k - mqx
		qy := canonicalFace[i][1]*k - mqy
		num1 += px*qx + py*qy // along the rotation
		num2 += px*qy - py*qx // perpendicular to it
		den += px*px + py*py
	}
	if den == 0 {
		// Degenerate landmarks — every point in the same place. Nothing can be
		// fitted, and an empty crop is better than a division by zero.
		return dst
	}
	a := num1 / den
	b := num2 / den

	/*
	 * Sampled backwards: for each destination pixel, find where it came from,
	 * because the forward direction leaves holes. The inverse of the scaled
	 * rotation [[a, -b], [b, a]] is its transpose over (a² + b²).
	 */
	det := a*a + b*b
	if det == 0 {
		return dst
	}
	for y := 0; y < out; y++ {
		for x := 0; x < out; x++ {
			qx := float64(x) - mqx
			qy := float64(y) - mqy
			px := (a*qx + b*qy) / det
			py := (-b*qx + a*qy) / det
			dst.Set(x, y, src.At(
				int(math.Round(px+mpx)), int(math.Round(py+mpy))))
		}
	}
	return dst
}
