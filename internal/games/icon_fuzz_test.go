package games

import (
	"bytes"
	"encoding/binary"
	"testing"
)

/*
 * R12-3: an icon whose bitmap header claims to be larger than the entry it is
 * in. decodeDIB sliced past the end, and the panic ended the desktop app every
 * time the Games screen drew that game. Every byte read here was written by a
 * game's installer, so each path is also fuzzed: whatever the bytes, the
 * answer is an icon or "no icon", never a panic.
 */

// oversizedHeader is a DIB entry whose biSize is 1000 in 40 bytes: the
// finding's proof, as an icon entry.
func oversizedHeader() []byte {
	d := make([]byte, 40)
	binary.LittleEndian.PutUint32(d[0:], 1000)
	binary.LittleEndian.PutUint32(d[4:], 16)
	binary.LittleEndian.PutUint32(d[8:], 32)
	binary.LittleEndian.PutUint16(d[14:], 32)
	return d
}

func TestADIBHeaderPastItsEntryIsNoIcon(t *testing.T) {
	if _, err := decodeDIB(oversizedHeader()); err == nil {
		t.Fatal("decodeDIB accepted a header larger than its data")
	}
	// One byte past the end is as wrong as a thousand; exactly the end is a
	// header with no pixels after it, which the pixel check then refuses.
	d := dib32(4, 4)
	binary.LittleEndian.PutUint32(d[0:], uint32(len(d)+1))
	if _, err := decodeDIB(d); err == nil {
		t.Fatal("decodeDIB accepted a header one byte past its data")
	}
	binary.LittleEndian.PutUint32(d[0:], uint32(len(d)))
	if _, err := decodeDIB(d); err == nil {
		t.Fatal("decodeDIB found pixels after a header that fills the entry")
	}
	binary.LittleEndian.PutUint32(d[0:], 0xFFFFFFFF)
	if _, err := decodeDIB(d); err == nil {
		t.Fatal("decodeDIB accepted a header of 4 GB")
	}
}

// In an .ico: the bad entry is passed over, and a good one beside it is used.
func TestAnIcoWithAnOversizedDIBHeaderFallsBackOrRefuses(t *testing.T) {
	if _, err := icoBest(ico([]iconEntry{{w: 16, h: 16, bpp: 32, data: oversizedHeader()}})); err == nil {
		t.Fatal("an .ico holding only the bad entry gave an icon")
	}
	out, err := icoBest(ico([]iconEntry{
		{w: 64, h: 64, bpp: 32, data: oversizedHeader()}, // claims to be the best
		{w: 4, h: 4, bpp: 32, data: dib32(4, 4)},
	}))
	if err != nil {
		t.Fatalf("the good entry should still be found: %v", err)
	}
	if b := decodePNG(t, out).Bounds(); b.Dx() != 4 {
		t.Fatalf("got %v, want the 4px entry", b)
	}
}

// rsrc builds a PE resource section, at virtual address base, holding one
// RT_GROUP_ICON (id 1) and the given RT_ICONs by id, laid out as the format
// does: a type directory, a name directory per type, a language directory per
// name, then data entries pointing at the bytes by virtual address.
func rsrc(base uint32, group []byte, icons map[uint16][]byte) []byte {
	type leaf struct {
		typ, id uint32
		data    []byte
	}
	leaves := []leaf{{rtGroupIcon, 1, group}}
	for id, d := range icons {
		leaves = append(leaves, leaf{rtIcon, uint32(id), d})
	}
	types := []uint32{rtIcon, rtGroupIcon}

	dirSize := func(n int) int { return 16 + 8*n }
	// Offsets, in order: root, one name dir per type, one lang dir per leaf,
	// one data entry per leaf, then the data.
	off := dirSize(len(types))
	nameDir := map[uint32]int{}
	for _, typ := range types {
		nameDir[typ] = off
		n := 0
		for _, l := range leaves {
			if l.typ == typ {
				n++
			}
		}
		off += dirSize(n)
	}
	langDir := make([]int, len(leaves))
	for i := range leaves {
		langDir[i] = off
		off += dirSize(1)
	}
	entry := make([]int, len(leaves))
	for i := range leaves {
		entry[i] = off
		off += 16
	}
	dataAt := make([]int, len(leaves))
	for i, l := range leaves {
		dataAt[i] = off
		off += len(l.data)
	}

	out := make([]byte, off)
	dir := func(at, n int) {
		binary.LittleEndian.PutUint16(out[at+14:], uint16(n))
	}
	put := func(at int, name, target uint32) {
		binary.LittleEndian.PutUint32(out[at:], name)
		binary.LittleEndian.PutUint32(out[at+4:], target)
	}
	dir(0, len(types))
	for i, typ := range types {
		put(16+8*i, typ, 0x80000000|uint32(nameDir[typ]))
	}
	for _, typ := range types {
		n := 0
		for i, l := range leaves {
			if l.typ == typ {
				put(nameDir[typ]+16+8*n, l.id, 0x80000000|uint32(langDir[i]))
				n++
			}
		}
		dir(nameDir[typ], n)
	}
	for i, l := range leaves {
		dir(langDir[i], 1)
		put(langDir[i]+16, 0x409, uint32(entry[i]))
		binary.LittleEndian.PutUint32(out[entry[i]:], base+uint32(dataAt[i]))
		binary.LittleEndian.PutUint32(out[entry[i]+4:], uint32(len(l.data)))
		copy(out[dataAt[i]:], l.data)
	}
	return out
}

