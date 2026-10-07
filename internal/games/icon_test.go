package games

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

/*
 * A game's own picture (icon.go): the ICO directory, both image forms an icon
 * entry takes, the executable's resources, and where each launcher's picture
 * is found. Fixtures are built here, byte by byte, as the formats lay them out.
 */

func pngOf(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// dib32 is a BITMAPINFOHEADER and bottom-up BGRA pixels, the top row red and
// the rest blue, with an AND mask after them.
func dib32(w, h int) []byte {
	var b bytes.Buffer
	hdr := make([]byte, 40)
	binary.LittleEndian.PutUint32(hdr[0:], 40)
	binary.LittleEndian.PutUint32(hdr[4:], uint32(w))
	binary.LittleEndian.PutUint32(hdr[8:], uint32(h*2))
	binary.LittleEndian.PutUint16(hdr[12:], 1)
	binary.LittleEndian.PutUint16(hdr[14:], 32)
	b.Write(hdr)
	for y := h - 1; y >= 0; y-- { // bottom-up
		for x := 0; x < w; x++ {
			if y == 0 {
				b.Write([]byte{0, 0, 255, 255}) // red
			} else {
				b.Write([]byte{255, 0, 0, 255}) // blue
			}
		}
	}
	b.Write(make([]byte, ((w+31)/32)*4*h))
	return b.Bytes()
}

// ico builds an .ico file holding the given images.
func ico(entries []iconEntry) []byte {
	var head, body bytes.Buffer
	binary.Write(&head, binary.LittleEndian, [3]uint16{0, 1, uint16(len(entries))})
	off := 6 + 16*len(entries)
	for _, e := range entries {
		w, h := byte(e.w), byte(e.h)
		if e.w >= 256 {
			w = 0
		}
		if e.h >= 256 {
			h = 0
		}
		head.Write([]byte{w, h, 0, 0})
		binary.Write(&head, binary.LittleEndian, uint16(1))
		binary.Write(&head, binary.LittleEndian, uint16(e.bpp))
		binary.Write(&head, binary.LittleEndian, uint32(len(e.data)))
		binary.Write(&head, binary.LittleEndian, uint32(off+body.Len()))
		body.Write(e.data)
	}
	return append(head.Bytes(), body.Bytes()...)
}

func decodePNG(t *testing.T, raw []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	return img
}

func TestIcoPrefersTheLargestImage(t *testing.T) {
	small := pngOf(t, 16, 16, color.NRGBA{255, 0, 0, 255})
	big := pngOf(t, 256, 256, color.NRGBA{0, 255, 0, 255})
	out, err := icoBest(ico([]iconEntry{{w: 16, h: 16, bpp: 32, data: small}, {w: 256, h: 256, bpp: 32, data: big}}))
	if err != nil {
		t.Fatal(err)
	}
	if b := decodePNG(t, out).Bounds(); b.Dx() != 256 {
		t.Errorf("picked %dpx, want the 256px image", b.Dx())
	}
}

// A 32-bit bitmap entry is decoded right way up, with its colours.
func TestIcoDecodesABitmapEntry(t *testing.T) {
	out, err := icoBest(ico([]iconEntry{{w: 4, h: 4, bpp: 32, data: dib32(4, 4)}}))
	if err != nil {
		t.Fatal(err)
	}
	img := decodePNG(t, out)
	r, _, b, a := img.At(0, 0).RGBA()
	if r>>8 != 255 || b != 0 || a>>8 != 255 {
		t.Errorf("top-left = %v, want red: the rows are stored bottom-up", img.At(0, 0))
	}
	r, _, b, _ = img.At(0, 3).RGBA()
	if r != 0 || b>>8 != 255 {
		t.Errorf("bottom-left = %v, want blue", img.At(0, 3))
	}
}

// Garbage is "no icon", never a crash.
func TestIcoRejectsWhatIsNotAnIcon(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte("not an icon"), ico([]iconEntry{{w: 4, h: 4, bpp: 8, data: []byte{1, 2, 3}}})} {
		if _, err := icoBest(raw); err == nil {
			t.Errorf("icoBest(%q) gave an icon", raw)
		}
	}
}

