package photo

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"sort"
)

/*
 * The picture inside a camera RAW file (ADR 0107).
 *
 * Every RAW format keeps a JPEG the camera made for its own screen: full size
 * in Canon's CR2 and CR3, Nikon's NEF, Fujifilm's RAF, most DNGs; a couple of
 * thousand pixels wide in Sony's ARW. That JPEG is the photograph as the
 * camera rendered it, and it is what this shows. The sensor data is never
 * developed: that is a raw converter's job, and a library is for looking.
 *
 * Found by content rather than by parsing each maker's container: every JPEG
 * in the file starts FF D8 FF, so each is tried and the largest that Go's
 * decoder accepts is kept. That one rule covers TIFF-based RAWs (CR2, NEF,
 * ARW, DNG, ORF, RW2, PEF), Fujifilm's RAF and Canon's CR3 alike. The two
 * things that are not a viewable picture refuse themselves: a tiny EXIF
 * thumbnail loses to the full preview on size, and sensor data stored as
 * lossless JPEG (DNG, CR2's raw strip) is a process Go's decoder does not
 * read, so it is never chosen.
 */

// maxPreviewCandidates bounds the JPEGs tried in one file. A RAW holds two to
// four; a file with hundreds of SOI markers is not a RAW worth reading.
const maxPreviewCandidates = 64

var errNoPreview = errors.New("no picture inside this RAW file")

type previewCandidate struct {
	at, area int
}

// rawPreview returns the largest decodable JPEG embedded in raw. Candidates
// are found by header and tried largest first, so a preview that is cut short
// or uses a process Go does not decode gives way to the next largest.
func rawPreview(raw []byte) (image.Image, error) {
	soi := []byte{0xFF, 0xD8, 0xFF}
	var cands []previewCandidate
	for i := 0; i < len(raw) && len(cands) < maxPreviewCandidates; {
		j := bytes.Index(raw[i:], soi)
		if j < 0 {
			break
		}
		at := i + j
		if cfg, err := jpeg.DecodeConfig(bytes.NewReader(raw[at:])); err == nil && cfg.Width > 0 && cfg.Height > 0 {
			cands = append(cands, previewCandidate{at: at, area: cfg.Width * cfg.Height})
		}
		i = at + len(soi)
	}
	sort.SliceStable(cands, func(a, b int) bool { return cands[a].area > cands[b].area })
	for _, c := range cands {
		if img, err := jpeg.Decode(bytes.NewReader(raw[c.at:])); err == nil {
			return img, nil
		}
	}
	return nil, errNoPreview
}
