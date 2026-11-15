// Package imagecaptest builds pictures whose header lies about their size,
// for the tests of everything that decodes a picture from a library.
package imagecaptest

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
)

// BombPNG is a PNG of under a hundred bytes whose header declares w × h RGBA
// pixels. Before imagecap, decoding a 1,000,000 × 1,000,000 one asked the
// runtime for 4 TB and killed the process (review R08-1).
func BombPNG(w, h uint32) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 6 // RGBA
	chunk(&b, "IHDR", ihdr)
	// A stored, empty zlib stream: the decoder gets as far as the pixel data,
	// which is where it used to allocate the whole image.
	chunk(&b, "IDAT", []byte{0x78, 0x01, 0x01, 0x00, 0x00})
	chunk(&b, "IEND", nil)
	return b.Bytes()
}

func chunk(b *bytes.Buffer, kind string, data []byte) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(data)))
	b.Write(n[:])
	b.WriteString(kind)
	b.Write(data)
	sum := crc32.NewIEEE()
	sum.Write([]byte(kind))
	sum.Write(data)
	binary.BigEndian.PutUint32(n[:], sum.Sum32())
	b.Write(n[:])
}
