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
 * AlignedCrop cuts a face out and rotates it upright, using the eye positions.
 *
 * The embedder is trained on faces in a canonical pose, and handing it a
 * tilted one costs accuracy in the way that is hardest to notice: the
 * embeddings are still valid vectors, still cluster into something, and are
 * simply less able to tell two people apart. A head tilted thirty degrees in a
 * holiday photograph is not unusual — it is most holiday photographs.
 *
 * A similarity transform from the two eyes is deliberately less than the full
 * five-point Umeyama fit OpenCV uses. It corrects rotation and scale, which is
 * the bulk of the benefit, with arithmetic that can be read and checked by
 * hand. The remaining error is a shear this does not model, and if recognition
 * quality ever needs the last few percent, this function is where it goes.
 */
func AlignedCrop(src image.Image, d Detection, out int) *image.RGBA {
	rightEye := d.Landmarks[0]
	leftEye := d.Landmarks[1]

	// The angle the eye line makes with horizontal, which is what must be
	// undone.
	dx := float64(leftEye[0] - rightEye[0])
	dy := float64(leftEye[1] - rightEye[1])
	angle := math.Atan2(dy, dx)

	cx := float64(d.X + d.W/2)
	cy := float64(d.Y + d.H/2)
	// A little wider than the box: the embedder was trained on crops that
	// include some hair and jaw, and a tight box loses both.
	side := math.Max(float64(d.W), float64(d.H)) * 1.3
	if side <= 0 {
		return image.NewRGBA(image.Rect(0, 0, out, out))
	}
	scale := float64(out) / side

	dst := image.NewRGBA(image.Rect(0, 0, out, out))
	/*
	 * `+angle`, not `-angle`, and the difference is not obvious enough to trust
	 * to reasoning — it was wrong here first, and the alignment test caught it.
	 *
	 * This is a *backward* map: it asks where a destination pixel came from. To
	 * make an upright destination out of a source tilted by `angle`, the
	 * destination offset has to be rotated *by* `angle` to find the source
	 * point. Rotating by `-angle` tilts it further, which produces a crop that
	 * is still a face, still centred, and rotated twice as far as it started —
	 * a picture nobody would look at twice and an embedding measurably worse.
	 */
	cos, sin := math.Cos(angle), math.Sin(angle)

	/*
	 * Sampled backwards — for each destination pixel, work out where it came
	 * from — because the forward direction leaves holes. Nearest-neighbour,
	 * since the crop is usually an upscale of a small region and the
	 * interpolation that would help is not what limits quality here.
	 */
	for y := 0; y < out; y++ {
		for x := 0; x < out; x++ {
			// Destination centre-relative, unscaled, then un-rotated.
			ux := (float64(x) - float64(out)/2) / scale
			uy := (float64(y) - float64(out)/2) / scale
			sx := cx + ux*cos - uy*sin
			sy := cy + ux*sin + uy*cos
			dst.Set(x, y, src.At(int(math.Round(sx)), int(math.Round(sy))))
		}
	}
	return dst
}
