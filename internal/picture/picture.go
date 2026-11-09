/*
 * Package picture turns an uploaded profile picture into two JPEGs the server
 * drew itself (ADR 0121).
 *
 * The uploaded bytes are never kept. They are decoded, centre-cropped to a
 * square, scaled to 256 and 64 pixels, flattened onto the page colour and
 * encoded afresh. That is what removes EXIF (a phone photo's GPS position
 * included) and anything else riding in the file, without a stripping step
 * that could miss a tag.
 *
 * Pure: bytes in, bytes out, no store and no request. The rules are tested,
 * and fuzzed, without a server.
 */
package picture

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png" // decoders: the allow-list below decides what is accepted
	"io"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Limits on what is accepted.
const (
	// MaxBytes is the largest upload read.
	MaxBytes = 10 << 20
	// MaxSide and MaxPixels bound the decoded size, checked from the header
	// before any pixel is allocated, so a small file cannot expand into
	// gigabytes.
	MaxSide   = 8000
	MaxPixels = 40_000_000
	// MinSide is the smallest picture worth drawing at 64 px.
	MinSide = 16

	Large = 256
	Small = 64
)

/*
 * formats is the allow-list, by the name the decoder registered.
 *
 * Judged by what the bytes are, not by a Content-Type somebody sent. And a
 * list rather than "whatever image.Decode accepts", because other packages in
 * the binary register decoders too (GIF among them), and none of those were
 * chosen for this.
 */
var formats = map[string]bool{"jpeg": true, "png": true, "webp": true}

// ErrRefused wraps every reason an upload is turned away. The text says which
// rule refused it, and is safe to show to the person who sent it.
var ErrRefused = errors.New("picture refused")

func refuse(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrRefused, fmt.Sprintf(format, args...))
}

// background is the page colour (--space-deep), under any transparency.
var background = color.RGBA{0x07, 0x0b, 0x18, 0xff}

// Result is what is kept: two JPEGs.
type Result struct {
	Large []byte // 256 × 256
	Small []byte // 64 × 64
}

// Make reads an upload and draws the two pictures from it. Any error that is
// the upload's fault wraps ErrRefused.
func Make(r io.Reader) (Result, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return Result{}, fmt.Errorf("read picture: %w", err)
	}
	if len(data) > MaxBytes {
		return Result{}, refuse("larger than %d MB", MaxBytes>>20)
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Result{}, refuse("not an image this server reads (JPEG, PNG or WebP)")
	}
	if !formats[format] {
		return Result{}, refuse("%s is not accepted; use JPEG, PNG or WebP", format)
	}
	if cfg.Width > MaxSide || cfg.Height > MaxSide ||
		int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return Result{}, refuse("%d × %d is too large", cfg.Width, cfg.Height)
	}
	if cfg.Width < MinSide || cfg.Height < MinSide {
		return Result{}, refuse("%d × %d is too small", cfg.Width, cfg.Height)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Result{}, refuse("the image could not be read")
	}
	b := img.Bounds()
	// The header and the decoded image are both the file's own claims. Check
	// the one that was actually built, too.
	if b.Dx() != cfg.Width || b.Dy() != cfg.Height || b.Dx() < MinSide || b.Dy() < MinSide {
		return Result{}, refuse("the image is not the size it says it is")
	}

	square := centreSquare(b)
	large, err := drawSquare(img, square, Large)
	if err != nil {
		return Result{}, err
	}
	small, err := drawSquare(img, square, Small)
	if err != nil {
		return Result{}, err
	}
	return Result{Large: large, Small: small}, nil
}

// centreSquare is the largest square in the middle of b.
func centreSquare(b image.Rectangle) image.Rectangle {
	side := min(b.Dx(), b.Dy())
	x := b.Min.X + (b.Dx()-side)/2
	y := b.Min.Y + (b.Dy()-side)/2
	return image.Rect(x, y, x+side, y+side)
}

// drawSquare scales the square region of img to size × size on the page colour
// and encodes it.
func drawSquare(img image.Image, square image.Rectangle, size int) ([]byte, error) {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, square, draw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("encode picture: %w", err)
	}
	return buf.Bytes(), nil
}
