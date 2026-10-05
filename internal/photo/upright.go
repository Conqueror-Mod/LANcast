package photo

import (
	"io"
	"os"
)

// exifReadLimit bounds how much of a file Orientation reads. EXIF sits in an
// APP1 segment near the start of a JPEG, and a segment cannot exceed 64KB.
const exifReadLimit = 128 * 1024

/*
 * Orientation reads a photograph's EXIF orientation, 1..8, or zero when it has
 * none or cannot be read.
 *
 * For readers that do not decode the photograph themselves but need to know
 * whether its pixels are stored the way it is meant to be seen. Go's decoders
 * ignore the tag, so a phone photo taken in portrait decodes on its side.
 */
func Orientation(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, exifReadLimit))
	if err != nil {
		return 0
	}
	e, err := readEXIF(raw)
	if err != nil {
		return 0
	}
	return e.orientation
}
