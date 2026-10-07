package games

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

/*
 * A game's icon, for the tile of a game no launcher kept a poster for.
 *
 * Steam caches posters; Epic, Battle.net, GOG and the EA app keep none on this
 * disk, and fetching one would be the phone-home LANcast does not do (ADR 0066).
 * But every game has an icon here: an Xbox package names its logos, GOG ships a
 * goggame-<id>.ico, and every Windows executable carries one in its resources,
 * usually at 256x256. That is what the tile shows instead of a letter.
 *
 * Pure Go: the ICO directory, the PE resource table, and the two image forms an
 * icon entry can take (a PNG, or a 32-bit bitmap with an AND mask). No Win32
 * call, so the rules are tested on any operating system.
 */

/*
 * IconTarget is a game's icon source, checked: inside its install folder, and
 * there. A path written by a launcher (or named in a manifest a launcher wrote)
 * is data, and this is where it becomes a file read.
 */
func IconTarget(g Game) (string, error) {
	if g.IconSource == "" {
		return "", errNoIcon
	}
	root, err := filepath.Abs(g.InstallPath)
	if err != nil || g.InstallPath == "" {
		return "", errNoIcon
	}
	src, err := filepath.Abs(g.IconSource)
	if err != nil {
		return "", errNoIcon
	}
	rel, err := filepath.Rel(root, src)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("icon: %q is outside %q", src, root)
	}
	if st, err := os.Stat(src); err != nil || st.IsDir() {
		return "", errNoIcon
	}
	return src, nil
}

// maxIconSource bounds what is read: a PNG or ICO file, or the resource
// section of an executable.
const maxIconSource = 32 << 20

var errNoIcon = errors.New("no usable icon")

// IconPNG returns the best image in source as PNG bytes. source is a .png, an
// .ico, or an .exe.
func IconPNG(source string) ([]byte, error) {
	switch strings.ToLower(filepath.Ext(source)) {
	case ".png":
		st, err := os.Stat(source)
		if err != nil {
			return nil, err
		}
		if st.Size() > maxIconSource {
			return nil, fmt.Errorf("icon: %s is too large", source)
		}
		return os.ReadFile(source)
	case ".ico":
		raw, err := readCapped(source)
		if err != nil {
			return nil, err
		}
		return icoBest(raw)
	case ".exe":
		return exeIcon(source)
	}
	return nil, fmt.Errorf("icon: %s is not an image or an executable", source)
}

func readCapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxIconSource+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxIconSource {
		return nil, fmt.Errorf("icon: %s is too large", path)
	}
	return raw, nil
}

// iconEntry is one image an icon offers: its size and colour depth, and its
// bytes (a PNG, or a bitmap without its file header).
type iconEntry struct {
	w, h, bpp int
	data      []byte
}

