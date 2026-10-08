package romhash

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// n64Fixture is a tiny "cartridge" in big-endian order: the real header
// word, then bytes that make every word distinct so a wrong swap shows.
func n64Fixture() []byte {
	b := append([]byte{}, n64Z64...)
	for i := 0; i < 60; i++ {
		b = append(b, byte(i*7+3))
	}
	return b
}

func toV64(z []byte) []byte {
	out := make([]byte, len(z))
	for i := 0; i+1 < len(z); i += 2 {
		out[i], out[i+1] = z[i+1], z[i]
	}
	return out
}

func toN64(z []byte) []byte {
	out := make([]byte, len(z))
	for i := 0; i+3 < len(z); i += 4 {
		out[i], out[i+1], out[i+2], out[i+3] = z[i+3], z[i+2], z[i+1], z[i]
	}
	return out
}

// All three N64 byte orders hash to the big-endian dump the DAT lists.
func TestN64ByteOrdersHashTheSame(t *testing.T) {
	z := n64Fixture()
	want := Of(z)
	for name, b := range map[string][]byte{"z64": z, "v64": toV64(z), "n64": toN64(z)} {
		t.Run(name, func(t *testing.T) {
			if got := N64Order(b); got != name {
				t.Errorf("N64Order = %q, want %q", got, name)
			}
			if got := Of(Layouts("n64", "."+name, b)[0]); got != want {
				t.Errorf("hash %+v, want %+v", got, want)
			}
		})
	}
}

// The extension is not believed: a .z64 that is really byte-swapped is still
// put right, because the header word says what it is.
func TestN64OrderComesFromTheHeaderNotTheExtension(t *testing.T) {
	z := n64Fixture()
	if got := Of(Layouts("n64", ".z64", toV64(z))[0]); got != Of(z) {
		t.Error("a mislabelled .v64 was hashed as-is")
	}
}

// An unrecognised N64 header is hashed unchanged rather than not at all.
func TestN64UnknownHeaderHashedAsIs(t *testing.T) {
	b := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	if !bytes.Equal(N64ToZ64(b), b) {
		t.Error("unknown order was rewritten")
	}
}

