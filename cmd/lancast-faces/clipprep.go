package main

import (
	"image"
	"math"

	"golang.org/x/image/draw"
)

/*
 * Turning a photograph into the tensor CLIP expects (ADR 0060).
 *
 * Kept out of the cgo file on purpose, the way `imageprep.go` and `decode.go`
 * already are. The same rule the probe follows — "process execution stays split
 * from parsing" — applies here: preprocessing is arithmetic over pixels, and
 * arithmetic that can only be tested by loading a 300MB model and linking a
 * native runtime is arithmetic nobody checks.
 *
 * IT HAS TO MATCH THE TRAINING TRANSFORM, AND NOTHING SAYS WHEN IT DOES NOT
 *
 * The model was trained on images prepared one particular way. Resize with the
 * wrong rule, crop from the wrong place, or normalise with the wrong constants,
 * and the vector is still 512 numbers, still unit length, still ranks the
 * library — a little worse, in a way no test and no user can point at. So the
 * three decisions are written down beside the numbers rather than left as
 * literals.
 */

const (
	// ClipImageSize is ViT-B/32's input resolution.
	ClipImageSize = 224
)

/*
 * clipMean and clipStd are the constants the model was trained with.
 *
 * They are *not* ImageNet's, which is the mistake to make here because almost
 * every other vision model uses those and they look close enough to be a typo
 * rather than a different distribution. These come from the reference
 * preprocessing and are the only ones that make the vector mean what the text
 * encoder thinks it means.
 */
var (
	clipMean = [3]float32{0.48145466, 0.4578275, 0.40821073}
	clipStd  = [3]float32{0.26862954, 0.26130258, 0.27577711}
)

/*
 * ClipCrop resizes so the shortest side is 224 and takes the centre square.
 *
 * Shortest-side-then-centre rather than a straight squash, because that is the
 * training transform: squashing a 3:2 photograph into a square changes every
 * aspect ratio in the library and the model has never seen the world that
 * shape. It costs the edges of a wide photograph, which is the trade the
 * reference makes and is why a search for something at the far left of a
 * panorama can miss.
 *
 * The resampling is CatmullRom, which is the same filter `Letterbox` already
 * uses for detection and is in the bicubic family the reference asks for. One
 * resampler in the binary rather than two: a second would be a second thing to
 * get subtly wrong, for a difference smaller than the one between two
 * photographs of the same scene.
 */
func ClipCrop(src image.Image) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return image.NewRGBA(image.Rect(0, 0, ClipImageSize, ClipImageSize))
	}

	// Scale so the *shorter* side reaches 224; the longer one overhangs and is
	// cropped away.
	scale := float64(ClipImageSize) / float64(w)
	if h < w {
		scale = float64(ClipImageSize) / float64(h)
	}
	rw := int(float64(w)*scale + 0.5)
	rh := int(float64(h)*scale + 0.5)
	if rw < ClipImageSize {
		rw = ClipImageSize
	}
	if rh < ClipImageSize {
		rh = ClipImageSize
	}

	resized := image.NewRGBA(image.Rect(0, 0, rw, rh))
	draw.CatmullRom.Scale(resized, resized.Bounds(), src, b, draw.Src, nil)

	// Centre square. Integer division floors, which biases a one-pixel
	// remainder to the top-left — the reference does the same.
	ox := (rw - ClipImageSize) / 2
	oy := (rh - ClipImageSize) / 2
	out := image.NewRGBA(image.Rect(0, 0, ClipImageSize, ClipImageSize))
	draw.Draw(out, out.Bounds(), resized, image.Pt(ox, oy), draw.Src)
	return out
}

/*
 * ClipTensor lays the crop out as CHW float32, normalised.
 *
 * Channel-major rather than pixel-major: ONNX wants [1,3,224,224], so all the
 * reds come first. Writing it interleaved produces a tensor of exactly the
 * right size whose every value is in the wrong place, which the runtime accepts
 * without complaint.
 *
 * **RGB, and `Tensor` in imageprep.go is BGR. That is not an inconsistency to
 * tidy up.** SFace comes from OpenCV, which is BGR by convention, and CLIP's
 * preprocessing is RGB — so the two functions in this binary genuinely disagree
 * because the two models do. Unifying them would fix the asymmetry and break
 * one of the two silently, since a channel-swapped image is still a valid
 * tensor producing a valid vector. `TestTensorIsBGRNotRGB` guards the other
 * one; `TestTensorIsChannelMajor` guards this.
 */
func ClipTensor(img *image.RGBA) []float32 {
	const n = ClipImageSize * ClipImageSize
	out := make([]float32, 3*n)
	for y := 0; y < ClipImageSize; y++ {
		row := img.PixOffset(0, y)
		for x := 0; x < ClipImageSize; x++ {
			p := row + x*4
			i := y*ClipImageSize + x
			// 0..255 to 0..1 first, because the constants are expressed against
			// that range.
			r := float32(img.Pix[p]) / 255
			g := float32(img.Pix[p+1]) / 255
			b := float32(img.Pix[p+2]) / 255
			out[i] = (r - clipMean[0]) / clipStd[0]
			out[n+i] = (g - clipMean[1]) / clipStd[1]
			out[2*n+i] = (b - clipMean[2]) / clipStd[2]
		}
	}
	return out
}

/*
 * Normalize scales a vector to unit length, in place, and reports whether it
 * could.
 *
 * The store compares with a cosine, which divides by the magnitudes — so
 * normalising here means the comparison is a plain dot product and, more
 * importantly, means an image vector and a text vector are on the same sphere.
 * The reference normalises both before comparing them, and skipping it makes
 * every score a little wrong in a way that still sorts.
 *
 * A zero vector is refused rather than divided by. It is what a blank frame or
 * a failed run produces, and a NaN-filled embedding stored once poisons every
 * later ranking it appears in.
 */
func Normalize(v []float32) bool {
	var sum float64
	for _, f := range v {
		sum += float64(f) * float64(f)
	}
	if sum == 0 {
		return false
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
	return true
}