// groupOf is an RT_GROUP_ICON naming one RT_ICON per entry.
func groupOf(entries []iconEntry, ids []uint16) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, [3]uint16{0, 1, uint16(len(entries))})
	for i, e := range entries {
		b.Write([]byte{byte(e.w), byte(e.h), 0, 0})
		binary.Write(&b, binary.LittleEndian, uint16(1))
		binary.Write(&b, binary.LittleEndian, uint16(e.bpp))
		binary.Write(&b, binary.LittleEndian, uint32(len(e.data)))
		binary.Write(&b, binary.LittleEndian, ids[i])
	}
	return b.Bytes()
}

// The executable path hands RT_ICON bytes to the same decoder; the finding's
// entry there is "no icon" too, and the walk itself reads a good one.
func TestAnExecutablesIconWithAnOversizedDIBHeaderIsNoIcon(t *testing.T) {
	const base = 0x5000
	good := dib32(4, 4)
	sec := rsrc(base, groupOf([]iconEntry{{w: 4, h: 4, bpp: 32, data: good}}, []uint16{2}), map[uint16][]byte{2: good})
	out, err := rsrcIcon(sec, base)
	if err != nil {
		t.Fatalf("a well-formed resource section: %v", err)
	}
	if b := decodePNG(t, out).Bounds(); b.Dx() != 4 {
		t.Fatalf("got %v, want the 4px icon", b)
	}

	bad := oversizedHeader()
	sec = rsrc(base, groupOf([]iconEntry{{w: 16, h: 16, bpp: 32, data: bad}}, []uint16{2}), map[uint16][]byte{2: bad})
	if _, err := rsrcIcon(sec, base); err == nil {
		t.Fatal("an executable holding only the bad icon gave an icon")
	}
}

func FuzzDecodeDIB(f *testing.F) {
	f.Add(oversizedHeader())
	f.Add(dib32(4, 4))
	f.Add(dib32(1, 1))
	f.Fuzz(func(t *testing.T, d []byte) {
		_, _ = decodeDIB(d)
	})
}

func FuzzIcoBest(f *testing.F) {
	f.Add(ico([]iconEntry{{w: 16, h: 16, bpp: 32, data: oversizedHeader()}}))
	f.Add(ico([]iconEntry{{w: 4, h: 4, bpp: 32, data: dib32(4, 4)}}))
	f.Add([]byte{0, 0, 1, 0, 0xFF, 0xFF})
	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _ = icoBest(raw)
	})
}

func FuzzRsrcIcon(f *testing.F) {
	const base = 0x5000
	good := dib32(4, 4)
	bad := oversizedHeader()
	f.Add(rsrc(base, groupOf([]iconEntry{{w: 4, h: 4, bpp: 32, data: good}}, []uint16{2}), map[uint16][]byte{2: good}), uint32(base))
	f.Add(rsrc(base, groupOf([]iconEntry{{w: 16, h: 16, bpp: 32, data: bad}}, []uint16{2}), map[uint16][]byte{2: bad}), uint32(base))
	f.Add([]byte{}, uint32(0))
	f.Fuzz(func(t *testing.T, sec []byte, base uint32) {
		_, _ = rsrcIcon(sec, base)
	})
}
