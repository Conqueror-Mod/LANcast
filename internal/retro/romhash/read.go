package romhash

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"lancast/internal/media"
	"lancast/internal/playlist"
)

// MaxCartridge is the largest cartridge read into memory to hash. The biggest
// N64 cartridge is 64MB; anything far past that is not a cartridge, and
// reading a mislabelled disc image whole would be a gigabyte for nothing.
const MaxCartridge = 128 << 20

// ErrTooLarge is returned for a cartridge past MaxCartridge.
var ErrTooLarge = errors.New("too large to be a cartridge")

// Result is what reading one ROM produced.
type Result struct {
	// Sums are the hashes of each layout Layouts proposed, most likely first.
	// Empty for a disc, which is identified by Serial instead.
	Sums []Sums
	// Serial is a disc's product code ("SLUS-00594"), or "".
	Serial string
	// InnerName is the file inside a zip that was hashed, or "".
	InnerName string
	// Platform is the console, when reading found one the name did not:
	// the platform of a zip's inner file.
	Platform string
}

// Read hashes the ROM at path, or reads a disc's serial. platform is what the
// file's name already said, and may be "" for a zip.
func Read(path, platform string) (Result, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".zip":
		return readZip(path, platform)
	case ".cue":
		serial, err := cueSerial(path)
		return Result{Serial: serial, Platform: platform}, err
	case ".m3u":
		return readM3U(path, platform)
	case ".pbp":
		serial, err := pbpSerial(path)
		return Result{Serial: serial, Platform: platform}, err
	case ".chd":
		// A CHD is compressed hunks behind codecs the standard library does
		// not have. Listed by its filename until something can read one.
		return Result{Platform: platform}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return Result{}, err
	}
	defer f.Close()
	b, err := readLimited(f)
	if err != nil {
		return Result{}, err
	}
	return Result{Sums: sumAll(platform, ext, b), Platform: platform}, nil
}

func readLimited(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, MaxCartridge+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxCartridge {
		return nil, ErrTooLarge
	}
	return b, nil
}

func sumAll(platform, ext string, b []byte) []Sums {
	var out []Sums
	for _, l := range Layouts(platform, ext, b) {
		out = append(out, Of(l))
	}
	return out
}

/*
 * readZip hashes the ROM inside a zip.
 *
 * The entry chosen is the first whose extension names a console — the one
 * matching the zip's own platform if that is known — else the largest. A zip
 * from a ROM set holds one file; one assembled by hand may hold a readme
 * beside it, and hashing the readme would be a confident miss.
 */
func readZip(path, platform string) (Result, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return Result{}, err
	}
	defer zr.Close()

	var pick, largest *zip.File
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		p := media.PlatformOfExt(f.Name)
		if p != "" && (platform == "" || p == platform) && pick == nil {
			pick = f
		}
		if largest == nil || f.UncompressedSize64 > largest.UncompressedSize64 {
			largest = f
		}
	}
	if pick == nil {
		pick = largest
	}
	if pick == nil {
		return Result{}, fmt.Errorf("empty zip")
	}
	if pick.UncompressedSize64 > MaxCartridge {
		return Result{}, ErrTooLarge
	}
	if platform == "" {
		platform = media.PlatformOfExt(pick.Name)
	}
	rc, err := pick.Open()
	if err != nil {
		return Result{}, err
	}
	defer rc.Close()
	b, err := readLimited(rc)
	if err != nil {
		return Result{}, err
	}
	ext := strings.ToLower(filepath.Ext(pick.Name))
	return Result{Sums: sumAll(platform, ext, b), InnerName: pick.Name, Platform: platform}, nil
}

/*
 * readM3U identifies a multi-disc game by its first disc.
 *
 * The list's own name is not a disc, and every disc of one game shares the
 * game's identity, so the first is enough — and disc 1 is the one whose
 * serial a DAT lists under the game's name. The disc must lie inside the
 * list's own folder: an .m3u is a text file anybody can write, and a game
 * that reaches out of its folder is not one this library holds.
 */
func readM3U(path, platform string) (Result, error) {
	discs, err := M3UDiscs(path)
	if err != nil {
		return Result{}, err
	}
	if len(discs) == 0 {
		return Result{Platform: platform}, nil
	}
	first := discs[0]
	if platform == "" {
		platform = media.PlatformOfExt(first)
	}
	r, err := Read(first, platform)
	if err != nil {
		return Result{Platform: platform}, err
	}
	r.InnerName = filepath.Base(first)
	if r.Platform == "" {
		r.Platform = platform
	}
	return r, nil
}