// A real executable's icon, from its resources. Windows only: CI on Linux has
// no PE file to read.
func TestExeIconFromARealExecutable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("needs a Windows executable")
	}
	exe := filepath.Join(os.Getenv("SystemRoot"), "explorer.exe")
	out, err := IconPNG(exe)
	if err != nil {
		t.Fatalf("explorer.exe: %v", err)
	}
	if b := decodePNG(t, out).Bounds(); b.Dx() < 32 {
		t.Errorf("icon is %dpx, want a real one", b.Dx())
	}
}

func TestIconTargetIsContained(t *testing.T) {
	dir := t.TempDir()
	game := filepath.Join(dir, "Game")
	writeFile(t, filepath.Join(game, "logo.png"), "x")
	writeFile(t, filepath.Join(dir, "elsewhere.png"), "x")
	g := Game{InstallPath: game, IconSource: filepath.Join(game, "logo.png")}
	if _, err := IconTarget(g); err != nil {
		t.Errorf("a contained icon was refused: %v", err)
	}
	for _, src := range []string{filepath.Join(dir, "elsewhere.png"), filepath.Join(game, "..", "elsewhere.png"), filepath.Join(game, "missing.png"), ""} {
		g.IconSource = src
		if _, err := IconTarget(g); err == nil {
			t.Errorf("accepted %q", src)
		}
	}
}

func TestEachLauncherNamesItsOwnPicture(t *testing.T) {
	// GOG: its .ico over its executable.
	gdir := filepath.Join(t.TempDir(), "Coromon")
	writeFile(t, filepath.Join(gdir, "goggame-1950069341.info"), coromonInfo)
	writeFile(t, filepath.Join(gdir, "goggame-1950069341.ico"), "ico")
	if g := ScanGOG([]GOGInstall{{GameID: "1950069341", Path: gdir}}).Games[0]; g.IconSource != filepath.Join(gdir, "goggame-1950069341.ico") {
		t.Errorf("GOG icon = %q, want its .ico", g.IconSource)
	}

	// EA: the anti-cheat splash, which is the game's key art, over the exe.
	edir := filepath.Join(t.TempDir(), "Skate")
	writeFile(t, filepath.Join(edir, "__Installer", "installerdata.xml"), skateManifest)
	writeFile(t, filepath.Join(edir, "EAAntiCheat.Splash.png"), "png")
	ea := EAGames([]InstalledProgram{{Publisher: "Electronic Arts", InstallLocation: edir,
		DisplayIcon: filepath.Join(edir, "Skate.exe")}}).Games[0]
	if ea.IconSource != filepath.Join(edir, "EAAntiCheat.Splash.png") {
		t.Errorf("EA icon = %q, want the splash", ea.IconSource)
	}

	// Xbox: the 480 logo the config names; a scaled variant when the plain
	// name is not shipped.
	content := filepath.Join(t.TempDir(), "Content")
	cfg := []byte(`<Game><ShellVisuals Square480x480Logo="LargeLogo.png" Square150x150Logo="Logo.png"/></Game>`)
	writeFile(t, filepath.Join(content, "LargeLogo.scale-100.png"), "small")
	writeFile(t, filepath.Join(content, "LargeLogo.scale-200.png"), "much bigger file")
	if got := xboxLogo(content, cfg); got != filepath.Join(content, "LargeLogo.scale-200.png") {
		t.Errorf("Xbox logo = %q, want the largest scaled variant", got)
	}
	writeFile(t, filepath.Join(content, "LargeLogo.png"), "plain")
	if got := xboxLogo(content, cfg); got != filepath.Join(content, "LargeLogo.png") {
		t.Errorf("Xbox logo = %q, want the plain name when it is shipped", got)
	}
}
