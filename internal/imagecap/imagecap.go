// Package imagecap bounds how large a picture may be before it is decoded.
//
// Go's decoders allocate the whole pixel buffer from the size the header
// declares, before they read any pixel data. A PNG of 204 bytes can declare
// 1,000,000 × 1,000,000 pixels, and decoding it asks the runtime for 4 TB.
// That is not a panic: the runtime throws "fatal error: out of memory", no
// defer runs, nothing can recover it, and the process exits. A file from a
// library is read by a worker that starts again at every start, so one such
// file in a library was a server that could not stay up (review R08-1).
//
// So every decode of a picture that came from a library, an archive, or the
// artwork cache goes through here: the header is read with image.DecodeConfig,
// and anything past MaxPixels is refused before a pixel is allocated.
package imagecap

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"
)

// MaxPixels is the largest picture decoded, as width × height.
//
// It is a guard against a header that lies, not a judgement about what a
// photograph should be, so it sits above every real camera: a 200 MP phone
// sensor is 16320 × 12240 (199.8 MP), a medium-format back about 150 MP.
// Decoded, the worst case is a 16-bit PNG at eight bytes a pixel, 1.6 GB:
// large, but an allocation a machine can refuse, where 4 TB is a crash.
//
// internal/picture keeps its own, much smaller, limit: a profile picture is an
// upload drawn at 256 px, and there it is a policy as well as a guard.
const MaxPixels = 200_000_000

// ErrTooLarge is a picture whose header declares more than MaxPixels.
var ErrTooLarge = errors.New("picture too large to decode")

// Check refuses a declared size past MaxPixels. The product is taken in
// int64 so two large sides cannot overflow their way under the limit.
func Check(cfg image.Config) error {
	if cfg.Width < 0 || cfg.Height < 0 ||
		int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return fmt.Errorf("%w: %d × %d", ErrTooLarge, cfg.Width, cfg.Height)
	}
	return nil
}

// Decode is image.Decode with the size checked first. Its errors are
// image.DecodeConfig's and image.Decode's, so image.ErrFormat still means no
// decoder recognised the bytes, or ErrTooLarge.
func Decode(data []byte) (image.Image, string, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	if err := Check(cfg); err != nil {
		return nil, "", err
	}
	return image.Decode(bytes.NewReader(data))
}

// DecodeFrom is Decode for a reader that can be rewound, such as an open file:
// the header is read, the reader goes back to where it started, and the
// picture is decoded from there.
func DecodeFrom(r io.ReadSeeker) (image.Image, string, error) {
	start, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, "", err
	}
	cfg, _, err := image.DecodeConfig(r)
	if err != nil {
		return nil, "", err
	}
	if err := Check(cfg); err != nil {
		return nil, "", err
	}
	if _, err := r.Seek(start, io.SeekStart); err != nil {
		return nil, "", err
	}
	return image.Decode(r)
}
