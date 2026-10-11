package webcores

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

	"lancast/internal/retro/cores"
)

// The browser plays exactly the consoles ADR 0077 decided, each with a core
// whose licence the server-fetch reading covers, and none of the cores that
// withhold commercial use.
func TestTheDecidedConsolesAndNoOthers(t *testing.T) {
	for _, p := range []string{"nes", "gb", "gbc", "gba", "sms", "genesis"} {
		if _, ok := For(p); !ok {
			t.Errorf("%s has no web core", p)
		}
	}
	// SNES by licence, N64 and PlayStation by decision.
	for _, p := range []string{"snes", "n64", "ps1"} {
		if c, ok := For(p); ok {
			t.Errorf("%s plays in the browser with %s; ADR 0077 says desktop only", p, c.Name)
		}
	}
	for _, c := range All() {
		switch c.Licence {
		case "GPL-2.0", "GPL-3.0", "AGPL-3.0", "MPL-2.0":
		default:
			t.Errorf("%s is %s, outside the licences ADR 0077 accepted", c.Name, c.Licence)
		}
		for _, nc := range []string{"snes9x", "genesis_plus_gx", "picodrive"} {
			if strings.HasPrefix(c.Name, nc) {
				t.Errorf("%s withholds commercial use and must not be listed", c.Name)
			}
		}
		for _, f := range c.Files() {
			if len(f.SHA256) != 64 || f.SizeBytes <= 0 || !strings.HasPrefix(f.ArchivePath, inArchive) {
				t.Errorf("%s: %s is not pinned", c.Name, f.Label)
			}
		}
		if c.SourceURL == "" {
			t.Errorf("%s has no source to offer", c.Name)
		}
	}
	if !strings.HasPrefix(Archive.URL, "https://buildbot.libretro.com/stable/"+Archive.Version+"/emscripten/") {
		t.Errorf("the archive is not libretro's stable emscripten build: %s", Archive.URL)
	}
	if Archive.Version != cores.Stable.Version {
		t.Errorf("web cores pin %s, desktop cores pin %s: keep them on one release", Archive.Version, cores.Stable.Version)
	}
}

// fixture repins every core to the tiny archive in testdata and serves it.
func fixture(t *testing.T) (cores.Archive, []Core, *atomic.Int32) {
	t.Helper()
	raw, err := os.ReadFile("testdata/webcores.7z")
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write(raw)
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(raw)
	a := cores.Archive{URL: srv.URL + "/RetroArch.7z", SHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(raw)), Version: "test"}

	pin := func(f cores.File, content string) cores.File {
		s := sha256.Sum256([]byte(content))
		f.SHA256, f.SizeBytes = hex.EncodeToString(s[:]), int64(len(content))
		return f
	}
	var want []Core
	for _, c := range all {
		c.JS = pin(c.JS, "the "+c.Name+" js")
		c.Wasm = pin(c.Wasm, "the "+c.Name+" wasm module")
		want = append(want, c)
	}
	return a, want, &hits
}

func TestInstallTakesTheFourCoresAndNothingElse(t *testing.T) {
	a, want, hits := fixture(t)
	dir := t.TempDir()
	if err := install(context.Background(), a, dir, want, nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range want {
		if !c.Present(dir) {
			t.Errorf("%s is not present", c.Name)
		}
		b, _ := os.ReadFile(filepath.Join(dir, c.Wasm.Dest))
		if string(b) != "the "+c.Name+" wasm module" {
			t.Errorf("%s installed as %q", c.Name, b)
		}
	}
	// The archive holds snes9x too. Nothing outside the list reaches the disk.
	if _, err := os.Stat(filepath.Join(dir, "snes9x")); !os.IsNotExist(err) {
		t.Error("snes9x was extracted")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() {
			t.Errorf("left behind: %s", e.Name())
		}
	}
	// Installed and right: nothing is fetched again.
	if err := install(context.Background(), a, dir, want, nil); err != nil || hits.Load() != 1 {
		t.Errorf("second install: %v, %d downloads", err, hits.Load())
	}
}

// A file that fails its own pin is not placed, though the archive was right.
func TestInstallRefusesAFileThatFailsItsPin(t *testing.T) {
	a, want, _ := fixture(t)
	want[0].Wasm.SHA256 = strings.Repeat("0", 64)
	dir := t.TempDir()
	if err := install(context.Background(), a, dir, want, nil); !errors.Is(err, cores.ErrChecksumMismatch) {
		t.Fatalf("err = %v", err)
	}
	if want[0].Present(dir) {
		t.Error("a core whose module failed its checksum counts as present")
	}
}

// The only way a request's words become a path.
func TestPathOnlyNamesInstalledPinnedFiles(t *testing.T) {
	a, want, _ := fixture(t)
	dir := t.TempDir()

	// Before install, nothing is served.
	if _, _, ok := Path(dir, "fceumm", "wasm"); ok {
		t.Error("a file was served before it was installed")
	}

	// Install, then repin the real table to the fixture's sizes so Path,
	// which reads it, agrees with what is on disk.
	if err := install(context.Background(), a, dir, want, nil); err != nil {
		t.Fatal(err)
	}
	saved := all
	all = want
	t.Cleanup(func() { all = saved })

	if p, f, ok := Path(dir, "fceumm", "wasm"); !ok || filepath.Base(p) != "fceumm_libretro.wasm" || f.SizeBytes == 0 {
		t.Errorf("fceumm wasm: %q %v", p, ok)
	}
	for _, bad := range [][2]string{
		{"snes9x", "wasm"},          // not listed
		{"fceumm", "dll"},           // not a kind
		{"../fceumm", "js"},         // not a name
		{"fceumm", "../../secrets"}, // not a kind
		{"", "js"},
	} {
		if p, _, ok := Path(dir, bad[0], bad[1]); ok {
			t.Errorf("Path(%q, %q) = %q", bad[0], bad[1], p)
		}
	}

	// A file of the wrong size is not present and not served.
	os.WriteFile(filepath.Join(dir, want[0].JS.Dest), []byte("short"), 0o644)
	if want[0].Present(dir) {
		t.Error("a truncated loader counts as present")
	}
	if _, _, ok := Path(dir, "fceumm", "js"); ok {
		t.Error("a truncated loader was served")
	}
}

func TestConsolesListsEachOnce(t *testing.T) {
	got := strings.Join(Consoles(), ",")
	if got != "gb,gba,gbc,genesis,nes,sms" {
		t.Errorf("Consoles() = %s", got)
	}
}
