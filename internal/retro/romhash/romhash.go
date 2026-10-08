// Package romhash turns a ROM's bytes into the hashes a DAT file lists it by
// (ADR 0073).
//
// Normalisation is pure and is the part worth testing: the same game reaches
// a library in several byte layouts, and the DAT lists exactly one. Reading
// files is kept apart from it, in read.go, the way probe.ParseJSON is kept
// apart from running ffprobe — so every layout is tested against a few bytes
// of synthetic fixture, with no real ROM anywhere near the repository.
package romhash

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"hash/crc32"
	"strings"
)

// Sums are one byte layout's hashes, upper-case hex as DAT files write them.
type Sums struct {
	CRC32 string
	SHA1  string
}

// Of hashes b.
func Of(b []byte) Sums {
	c := crc32.ChecksumIEEE(b)
	s := sha1.Sum(b)
	return Sums{
		CRC32: strings.ToUpper(hex.EncodeToString([]byte{byte(c >> 24), byte(c >> 16), byte(c >> 8), byte(c)})),
		SHA1:  strings.ToUpper(hex.EncodeToString(s[:])),
	}
}

// Layouts returns the byte layouts a DAT might list a ROM under, most likely
// first. The first is always the file as the DAT expects it; a second, when
// there is one, is the file with a header removed.
//
// Both are kept because a DAT lists a dump one way and a library holds it
// either way. libretro's NES DAT hashes the 16-byte iNES header *in*
// ("Super Mario Bros. (World)" is listed at 40,976 bytes, which is 16 +
// 40,960), while its SNES DAT hashes without the 512-byte copier header some
// dumps still carry. Trying both costs one more hash of a few megabytes.
func Layouts(platform, ext string, b []byte) [][]byte {
	switch platform {
	case "n64":
		return [][]byte{N64ToZ64(b)}
	case "nes":
		if hasINESHeader(b) {
			return [][]byte{b, b[16:]}
		}
		return [][]byte{b}
	case "snes":
		if len(b)%1024 == 512 {
			return [][]byte{b[512:], b}
		}
		return [][]byte{b}
	case "genesis":
		if strings.EqualFold(ext, ".smd") || isSMD(b) {
			if d := DeinterleaveSMD(b); d != nil {
				return [][]byte{d, b}
			}
		}
		return [][]byte{b}
	}
	return [][]byte{b}
}

// N64 dumps come in three byte orders, told apart by the first word, which
// is a fixed value in every cartridge's header. DATs hash the big-endian
// (.z64) order. The first word is checked rather than the extension, because
// files are renamed between the three freely and the extension lies more
// often than the header does.
var (
	n64Z64 = []byte{0x80, 0x37, 0x12, 0x40} // big-endian, as the DAT has it
	n64V64 = []byte{0x37, 0x80, 0x40, 0x12} // 16-bit words byte-swapped
	n64N64 = []byte{0x40, 0x12, 0x37, 0x80} // 32-bit words little-endian
)

// N64Order names the byte order of an N64 dump: "z64", "v64", "n64", or ""
// when the header is none of them.
func N64Order(b []byte) string {
	if len(b) < 4 {
		return ""
	}
	switch {
	case bytes.Equal(b[:4], n64Z64):
		return "z64"
	case bytes.Equal(b[:4], n64V64):
		return "v64"
	case bytes.Equal(b[:4], n64N64):
		return "n64"
	}
	return ""
}

// N64ToZ64 returns an N64 dump in big-endian order. A dump already in that
// order, or one whose header is unrecognised, is returned unchanged — hashing
// an unknown layout as-is gives it its one chance to match rather than none.
func N64ToZ64(b []byte) []byte {
	switch N64Order(b) {
	case "v64":
		out := make([]byte, len(b))
		for i := 0; i+1 < len(b); i += 2 {
			out[i], out[i+1] = b[i+1], b[i]
		}
		return out
	case "n64":
		out := make([]byte, len(b))
		for i := 0; i+3 < len(b); i += 4 {
			out[i], out[i+1], out[i+2], out[i+3] = b[i+3], b[i+2], b[i+1], b[i]
		}
		return out
	}
	return b
}

func hasINESHeader(b []byte) bool {
	return len(b) > 16 && bytes.Equal(b[:4], []byte{'N', 'E', 'S', 0x1A})
}

/*
 * The Super Magic Drive format: a 512-byte header, then the cartridge in
 * 16KB blocks, each storing its odd bytes in the first half and its even
 * bytes in the second. A copier's layout rather than the cartridge's, and the
 * DAT hashes the cartridge.
 */
const smdBlock = 16384

func isSMD(b []byte) bool {
	// Byte 8 and 9 of an SMD header are 0xAA 0xBB, and the remainder after
	// the header divides into whole blocks.
	return len(b) > 512 && (len(b)-512)%smdBlock == 0 && b[8] == 0xAA && b[9] == 0xBB
}

// DeinterleaveSMD returns an SMD dump as a plain cartridge image, or nil when
// b is not a whole number of blocks after its header.
func DeinterleaveSMD(b []byte) []byte {
	if len(b) <= 512 || (len(b)-512)%smdBlock != 0 {
		return nil
	}
	body := b[512:]
	out := make([]byte, len(body))
	half := smdBlock / 2
	for blk := 0; blk < len(body); blk += smdBlock {
		for i := 0; i < half; i++ {
			out[blk+2*i] = body[blk+half+i]
			out[blk+2*i+1] = body[blk+i]
		}
	}
	return out
}

// FormatPS1Serial turns the boot file name in a PlayStation disc's SYSTEM.CNF
// into the serial a Redump DAT lists: "SLUS_005.94" is "SLUS-00594". Returns
// "" for a name that is not shaped like one.
func FormatPS1Serial(boot string) string {
	// "cdrom:\SLUS_005.94;1", "cdrom:SLUS_005.94;1", "cdrom0:\SLUS_005.94"
	s := boot
	if i := strings.LastIndexAny(s, `\/:`); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.IndexByte(s, ';'); i >= 0 {
		s = s[:i]
	}
	s = strings.NewReplacer("_", "", ".", "", "-", "").Replace(strings.ToUpper(strings.TrimSpace(s)))
	return splitSerial(s)
}

// splitSerial puts the dash back into "SLUS00594": the letters, a dash, the
// digits. The DAT always writes the dash; a disc never does.
func splitSerial(s string) string {
	i := 0
	for i < len(s) && s[i] >= 'A' && s[i] <= 'Z' {
		i++
	}
	if i == 0 || i == len(s) {
		return ""
	}
	for _, r := range s[i:] {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return s[:i] + "-" + s[i:]
}
