// Package photo decodes picture files and reads the two EXIF facts a picture
// library needs (ADR 0028).
//
// Decoding is split from process execution for the same reason probing is:
// everything except HEIC is decoded in-process against fixtures, so the format
// table is testable in milliseconds with no ffmpeg installed and no photos on
// disk. Only the formats Go cannot read reach a subprocess.
package photo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"

	// Registered for their side effects: each adds itself to image.Decode's
	// format table. jpeg and png also carry the encoders used below.
	_ "image/gif"
	"image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// ErrUnsupported is returned for a file no decoder here can read. It is a
// distinct error rather than a generic failure because the two are acted on
// differently: an unsupported format is reported and moved past, while a
// corrupt file of a supported format is worth naming as a problem.
var ErrUnsupported = errors.New("no decoder for this format")

// Meta is what one pass over a picture yields.
type Meta struct {
	Width  int
	Height int
	// TakenAt is the EXIF capture time in Unix seconds, zero when absent —
	// which is most of a wallpaper or AI-art library. Callers fall back to the
	// file's mtime rather than treating zero as 1970.
	TakenAt int64
	// Orientation is the raw EXIF value, 1..8, zero when absent. Applied when
	// deriving thumbnails; never handed to a client, which would make every
	// consumer responsible for rotating correctly.
	Orientation int
}

// needsFFmpeg is the set Go cannot decode in-process. HEIC is the whole reason
// this branch exists: a phone backup is mostly HEIC, and a picture library that
// showed placeholders for the user's actual photos would be a feature that
// looks finished and is useless.
var needsFFmpeg = map[string]bool{".heic": true, ".heif": true}

// Decoder reads pictures, spawning ffmpeg only for the formats that need it.
type Decoder struct {
	// FFmpeg is the extractor for formats Go cannot read. Nil means those
	// formats are unsupported rather than broken — the caller reports that.
	FFmpeg FFmpegDecoder
}

// FFmpegDecoder converts a picture Go cannot decode into PNG bytes.
type FFmpegDecoder interface {
	Available() bool
	ToPNG(ctx context.Context, path string) ([]byte, error)
}

// Read decodes a picture and returns it with its metadata.
//
// The format is decided by content, not by extension. A .jpg that is really a
// PNG is common enough (every "save as" dialog produces some), and trusting the
// extension would fail a file that decodes perfectly well.
func (d *Decoder) Read(ctx context.Context, path string) (image.Image, Meta, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, Meta{}, err
	}

	meta := Meta{}
	// EXIF is read from the original bytes, before any conversion: an ffmpeg
	// re-encode does not carry the capture time or the orientation forward, so
	// reading it afterwards would silently lose both on exactly the format
	// where they matter most.
	if e, err := readEXIF(raw); err == nil {
		meta.TakenAt, meta.Orientation = e.takenAt, e.orientation
	}

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		ext := strings.ToLower(filepath.Ext(path))
		if !needsFFmpeg[ext] {
			return nil, meta, fmt.Errorf("decode %s: %w", filepath.Base(path), err)
		}
		if d.FFmpeg == nil || !d.FFmpeg.Available() {
			return nil, meta, ErrUnsupported
		}
		png, ferr := d.FFmpeg.ToPNG(ctx, path)
		if ferr != nil {
			return nil, meta, fmt.Errorf("decode %s via ffmpeg: %w", filepath.Base(path), ferr)
		}
		img, _, err = image.Decode(bytes.NewReader(png))
		if err != nil {
			return nil, meta, fmt.Errorf("decode %s after conversion: %w", filepath.Base(path), err)
		}
	}

	b := img.Bounds()
	meta.Width, meta.Height = b.Dx(), b.Dy()
	// Dimensions describe the picture as it will be seen. A quarter turn swaps
	// them, and reporting the stored pair would give a portrait photo landscape
	// dimensions — wrong in the database, and wrong in any layout that reads
	// them before the image loads.
	if meta.Orientation >= 5 && meta.Orientation <= 8 {
		meta.Width, meta.Height = meta.Height, meta.Width
	}
	return img, meta, nil
}

// JPEG re-encodes an image, which is what the artwork cache stores and resizes.
//
// Quality 90 rather than the default 75: these are photographs being viewed as
// photographs, and the cache already holds the original for full screen.
func JPEG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
