package report

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"

	_ "golang.org/x/image/webp"
	_ "image/gif" // decoders: a report's picture may be any of these

	xdraw "golang.org/x/image/draw"
)

/*
 * Pictures with a report: screenshots, mostly, and the odd phone photo of a
 * television.
 *
 * Every one is decoded and encoded again here, never passed along as sent.
 * That is what makes it a picture rather than whatever bytes arrived with an
 * image's name, and it is what drops a photo's metadata: a phone photograph
 * carries where it was taken, and the person attaching it to a bug report
 * did not mean to send their address to the developer.
 */

const (
	MaxImages = 4
	// MaxImageBytes is what one picture may be as sent. The client shrinks
	// anything larger before it leaves the device.
	MaxImageBytes = 6 << 20
	// maxImagePixels refuses a small file that claims an enormous canvas
	// before anything is allocated for it.
	maxImagePixels = 50_000_000
	maxImageEdge   = 2560
	// What one picture may be once encoded here. Four of them and a full log
	// stay under the 10 MB Discord accepts for one message.
	maxPreparedBytes = 2 << 20
)

// Image is one picture, ready to attach.
type Image struct {
	Name string
	Data []byte
}

// ErrNotAnImage is a file that is not a PNG, JPEG, GIF or WebP picture.
var ErrNotAnImage = errors.New("report: not a picture")

// PrepareImage checks that data is a picture and encodes it again, no larger
// than 2560 pixels on its long edge: as PNG when it came as anything but a
// photograph, so text in a screenshot stays sharp, and as JPEG when PNG would
// be too large or it came as JPEG. n numbers its file name.
func PrepareImage(data []byte, n int) (Image, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Image{}, ErrNotAnImage
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxImagePixels {
		return Image{}, fmt.Errorf("%w: %d×%d is too large", ErrNotAnImage, cfg.Width, cfg.Height)
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Image{}, ErrNotAnImage
	}
	img := fit(src, maxImageEdge)

	name := fmt.Sprintf("picture-%d", n)
	if format != "jpeg" {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return Image{}, err
		}
		if buf.Len() <= maxPreparedBytes {
			return Image{Name: name + ".png", Data: buf.Bytes()}, nil
		}
	}
	// A photograph, or a screenshot too busy for PNG to keep small. Lower
	// quality first, then fewer pixels, until it fits.
	for _, step := range []struct{ edge, quality int }{
		{maxImageEdge, 88}, {maxImageEdge, 75}, {1920, 75}, {1280, 70},
	} {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, fit(img, step.edge), &jpeg.Options{Quality: step.quality}); err != nil {
			return Image{}, err
		}
		if buf.Len() <= maxPreparedBytes {
			return Image{Name: name + ".jpg", Data: buf.Bytes()}, nil
		}
	}
	return Image{}, fmt.Errorf("report: picture %d could not be made small enough", n)
}

// fit returns img no larger than edge pixels on its long side, and as an
// opaque RGBA picture in either case, so every encoder sees the same thing.
func fit(img image.Image, edge int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > edge || h > edge {
		if w >= h {
			h, w = max(1, h*edge/w), edge
		} else {
			w, h = max(1, w*edge/h), edge
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	// Transparent areas become white, as they look in every viewer that
	// shows a PNG on a page, rather than black, as JPEG would make them.
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	if w == b.Dx() && h == b.Dy() {
		draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Over)
	} else {
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
	}
	return dst
}