// M3UDiscs lists the disc images an .m3u names, resolved and kept inside its
// folder. Entries that are not disc images, or that escape the folder, are
// dropped rather than failing the list. The scanner uses the same answer to
// decide which discs are covered by a list, so the two cannot disagree.
func M3UDiscs(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := playlist.Parse(io.LimitReader(f, 64<<10))
	if err != nil {
		return nil, err
	}
	dir := filepath.Clean(filepath.Dir(path))
	var out []string
	for _, e := range entries {
		p, ok := playlist.Resolve(dir, e.Path)
		if !ok || !media.IsDisc(p) {
			continue
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// cueSerial reads the serial from the first data track a cue sheet names.
func cueSerial(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	track := ""
	sc := bufio.NewScanner(io.LimitReader(f, 64<<10))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(strings.ToUpper(line), "FILE ") {
			continue
		}
		track = cueFileName(line[5:])
		break
	}
	if track == "" {
		return "", nil
	}
	// The track is resolved beside the cue and must stay there: a cue is a
	// text file anybody can write, and "FILE ..\..\something" is not a track.
	// Backslashes are separators here whatever the host: a cue made on
	// Windows says "Disc	rack.bin", and on Linux that is one odd file name.
	track = strings.ReplaceAll(track, `\`, "/")
	bin := filepath.Join(filepath.Dir(path), filepath.Base(filepath.FromSlash(track)))
	b, err := os.Open(bin)
	if err != nil {
		return "", err
	}
	defer b.Close()
	return discSerial(b)
}

// cueFileName takes `"Name (Track 1).bin" BINARY` and returns the name.
func cueFileName(rest string) string {
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, `"`) {
		if i := strings.Index(rest[1:], `"`); i >= 0 {
			return rest[1 : 1+i]
		}
		return ""
	}
	if i := strings.LastIndexByte(rest, ' '); i > 0 {
		return rest[:i]
	}
	return rest
}

// discSerial reads SYSTEM.CNF from an ISO 9660 track and returns the serial
// in its BOOT line.
func discSerial(r io.ReaderAt) (string, error) {
	d, err := newDisc(r)
	if err != nil {
		return "", err
	}
	cnf, err := d.rootFile("SYSTEM.CNF")
	if err != nil || cnf == nil {
		return "", err
	}
	return SerialFromSystemCNF(cnf), nil
}

// SerialFromSystemCNF finds the BOOT line in SYSTEM.CNF and formats it.
func SerialFromSystemCNF(cnf []byte) string {
	for _, line := range strings.Split(string(cnf), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.ToUpper(strings.TrimSpace(k))
		if k == "BOOT" || k == "BOOT2" {
			return FormatPS1Serial(strings.TrimSpace(v))
		}
	}
	return ""
}

/*
 * disc reads 2048-byte logical sectors from a track, whether it was ripped
 * raw (2352 bytes a sector, with sync, header and error correction) or as
 * plain user data. A PlayStation .bin is raw, almost always in Mode 2.
 */
type disc struct {
	r                io.ReaderAt
	sectorSize, skip int64
}

var cdSync = []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00}

func newDisc(r io.ReaderAt) (*disc, error) {
	head := make([]byte, 16)
	if _, err := r.ReadAt(head, 0); err != nil {
		return nil, err
	}
	if bytes.Equal(head[:12], cdSync) {
		switch head[15] {
		case 1:
			return &disc{r: r, sectorSize: 2352, skip: 16}, nil
		case 2:
			// Mode 2 Form 1: an 8-byte subheader follows the 4-byte header.
			return &disc{r: r, sectorSize: 2352, skip: 24}, nil
		}
		return nil, fmt.Errorf("unknown sector mode %d", head[15])
	}
	return &disc{r: r, sectorSize: 2048}, nil
}

func (d *disc) read(lba, n int64) ([]byte, error) {
	out := make([]byte, 0, n*2048)
	buf := make([]byte, 2048)
	for i := int64(0); i < n; i++ {
		if _, err := d.r.ReadAt(buf, (lba+i)*d.sectorSize+d.skip); err != nil {
			return nil, err
		}
		out = append(out, buf...)
	}
	return out, nil
}

// maxCNF bounds what a directory or SYSTEM.CNF may claim to occupy, so a
// corrupt length cannot ask for gigabytes.
const maxCNF = 64 << 10

// rootFile returns a file from the root directory, or nil if it is absent.
func (d *disc) rootFile(name string) ([]byte, error) {
	pvd, err := d.read(16, 1)
	if err != nil {
		return nil, err
	}
	if pvd[0] != 1 || string(pvd[1:6]) != "CD001" {
		return nil, fmt.Errorf("no ISO 9660 volume descriptor")
	}
	root := pvd[156 : 156+34]
	lba := int64(binary.LittleEndian.Uint32(root[2:6]))
	size := int64(binary.LittleEndian.Uint32(root[10:14]))
	if size <= 0 || size > maxCNF {
		return nil, fmt.Errorf("implausible root directory size %d", size)
	}
	dir, err := d.read(lba, (size+2047)/2048)
	if err != nil {
		return nil, err
	}
	for off := 0; off < len(dir); {
		n := int(dir[off])
		if n == 0 {
			// Records do not cross sectors; a zero length pads to the next.
			off = (off/2048 + 1) * 2048
			continue
		}
		if off+n > len(dir) || n < 34 {
			break
		}
		rec := dir[off : off+n]
		nameLen := int(rec[32])
		if 33+nameLen <= len(rec) {
			got := string(rec[33 : 33+nameLen])
			if i := strings.IndexByte(got, ';'); i >= 0 {
				got = got[:i]
			}
			if strings.EqualFold(got, name) {
				flba := int64(binary.LittleEndian.Uint32(rec[2:6]))
				fsize := int64(binary.LittleEndian.Uint32(rec[10:14]))
				if fsize <= 0 || fsize > maxCNF {
					return nil, fmt.Errorf("implausible %s size %d", name, fsize)
				}
				b, err := d.read(flba, (fsize+2047)/2048)
				if err != nil {
					return nil, err
				}
				return b[:fsize], nil
			}
		}
		off += n
	}
	return nil, nil
}

// pbpSerial reads DISC_ID from the PARAM.SFO at the front of a PBP.
func pbpSerial(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	head := make([]byte, 0x28)
	if _, err := io.ReadFull(f, head); err != nil {
		return "", err
	}
	if string(head[:4]) != "\x00PBP" {
		return "", fmt.Errorf("not a PBP")
	}
	start := int64(binary.LittleEndian.Uint32(head[8:12]))
	end := int64(binary.LittleEndian.Uint32(head[12:16]))
	if end <= start || end-start > maxCNF {
		return "", fmt.Errorf("implausible PARAM.SFO size")
	}
	sfo := make([]byte, end-start)
	if _, err := f.ReadAt(sfo, start); err != nil {
		return "", err
	}
	return SerialFromSFO(sfo), nil
}

// SerialFromSFO returns DISC_ID from a PARAM.SFO, dashed as a DAT writes it.
func SerialFromSFO(sfo []byte) string {
	if len(sfo) < 20 || string(sfo[:4]) != "\x00PSF" {
		return ""
	}
	keys := int(binary.LittleEndian.Uint32(sfo[8:12]))
	data := int(binary.LittleEndian.Uint32(sfo[12:16]))
	n := int(binary.LittleEndian.Uint32(sfo[16:20]))
	for i := 0; i < n; i++ {
		e := 20 + i*16
		if e+16 > len(sfo) {
			return ""
		}
		ko := keys + int(binary.LittleEndian.Uint16(sfo[e:e+2]))
		l := int(binary.LittleEndian.Uint32(sfo[e+4 : e+8]))
		do := data + int(binary.LittleEndian.Uint32(sfo[e+12:e+16]))
		if ko >= len(sfo) || do+l > len(sfo) || l < 0 {
			return ""
		}
		key := sfo[ko:]
		if j := bytes.IndexByte(key, 0); j >= 0 {
			key = key[:j]
		}
		if string(key) == "DISC_ID" {
			v := strings.TrimRight(string(sfo[do:do+l]), "\x00")
			return splitSerial(strings.ToUpper(v))
		}
	}
	return ""
}

/*
 * GameFiles lists every file a game is made of, entry file first: a cartridge
 * is itself; a .cue is itself and the tracks it names; an .m3u is itself,
 * each disc it lists, and each disc's tracks.
 *
 * It is what the desktop client fetches before a disc game can start, and it
 * is the whole of what the server will serve for a game beyond its entry file
 * (ADR 0073). So every path is resolved against the entry's folder and kept
 * inside it — a cue or a list is a text file anybody can write — and a name
 * that would leave the folder is dropped rather than served.
 */
func GameFiles(path string) ([]string, error) {
	dir := filepath.Clean(filepath.Dir(path))
	out := []string{path}
	seen := map[string]bool{filepath.Clean(path): true}
	add := func(p string) {
		p = filepath.Clean(p)
		if !seen[p] && inside(dir, p) {
			seen[p] = true
			out = append(out, p)
		}
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cue":
		tracks, err := cueFiles(path)
		if err != nil {
			return nil, err
		}
		for _, t := range tracks {
			add(t)
		}
	case ".m3u":
		discs, err := M3UDiscs(path)
		if err != nil {
			return nil, err
		}
		for _, d := range discs {
			add(d)
			if strings.EqualFold(filepath.Ext(d), ".cue") {
				tracks, err := cueFiles(d)
				if err != nil {
					// A disc whose sheet is missing is reported when the
					// client asks for it, not by refusing the whole list.
					continue
				}
				for _, t := range tracks {
					add(t)
				}
			}
		}
	}
	return out, nil
}

// cueFiles is every FILE a cue sheet names, beside the sheet.
func cueFiles(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(io.LimitReader(f, 64<<10))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(strings.ToUpper(line), "FILE ") {
			continue
		}
		name := strings.ReplaceAll(cueFileName(line[5:]), `\`, "/")
		if name == "" {
			continue
		}
		out = append(out, filepath.Join(filepath.Dir(path), filepath.FromSlash(name)))
	}
	return out, sc.Err()
}

func inside(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
