package cores

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	}
	if c, _ := For("n64"); !c.NeedsGL {
		t.Error("the N64 core is not marked as needing OpenGL")
	}
	if c, _ := For("ps1"); len(c.NeedsBIOS) == 0 {
		t.Error("the PlayStation core does not name its BIOS")
	}
}

func TestUnpinnedCoreIsNotFetched(t *testing.T) {
	c, _ := For("gba")
	if c.Pinned() {
		t.Skip("a build has been pinned; this test is about the state before that")
	}
	if err := Install(context.Background(), c, t.TempDir(), nil); !errors.Is(err, ErrNotPinned) {
		t.Errorf("err = %v, want ErrNotPinned", err)
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
	os.WriteFile(gba.Path(dir), []byte("dll"), 0o644)
	if p, _, err := Resolve("gb", dir, nil); err != nil || p != gba.Path(dir) {
		t.Errorf("installed: %q %v (gb shares mGBA)", p, err)
	}
	if _, _, err := Resolve("dreamcast", dir, nil); err == nil {
		t.Error("an unknown console resolved")
	}
}

func zipWith(t *testing.T, name string, body []byte) []byte {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	w, _ := zw.Create(name)
	w.Write(body)
	zw.Close()
	return b.Bytes()
}

// A pinned build is verified before it is unpacked, and a mismatch leaves
// nothing under the real name.
func TestInstall(t *testing.T) {
	good := zipWith(t, "mgba_libretro.dll", []byte("the core"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(good) }))
	defer srv.Close()
	sum := sha256.Sum256(good)
	c := mgba
	c.URL, c.SHA256, c.SizeBytes = srv.URL, hex.EncodeToString(sum[:]), int64(len(good))

	dir := t.TempDir()
	var last int64
	if err := Install(context.Background(), c, dir, func(d, _ int64) { last = d }); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(c.Path(dir)); string(b) != "the core" || last != int64(len(good)) {
		t.Errorf("installed %q, progress %d", b, last)
	}

	bad := c
	bad.SHA256 = strings.Repeat("0", 64)
	dir2 := t.TempDir()
	if err := Install(context.Background(), bad, dir2, nil); !errors.Is(err, ErrChecksumMismatch) {
		t.Errorf("err = %v", err)
	}
	if _, err := os.Stat(bad.Path(dir2)); !os.IsNotExist(err) {
		t.Error("a build that failed its checksum was unpacked")
	}
	entries, _ := os.ReadDir(filepath.Dir(bad.Path(dir2)))
	for _, e := range entries {
		t.Errorf("left behind: %s", e.Name())
	}
}
