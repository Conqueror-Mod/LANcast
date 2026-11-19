package preview

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"io"
)

/*
 * BIF is Roku's trick-play format: one file holding a JPEG per moment, which
 * the TV's own player shows while you fast-forward or rewind. Roku's
 * certification requires trick-play thumbnails on anything over 15 minutes.
 *
 * The frames already exist, tiled into the scrub-bar sheets (ADR 0105), so a
 * BIF is those sheets cut apart. Nothing is read from the film.
 *
 * Layout (all little-endian):
 *
 *	0   magic        89 42 49 46 0d 0a 1a 0a
 *	8   version      uint32, 0
 *	12  frames       uint32
 *	16  multiplier   uint32, ms per timestamp unit (1000: seconds)
 *	20  reserved     44 zero bytes, to 64
 *	64  index        frames+1 × (timestamp uint32, offset uint32); the last
 *	                 entry is (0xffffffff, end of data)
 *	    data         the JPEGs, in order
 */

var bifMagic = []byte{0x89, 'B', 'I', 'F', 0x0d, 0x0a, 0x1a, 0x0a}

// BIFFrames is how many frames a BIF of this manifest holds for a file of
// durationMS (0 for unknown: every tile, padding included), starting at
// fromMS. The first frame is the one at or after fromMS.
func BIFFrames(m *Manifest, durationMS, fromMS int64) (first, count int) {
	total := m.Sheets * m.Columns * m.Rows
	if durationMS > 0 {
		inFile := int((durationMS + int64(m.IntervalMS) - 1) / int64(m.IntervalMS))
		total = min(total, inFile)
	}
	first = int((fromMS + int64(m.IntervalMS) - 1) / int64(m.IntervalMS))
	if first >= total {
		return first, 0
	}
	return first, total - first
}

// WriteBIF writes a BIF of the manifest's frames from fromMS on, with
// timestamps counted from fromMS, so a stream that starts there (an HLS
// conversion begun at a resume point) shows the right picture at its own
// zero. sheet returns sheet n decoded.
func WriteBIF(w io.Writer, m *Manifest, durationMS, fromMS int64, sheet func(n int) (image.Image, error)) error {
	if m.IntervalMS <= 0 || m.Columns <= 0 || m.Rows <= 0 || m.Width <= 0 || m.Height <= 0 {
		return fmt.Errorf("preview: manifest has no shape")
	}
	first, count := BIFFrames(m, durationMS, fromMS)
	perSheet := m.Columns * m.Rows

	var data bytes.Buffer
	stamps := make([]uint32, 0, count)
	sizes := make([]int, 0, count)
	cur, curN := image.Image(nil), -1
	for i := first; i < first+count; i++ {
		n := i / perSheet
		if n != curN {
			img, err := sheet(n)
			if err != nil {
				return fmt.Errorf("preview: sheet %d: %w", n, err)
			}
			cur, curN = img, n
		}
		k := i % perSheet
		x, y := (k%m.Columns)*m.Width, (k/m.Columns)*m.Height
		r := image.Rect(x, y, x+m.Width, y+m.Height).Add(cur.Bounds().Min)
		sub, ok := cur.(interface {
			SubImage(image.Rectangle) image.Image
		})
		if !ok {
			return fmt.Errorf("preview: sheet %d cannot be cut", n)
		}
		before := data.Len()
		if err := jpeg.Encode(&data, sub.SubImage(r), &jpeg.Options{Quality: 75}); err != nil {
			return err
		}
		stamps = append(stamps, uint32((int64(i)*int64(m.IntervalMS)-fromMS)/1000))
		sizes = append(sizes, data.Len()-before)
	}

	head := make([]byte, 64, 64+8*(count+1))
	copy(head, bifMagic)
	binary.LittleEndian.PutUint32(head[12:], uint32(count))
	binary.LittleEndian.PutUint32(head[16:], 1000)
	off := uint32(64 + 8*(count+1))
	for j := range count {
		head = binary.LittleEndian.AppendUint32(head, stamps[j])
		head = binary.LittleEndian.AppendUint32(head, off)
		off += uint32(sizes[j])
	}
	head = binary.LittleEndian.AppendUint32(head, 0xffffffff)
	head = binary.LittleEndian.AppendUint32(head, off)
	if _, err := w.Write(head); err != nil {
		return err
	}
	_, err := w.Write(data.Bytes())
	return err
}