// libretro's NES DAT hashes the iNES header in, so the whole file comes first
// and the headerless body second.
func TestNESHeaderedFirstThenHeaderless(t *testing.T) {
	body := bytes.Repeat([]byte{0xEA}, 64)
	file := append([]byte{'N', 'E', 'S', 0x1A, 2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, body...)
	l := Layouts("nes", ".nes", file)
	if len(l) != 2 || !bytes.Equal(l[0], file) || !bytes.Equal(l[1], body) {
		t.Errorf("layouts = %d, want whole file then body", len(l))
	}
	if l := Layouts("nes", ".nes", body); len(l) != 1 {
		t.Errorf("a headerless dump got %d layouts, want 1", len(l))
	}
}

// A SNES dump with a 512-byte copier header is hashed without it first.
func TestSNESCopierHeaderStripped(t *testing.T) {
	body := bytes.Repeat([]byte{0x42}, 1024)
	file := append(bytes.Repeat([]byte{0}, 512), body...)
	l := Layouts("snes", ".smc", file)
	if len(l) != 2 || !bytes.Equal(l[0], body) {
		t.Error("copier header not stripped first")
	}
	if l := Layouts("snes", ".sfc", body); len(l) != 1 || !bytes.Equal(l[0], body) {
		t.Error("a clean dump was altered")
	}
}

// An SMD dump de-interleaves back to the cartridge it came from.
func TestSMDDeinterleaves(t *testing.T) {
	cart := make([]byte, smdBlock*2)
	for i := range cart {
		cart[i] = byte(i * 31)
	}
	smd := make([]byte, 512, 512+len(cart))
	smd[8], smd[9] = 0xAA, 0xBB
	half := smdBlock / 2
	for blk := 0; blk < len(cart); blk += smdBlock {
		odd := make([]byte, half)
		even := make([]byte, half)
		for i := 0; i < half; i++ {
			even[i] = cart[blk+2*i]
			odd[i] = cart[blk+2*i+1]
		}
		smd = append(smd, odd...)
		smd = append(smd, even...)
	}
	l := Layouts("genesis", ".smd", smd)
	if !bytes.Equal(l[0], cart) {
		t.Error("de-interleaved SMD does not match the cartridge")
	}
	// A plain .md is left alone.
	if l := Layouts("genesis", ".md", cart); len(l) != 1 || !bytes.Equal(l[0], cart) {
		t.Error("a plain cartridge was altered")
	}
}

func TestFormatPS1Serial(t *testing.T) {
	cases := map[string]string{
		`cdrom:\SLUS_005.94;1`: "SLUS-00594",
		`cdrom:SCUS_941.63;1`:  "SCUS-94163",
		`cdrom0:\SLES_123.45`:  "SLES-12345",
		`cdrom:\PSX.EXE;1`:     "",
		``:                     "",
	}
	for in, want := range cases {
		if got := FormatPS1Serial(in); got != want {
			t.Errorf("FormatPS1Serial(%q) = %q, want %q", in, got, want)
		}
	}
}

// rawMode2 builds a disc track in raw 2352-byte Mode 2 sectors from 2048-byte
// logical sectors, the shape a PlayStation .bin has.
func rawMode2(sectors [][]byte) []byte {
	var out []byte
	for _, s := range sectors {
		raw := make([]byte, 2352)
		copy(raw, cdSync)
		raw[15] = 2
		copy(raw[24:], s)
		out = append(out, raw...)
	}
	return out
}

// isoWithCNF is the smallest ISO 9660 image that holds a SYSTEM.CNF: a
// primary volume descriptor at sector 16, the root directory at 18, the file
// at 19.
func isoWithCNF(cnf string) [][]byte {
	secs := make([][]byte, 20)
	for i := range secs {
		secs[i] = make([]byte, 2048)
	}
	pvd := secs[16]
	pvd[0] = 1
	copy(pvd[1:], "CD001")
	root := pvd[156:]
	root[0] = 34
	binary.LittleEndian.PutUint32(root[2:], 18)
	binary.LittleEndian.PutUint32(root[10:], 2048)

	dir := secs[18]
	name := "SYSTEM.CNF;1"
	rec := make([]byte, 33+len(name)+1)
	rec[0] = byte(len(rec))
	binary.LittleEndian.PutUint32(rec[2:], 19)
	binary.LittleEndian.PutUint32(rec[10:], uint32(len(cnf)))
	rec[32] = byte(len(name))
	copy(rec[33:], name)
	// "." and ".." come first on a real disc, so the search must walk past them.
	dot := make([]byte, 34)
	dot[0] = 34
	dot[32] = 1
	off := copy(dir, dot)
	off += copy(dir[off:], dot)
	copy(dir[off:], rec)

	copy(secs[19], cnf)
	return secs
}

const fixtureCNF = "BOOT = cdrom:\\SLUS_005.94;1\r\nTCB = 4\r\nEVENT = 10\r\nSTACK = 801FFFF0\r\n"

func TestDiscSerialFromRawMode2Track(t *testing.T) {
	bin := rawMode2(isoWithCNF(fixtureCNF))
	got, err := discSerial(bytes.NewReader(bin))
	if err != nil {
		t.Fatal(err)
	}
	if got != "SLUS-00594" {
		t.Errorf("serial = %q", got)
	}
}

func TestDiscSerialFromCookedTrack(t *testing.T) {
	var iso []byte
	for _, s := range isoWithCNF(fixtureCNF) {
		iso = append(iso, s...)
	}
	got, err := discSerial(bytes.NewReader(iso))
	if err != nil || got != "SLUS-00594" {
		t.Errorf("serial = %q, %v", got, err)
	}
}

// Read on a .cue follows its first FILE line to the track beside it.
func TestReadCue(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "Game (Track 1).bin"), rawMode2(isoWithCNF(fixtureCNF)))
	cue := filepath.Join(dir, "Game.cue")
	write(t, cue, []byte("FILE \"Game (Track 1).bin\" BINARY\r\n  TRACK 01 MODE2/2352\r\n    INDEX 01 00:00:00\r\n"))
	r, err := Read(cue, "ps1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Serial != "SLUS-00594" || len(r.Sums) != 0 {
		t.Errorf("got %+v", r)
	}
}

