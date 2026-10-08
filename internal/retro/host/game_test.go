package host

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/retro/libretro"
)

/*
 * Which file a core is handed. Every case here was a real game on
 * 2026-10-08: a zipped GBA, NES, N64 or Genesis dump refused by its core (or,
 * for BlastEm, crashing the client), while a zipped SNES dump played because
 * bsnes opens zips itself.
 */

func zipWith(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "game.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return p
}

var mgba = libretro.SystemInfo{LibraryName: "mGBA", ValidExtensions: "gba|gb|gbc|sgb"}

func TestAZipIsExtractedForACoreThatCannotOpenOne(t *testing.T) {
	zp := zipWith(t, map[string]string{"readme.txt": "no", "Pokemon - Emerald.gba": "ROM"})
	dir := t.TempDir()
	got, err := GameFile(zp, mgba, dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(got) != dir || filepath.Base(got) != "Pokemon - Emerald.gba" {
		t.Fatalf("handed %s", got)
	}
	if b, _ := os.ReadFile(got); string(b) != "ROM" {
		t.Fatalf("extracted %q", b)
	}
}

func TestAZipGoesWholeToACoreThatOpensZips(t *testing.T) {
	zp := zipWith(t, map[string]string{"Bubsy.sfc": "ROM"})
	got, err := GameFile(zp, libretro.SystemInfo{ValidExtensions: "smc|sfc|zip"}, t.TempDir())
	if err != nil || got != zp {
		t.Fatalf("got %s, %v; want the zip itself", got, err)
	}
}

func TestBlockExtractKeepsTheArchiveWhole(t *testing.T) {
	zp := zipWith(t, map[string]string{"x.bin": "ROM"})
	got, err := GameFile(zp, libretro.SystemInfo{ValidExtensions: "bin", BlockExtract: true}, t.TempDir())
	// Blocking extraction while not listing zip means the core asked for
	// something it says it cannot open; refusing beats finding out by crash.
	if err == nil || got != "" {
		t.Fatalf("got %s, %v; want a refusal, since the core does not open .zip", got, err)
	}
	got, err = GameFile(zp, libretro.SystemInfo{ValidExtensions: "bin|zip", BlockExtract: true}, t.TempDir())
	if err != nil || got != zp {
		t.Fatalf("got %s, %v", got, err)
	}
}

func TestAFileTheCoreNeverClaimedIsRefusedNotLoaded(t *testing.T) {
	p := filepath.Join(t.TempDir(), "Sonic 3.7z")
	os.WriteFile(p, []byte("x"), 0o644)
	_, err := GameFile(p, libretro.SystemInfo{LibraryName: "BlastEm", ValidExtensions: "md|bin|smd|gen"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "BlastEm cannot open a .7z file") || !strings.Contains(err.Error(), ".md, .bin") {
		t.Fatalf("got %v", err)
	}
}

func TestAPlainRomPassesStraightThrough(t *testing.T) {
	p := filepath.Join(t.TempDir(), "Zelda.NES")
	os.WriteFile(p, []byte("x"), 0o644)
	got, err := GameFile(p, libretro.SystemInfo{ValidExtensions: "nes|fds"}, t.TempDir())
	if err != nil || got != p {
		t.Fatalf("got %s, %v", got, err)
	}
}

func TestAZipWithNothingTheCoreOpensSaysSo(t *testing.T) {
	zp := zipWith(t, map[string]string{"manual.pdf": "x"})
	if _, err := GameFile(zp, mgba, t.TempDir()); err == nil || !strings.Contains(err.Error(), "nothing this console's core can open") {
		t.Fatalf("got %v", err)
	}
}

func TestANameInsideTheZipCannotEscapeTheDirectory(t *testing.T) {
	zp := zipWith(t, map[string]string{`..\..\evil.gba`: "ROM"})
	dir := filepath.Join(t.TempDir(), "x")
	got, err := GameFile(zp, mgba, dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(got) != dir {
		t.Fatalf("extracted to %s, outside %s", got, dir)
	}
}

func TestPlayingAgainDoesNotExtractAgain(t *testing.T) {
	zp := zipWith(t, map[string]string{"g.gba": "ROM"})
	dir := t.TempDir()
	first, err := GameFile(zp, mgba, dir)
	if err != nil {
		t.Fatal(err)
	}
	// Same size, different bytes: only a reuse leaves these in place.
	os.WriteFile(first, []byte("OLD"), 0o644)
	again, err := GameFile(zp, mgba, dir)
	if err != nil || again != first {
		t.Fatalf("got %s, %v", again, err)
	}
	if b, _ := os.ReadFile(again); string(b) != "OLD" {
		t.Fatal("extracted again")
	}
}
