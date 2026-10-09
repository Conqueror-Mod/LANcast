package cores

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Every console the library recognises has a default core, every default is
// GPL or more permissive, and none is one of the non-commercial cores.
func TestDefaults(t *testing.T) {
	for _, p := range []string{"nes", "snes", "n64", "gb", "gbc", "gba", "sms", "genesis", "ps1"} {
		c, ok := For(p)
		if !ok {
			t.Errorf("%s has no core", p)
			continue
		}
		if !strings.HasPrefix(c.Licence, "GPL-") && c.Licence != "MPL-2.0" {
			t.Errorf("%s: %s is %s", p, c.Name, c.Licence)
		}
		for _, nc := range []string{"snes9x", "genesis_plus_gx", "picodrive"} {
			if c.Name == nc {
				t.Errorf("%s defaults to non-commercial %s", p, nc)
			}
		}
		if c.SourceURL == "" || !strings.HasSuffix(c.DLL, "_libretro.dll") {
			t.Errorf("%s: incomplete entry %+v", p, c)
		}
		if !c.Pinned() || c.ArchivePath != inArchive+c.DLL {
			t.Errorf("%s: %s is not pinned to its place in the archive", p, c.Name)
		}
	}
	if c, _ := For("n64"); !c.NeedsGL {
		t.Error("the N64 core is not marked as needing OpenGL")
	}
	if c, _ := For("ps1"); len(c.NeedsBIOS) == 0 {
		t.Error("the PlayStation core does not name its BIOS")
	}
	if !strings.HasPrefix(Stable.URL, "https://buildbot.libretro.com/stable/"+Stable.Version+"/") {
		t.Errorf("the archive is not libretro's stable %s: %s", Stable.Version, Stable.URL)
	}
}

// mGBA plays three consoles and is fetched once.
func TestAllIsEachCoreOnce(t *testing.T) {
	names := map[string]int{}
	for _, c := range All() {
		names[c.Name]++
	}
	if len(names) != 7 || names["mgba"] != 1 {
		t.Errorf("All() = %v", names)
	}
}

// An override wins over the installed default; a missing override is an
// error that names where it looked, not a silent fallback.
func TestResolve(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := Resolve("gba", dir, nil); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("nothing installed: %v", err)
	}
	mine := filepath.Join(t.TempDir(), "my_mgba.dll")
	os.WriteFile(mine, []byte("x"), 0o644)
	p, c, err := Resolve("gba", dir, map[string]string{"gba": mine})
	if err != nil || p != mine || c.Name != "mgba" {
		t.Errorf("override: %q %v %v", p, c.Name, err)
	}
	if _, _, err := Resolve("gba", dir, map[string]string{"gba": filepath.Join(dir, "nope.dll")}); err == nil || errors.Is(err, ErrNotInstalled) {
		t.Errorf("a missing override: %v", err)
	}
	gba, _ := For("gba")
	os.MkdirAll(filepath.Dir(gba.Path(dir)), 0o755)
	// A DLL of another size is a build some other pin put there.
	os.WriteFile(gba.Path(dir), []byte("dll"), 0o644)
	if _, _, err := Resolve("gb", dir, nil); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("a stale build resolved: %v", err)
	}
	os.WriteFile(gba.Path(dir), make([]byte, gba.SizeBytes), 0o644)
	if p, _, err := Resolve("gb", dir, nil); err != nil || p != gba.Path(dir) {
		t.Errorf("installed: %q %v (gb shares mGBA)", p, err)
	}
	if _, _, err := Resolve("dreamcast", dir, nil); err == nil {
		t.Error("an unknown console resolved")
	}
}

/*
 * The fixture is a solid 7z built by 7-Zip itself, laid out as the stable
 * archive is: two default cores and one that is not, under
 * RetroArch-Win64/cores/.
 */