// A cue names its track relative to itself, and the track must stay beside
// it: "FILE ..\..\x.bin" does not leave the folder.
func TestCueCannotReachOutsideItsFolder(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "disc")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(parent, "outside.bin"), rawMode2(isoWithCNF(fixtureCNF)))
	cue := filepath.Join(dir, "Game.cue")
	write(t, cue, []byte("FILE \"..\\outside.bin\" BINARY\n"))
	r, err := Read(cue, "ps1")
	if err == nil && r.Serial != "" {
		t.Errorf("a cue read a track outside its folder: %+v", r)
	}
}

func TestSerialFromSFO(t *testing.T) {
	if got := SerialFromSFO(sfoFixture("SLUS00594")); got != "SLUS-00594" {
		t.Errorf("got %q", got)
	}
}

func sfoFixture(discID string) []byte {
	keys := []byte("CATEGORY\x00DISC_ID\x00")
	val := append([]byte(discID), 0)
	head := make([]byte, 20+2*16)
	copy(head, "\x00PSF")
	binary.LittleEndian.PutUint32(head[4:], 0x101)
	binary.LittleEndian.PutUint32(head[8:], uint32(len(head)))
	binary.LittleEndian.PutUint32(head[12:], uint32(len(head)+len(keys)))
	binary.LittleEndian.PutUint32(head[16:], 2)
	// CATEGORY → "ME\0"; DISC_ID → discID.
	e := head[20:]
	binary.LittleEndian.PutUint16(e[0:], 0)
	binary.LittleEndian.PutUint32(e[4:], 3)
	binary.LittleEndian.PutUint32(e[12:], 0)
	e = head[36:]
	binary.LittleEndian.PutUint16(e[0:], 9)
	binary.LittleEndian.PutUint32(e[4:], uint32(len(val)))
	binary.LittleEndian.PutUint32(e[12:], 3)
	out := append(head, keys...)
	out = append(out, []byte("ME\x00")...)
	return append(out, val...)
}

func TestReadPBP(t *testing.T) {
	sfo := sfoFixture("SCUS94163")
	head := make([]byte, 0x28)
	copy(head, "\x00PBP")
	binary.LittleEndian.PutUint32(head[8:], 0x28)
	binary.LittleEndian.PutUint32(head[12:], uint32(0x28+len(sfo)))
	path := filepath.Join(t.TempDir(), "Game.pbp")
	write(t, path, append(head, sfo...))
	r, err := Read(path, "ps1")
	if err != nil || r.Serial != "SCUS-94163" {
		t.Errorf("got %+v, %v", r, err)
	}
}