// pick orders the entries best first: the largest, then the deepest colour.
func pick(entries []iconEntry) []iconEntry {
	out := append([]iconEntry(nil), entries...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && better(out[j], out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func better(a, b iconEntry) bool {
	if a.w*a.h != b.w*b.h {
		return a.w*a.h > b.w*b.h
	}
	return a.bpp > b.bpp
}

// encodeEntry turns the best entry it can decode into PNG bytes.
func encodeEntries(entries []iconEntry) ([]byte, error) {
	for _, e := range pick(entries) {
		if bytes.HasPrefix(e.data, []byte("\x89PNG\r\n\x1a\n")) {
			return e.data, nil
		}
		img, err := decodeDIB(e.data)
		if err != nil {
			continue
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			continue
		}
		return buf.Bytes(), nil
	}
	return nil, errNoIcon
}

// icoBest reads an .ico file: a 6-byte header, then 16-byte entries naming
// each image's size, depth, length and offset.
func icoBest(raw []byte) ([]byte, error) {
	if len(raw) < 6 || binary.LittleEndian.Uint16(raw[2:4]) != 1 {
		return nil, errNoIcon
	}
	n := int(binary.LittleEndian.Uint16(raw[4:6]))
	var entries []iconEntry
	for i := 0; i < n; i++ {
		off := 6 + 16*i
		if off+16 > len(raw) {
			break
		}
		e := raw[off : off+16]
		size := int(binary.LittleEndian.Uint32(e[8:12]))
		at := int(binary.LittleEndian.Uint32(e[12:16]))
		if size <= 0 || at < 0 || at+size > len(raw) {
			continue
		}
		entries = append(entries, iconEntry{
			w: dim(e[0]), h: dim(e[1]), bpp: int(binary.LittleEndian.Uint16(e[6:8])),
			data: raw[at : at+size],
		})
	}
	return encodeEntries(entries)
}

// dim reads an icon dimension byte, where 0 means 256.
func dim(b byte) int {
	if b == 0 {
		return 256
	}
	return int(b)
}

/*
 * decodeDIB decodes the bitmap form of an icon image: a BITMAPINFOHEADER and
 * pixels, bottom-up, with the height doubled to cover the AND mask after them.
 * 32-bit (alpha in the pixels) and 24-bit (alpha from the mask) are read; the
 * palette depths are old enough that a game with only those gets its letter.
 */
func decodeDIB(d []byte) (image.Image, error) {
	if len(d) < 40 || binary.LittleEndian.Uint32(d[0:4]) < 40 {
		return nil, errNoIcon
	}
	hdr := int(binary.LittleEndian.Uint32(d[0:4]))
	w := int(int32(binary.LittleEndian.Uint32(d[4:8])))
	h := int(int32(binary.LittleEndian.Uint32(d[8:12]))) / 2
	bpp := int(binary.LittleEndian.Uint16(d[14:16]))
	if w <= 0 || h <= 0 || w > 1024 || h > 1024 || (bpp != 32 && bpp != 24) {
		return nil, errNoIcon
	}
	stride := ((w*bpp + 31) / 32) * 4
	maskStride := ((w + 31) / 32) * 4
	pix := d[hdr:]
	if len(pix) < stride*h {
		return nil, errNoIcon
	}
	mask := pix[stride*h:]
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	anyAlpha := false
	for y := 0; y < h; y++ {
		row := pix[(h-1-y)*stride:]
		for x := 0; x < w; x++ {
			i := x * bpp / 8
			c := color.NRGBA{R: row[i+2], G: row[i+1], B: row[i], A: 255}
			if bpp == 32 {
				c.A = row[i+3]
				if c.A != 0 {
					anyAlpha = true
				}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	// No alpha in the pixels (24-bit, or 32-bit written without it): the AND
	// mask says which pixels are transparent.
	if !anyAlpha && len(mask) >= maskStride*h {
		for y := 0; y < h; y++ {
			mrow := mask[(h-1-y)*maskStride:]
			for x := 0; x < w; x++ {
				c := img.NRGBAAt(x, y)
				if mrow[x/8]&(0x80>>(x%8)) != 0 {
					c.A = 0
				} else {
					c.A = 255
				}
				img.SetNRGBA(x, y, c)
			}
		}
	}
	return img, nil
}

const (
	rtIcon      = 3
	rtGroupIcon = 14
)

/*
 * exeIcon reads the icon an executable shows in Explorer: the first icon group
 * in its resources (RT_GROUP_ICON), whose entries name images stored as
 * RT_ICON resources by id.
 */
func exeIcon(path string) ([]byte, error) {
	f, err := pe.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sec := f.Section(".rsrc")
	if sec == nil || sec.Size == 0 || sec.Size > maxIconSource {
		return nil, errNoIcon
	}
	rsrc, err := sec.Data()
	if err != nil {
		return nil, err
	}
	r := resources{data: rsrc, base: sec.VirtualAddress}

	groups := r.leaves(rtGroupIcon)
	if len(groups) == 0 {
		return nil, errNoIcon
	}
	icons := map[uint32][]byte{}
	for id, data := range r.byID(rtIcon) {
		icons[id] = data
	}
	grp := groups[0]
	if len(grp) < 6 {
		return nil, errNoIcon
	}
	n := int(binary.LittleEndian.Uint16(grp[4:6]))
	var entries []iconEntry
	for i := 0; i < n; i++ {
		off := 6 + 14*i
		if off+14 > len(grp) {
			break
		}
		e := grp[off : off+14]
		id := uint32(binary.LittleEndian.Uint16(e[12:14]))
		data, ok := icons[id]
		if !ok {
			continue
		}
		entries = append(entries, iconEntry{
			w: dim(e[0]), h: dim(e[1]), bpp: int(binary.LittleEndian.Uint16(e[6:8])), data: data,
		})
	}
	return encodeEntries(entries)
}

// resources reads a PE resource section: a tree of type, name and language
// directories whose leaves point at data by relative virtual address.
type resources struct {
	data []byte
	base uint32
}

// dir returns the (id, offset, isDir) entries of the directory at off.
func (r resources) dir(off int) [][3]uint32 {
	if off+16 > len(r.data) {
		return nil
	}
	n := int(binary.LittleEndian.Uint16(r.data[off+12:])) + int(binary.LittleEndian.Uint16(r.data[off+14:]))
	var out [][3]uint32
	for i := 0; i < n; i++ {
		e := off + 16 + 8*i
		if e+8 > len(r.data) {
			break
		}
		name := binary.LittleEndian.Uint32(r.data[e:])
		target := binary.LittleEndian.Uint32(r.data[e+4:])
		isDir := uint32(0)
		if target&0x80000000 != 0 {
			isDir = 1
		}
		out = append(out, [3]uint32{name, target &^ 0x80000000, isDir})
	}
	return out
}

// leafData reads the first language's data under a name directory.
func (r resources) leafData(off uint32) ([]byte, bool) {
	langs := r.dir(int(off))
	if len(langs) == 0 || langs[0][2] == 1 {
		return nil, false
	}
	e := int(langs[0][1])
	if e+8 > len(r.data) {
		return nil, false
	}
	rva := binary.LittleEndian.Uint32(r.data[e:])
	size := binary.LittleEndian.Uint32(r.data[e+4:])
	if rva < r.base {
		return nil, false
	}
	at := int(rva - r.base)
	if at+int(size) > len(r.data) || size == 0 {
		return nil, false
	}
	return r.data[at : at+int(size)], true
}

// typeDir finds the directory of one resource type.
func (r resources) typeDir(typ uint32) (uint32, bool) {
	for _, e := range r.dir(0) {
		if e[0] == typ && e[2] == 1 {
			return e[1], true
		}
	}
	return 0, false
}

// leaves is every resource of a type, in directory order.
func (r resources) leaves(typ uint32) [][]byte {
	td, ok := r.typeDir(typ)
	if !ok {
		return nil
	}
	var out [][]byte
	for _, name := range r.dir(int(td)) {
		if name[2] != 1 {
			continue
		}
		if data, ok := r.leafData(name[1]); ok {
			out = append(out, data)
		}
	}
	return out
}

// byID is every resource of a type named by a numeric id.
func (r resources) byID(typ uint32) map[uint32][]byte {
	out := map[uint32][]byte{}
	td, ok := r.typeDir(typ)
	if !ok {
		return out
	}
	for _, name := range r.dir(int(td)) {
		if name[2] != 1 || name[0]&0x80000000 != 0 {
			continue
		}
		if data, ok := r.leafData(name[1]); ok {
			out[name[0]] = data
		}
	}
	return out
}