func fixture(t *testing.T) (Archive, []Core, *atomic.Int32) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "cores.7z"))
	if err != nil {
		t.Fatal(err)
	}
	hits := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	a := Archive{URL: srv.URL, SHA256: sum([]byte(body)), SizeBytes: int64(len(body)), Version: "test"}
	pin := func(c Core, content string) Core {
		c.SHA256, c.SizeBytes = sum([]byte(content)), int64(len(content))
		return c
	}
	mesen, _ := For("nes")
	return a, []Core{pin(mgba, "the mgba core"), pin(mesen, "the mesen core")}, hits
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestInstallAll(t *testing.T) {
	a, want, hits := fixture(t)
	dir := t.TempDir()
	var stages []string
	err := InstallAll(context.Background(), a, dir, want, func(p Progress) {
		if len(stages) == 0 || stages[len(stages)-1] != p.Stage {
			stages = append(stages, p.Stage)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range want {
		b, _ := os.ReadFile(c.Path(dir))
		if string(b) != "the "+c.Name+" core" {
			t.Errorf("%s installed as %q", c.Name, b)
		}
	}
	if strings.Join(stages, ",") != "download,unpack,done" {
		t.Errorf("stages %v", stages)
	}
	if _, err := os.Stat(filepath.Join(dir, "snes9x")); !os.IsNotExist(err) {
		t.Error("a core nobody asked for was unpacked")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() {
			t.Errorf("left behind: %s", e.Name())
		}
	}

	// Installed and right: nothing is fetched again.
	if err := InstallAll(context.Background(), a, dir, want, nil); err != nil || hits.Load() != 1 {
		t.Errorf("second install: %v, %d downloads", err, hits.Load())
	}
	// One core changed under it: the archive is fetched for that one.
	os.WriteFile(want[1].Path(dir), []byte("tampered core!"), 0o644)
	if err := InstallAll(context.Background(), a, dir, want, nil); err != nil || hits.Load() != 2 {
		t.Errorf("repair: %v, %d downloads", err, hits.Load())
	}
	if b, _ := os.ReadFile(want[1].Path(dir)); string(b) != "the mesen core" {
		t.Errorf("repaired as %q", b)
	}
}

// An archive that fails its checksum is never opened.
func TestInstallAllRefusesABadArchive(t *testing.T) {
	a, want, _ := fixture(t)
	a.SHA256 = strings.Repeat("0", 64)
	dir := t.TempDir()
	if err := InstallAll(context.Background(), a, dir, want, nil); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("err = %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		t.Errorf("left behind: %s", e.Name())
	}
}

// A DLL that fails its own pin is not placed, though the archive was right.
func TestInstallAllRefusesABadCore(t *testing.T) {
	a, want, _ := fixture(t)
	want[1].SHA256 = strings.Repeat("0", 64)
	dir := t.TempDir()
	if err := InstallAll(context.Background(), a, dir, want, nil); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(want[1].Path(dir)); !os.IsNotExist(err) {
		t.Error("a core that failed its checksum was installed")
	}
	left, _ := filepath.Glob(filepath.Join(dir, "*", "*.tmp"))
	part, _ := filepath.Glob(filepath.Join(dir, "*.part"))
	if len(left)+len(part) > 0 {
		t.Errorf("left behind: %v %v", left, part)
	}
}

// A core the archive does not hold is an error naming it, not a quiet skip.
func TestInstallAllNamesAMissingCore(t *testing.T) {
	a, want, _ := fixture(t)
	bsnes, _ := For("snes")
	bsnes.SHA256, bsnes.SizeBytes = strings.Repeat("1", 64), 4
	err := InstallAll(context.Background(), a, t.TempDir(), append(want, bsnes), nil)
	if err == nil || !strings.Contains(err.Error(), "bsnes_libretro.dll") {
		t.Errorf("err = %v", err)
	}
}

// A download left by a client that was killed is swept, not kept for ever.
func TestInstallAllSweepsAKilledDownload(t *testing.T) {
	a, want, _ := fixture(t)
	dir := t.TempDir()
	stale := filepath.Join(dir, "download-123.part")
	os.WriteFile(stale, []byte("half"), 0o644)
	if err := InstallAll(context.Background(), a, dir, want, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("the killed download is still there")
	}
}

func TestInstallAllRefusesAnUnpinnedCore(t *testing.T) {
	a, want, hits := fixture(t)
	want[0].SHA256 = ""
	if err := InstallAll(context.Background(), a, t.TempDir(), want, nil); !errors.Is(err, ErrNotPinned) || hits.Load() != 0 {
		t.Errorf("err = %v, %d downloads", err, hits.Load())
	}
}