// A zip is hashed by the ROM inside it, not the readme beside it, and a zip
// whose folder named no console takes its platform from that file.
func TestReadZipHashesTheROMInside(t *testing.T) {
	z := n64Fixture()
	path := filepath.Join(t.TempDir(), "Game.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range map[string][]byte{
		"readme.txt":               bytes.Repeat([]byte("x"), 500),
		"Super Mario 64 (USA).v64": toV64(z),
	} {
		w, _ := zw.Create(name)
		w.Write(body)
	}
	zw.Close()
	f.Close()

	r, err := Read(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Platform != "n64" || r.InnerName != "Super Mario 64 (USA).v64" {
		t.Errorf("got platform %q inner %q", r.Platform, r.InnerName)
	}
	if len(r.Sums) == 0 || r.Sums[0] != Of(z) {
		t.Errorf("zip hash %+v, want the z64 hash %+v", r.Sums, Of(z))
	}
}

/*
 * A zip filed under the wrong console is read as the console the file inside
 * names. Found on a real library: a GBA dump zipped into "nes roms" was hashed
 * and looked up as NES. Here an N64 dump in byte-swapped .v64 order is read
 * with "nes" from its folder; only N64's rules turn it back into the z64 hash
 * the DAT lists, so the hash itself proves which console's rules ran.
 */
func TestAZipsInnerFileOverrulesItsFolder(t *testing.T) {
	z := n64Fixture()
	path := filepath.Join(t.TempDir(), "Game.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("Super Mario 64 (USA).v64")
	w.Write(toV64(z))
	zw.Close()
	f.Close()

	r, err := Read(path, "nes")
	if err != nil {
		t.Fatal(err)
	}
	if r.Platform != "n64" {
		t.Errorf("platform %q, want the n64 the file inside names", r.Platform)
	}
	if len(r.Sums) == 0 || r.Sums[0] != Of(z) {
		t.Errorf("hashed under the folder's console: %+v", r.Sums)
	}
}

func TestReadCartridge(t *testing.T) {
	z := n64Fixture()
	path := filepath.Join(t.TempDir(), "Game.n64")
	write(t, path, toN64(z))
	r, err := Read(path, "n64")
	if err != nil || len(r.Sums) != 1 || r.Sums[0] != Of(z) {
		t.Errorf("got %+v, %v", r, err)
	}
}

// Of writes hex the way a DAT does: eight upper-case digits for the CRC,
// leading zeros kept.
func TestOfFormat(t *testing.T) {
	s := Of([]byte("a"))
	if s.CRC32 != "E8B7BE43" || s.SHA1 != "86F7E437FAA5A7FCE15D1DDCB9EAEAEA377667B8" {
		t.Errorf("got %+v", s)
	}
	if len(Of(nil).CRC32) != 8 {
		t.Error("CRC not zero-padded")
	}
}

func write(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// An .m3u names its discs relative to itself. Only disc images inside its own
// folder count: a list cannot reach out of the game's folder, and a line that
// is not a disc is not one.
func TestM3UDiscs(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "Game")
	hidden := filepath.Join(dir, ".hidden")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}
	m3u := filepath.Join(dir, "Game (USA).m3u")
	write(t, m3u, []byte("#EXTM3U\n.hidden\\Game (USA) (Disc 1).cue\n.hidden/Game (USA) (Disc 2).cue\n..\\Elsewhere.cue\nreadme.txt\n"))
	discs, err := M3UDiscs(m3u)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(hidden, "Game (USA) (Disc 1).cue"),
		filepath.Join(hidden, "Game (USA) (Disc 2).cue"),
	}
	if len(discs) != 2 || discs[0] != want[0] || discs[1] != want[1] {
		t.Errorf("discs = %v, want %v", discs, want)
	}
}

// A multi-disc game is identified by its first disc's serial, and takes its
// platform from that disc when its folder said nothing.
func TestReadM3UFollowsTheFirstDisc(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "Game (Disc 1) (Track 1).bin"), rawMode2(isoWithCNF(fixtureCNF)))
	write(t, filepath.Join(dir, "Game (Disc 1).cue"), []byte("FILE \"Game (Disc 1) (Track 1).bin\" BINARY\n"))
	m3u := filepath.Join(dir, "Game.m3u")
	write(t, m3u, []byte("Game (Disc 1).cue\nGame (Disc 2).cue\n"))
	r, err := Read(m3u, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Serial != "SLUS-00594" || r.Platform != "ps1" || r.InnerName != "Game (Disc 1).cue" {
		t.Errorf("got %+v", r)
	}
}
