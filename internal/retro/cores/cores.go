// Package cores decides which emulator core plays each console, and fetches
// it (ADR 0073).
//
// Cores are fetched, never bundled: the N64 and PlayStation cores are GPL, and
// a core in LANcast's own installer would be a GPL work inside a commercially
// licensed one (ADR 0053). Fetching follows ADR 0043 in every respect: a
// pinned URL with a recorded SHA-256, checked before anything is unpacked, a
// partial install counting as absent, and nothing fetched without a person
// asking.
//
// # The pin is not filled in yet, on purpose
//
// libretro's buildbot cannot be pinned per core. Per-core zips exist only
// under nightly/…/latest/, rebuilt every night, so a recorded checksum would
// stop verifying by the next morning. Stable releases ship every core in one
// 230MB .7z, which the standard library cannot open, to deliver one 1MB DLL.
// The ADR's fallback is a mirror LANcast controls, which means publishing
// GPL binaries somewhere with their source beside them — a decision for the
// project's owner, not something to do quietly. Until a URL and checksum are
// recorded here, Install refuses with ErrNotPinned, and a person may point a
// console at a core DLL they already have (Resolve's override), which is how
// the player is tested in the meantime.
package cores

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Core is one emulator core and where its pinned build comes from.
type Core struct {
	Name      string // libretro's name: "mgba", "mesen", …
	Display   string // "mGBA"
	Licence   string // SPDX id, checked against the upstream repository
	SourceURL string // the source a GPL binary must be offered with
	DLL       string // the file inside the zip
	URL       string // the pinned zip, or "" until one is chosen
	SHA256    string
	SizeBytes int64
	Version   string
	// NeedsGL marks a core that renders through OpenGL. The stage 2 player
	// draws framebuffers only; these play from stage 3.
	NeedsGL bool
	// NeedsBIOS names the BIOS files a core needs from the person, by file
	// name, in the system directory. LANcast never supplies one.
	NeedsBIOS []string
}

/*
 * Defaults: one core per console, chosen for accuracy and for a licence a
 * commercially licensed LANcast could at least point to — GPL or more
 * permissive. Non-commercial cores (Snes9x, Genesis Plus GX, PicoDrive) are
 * never defaults. Licences were read from each upstream repository on
 * 2026-10-08.
 */
var defaults = map[string]Core{
	"gb":      mgba,
	"gbc":     mgba,
	"gba":     mgba,
	"nes":     {Name: "mesen", Display: "Mesen", Licence: "GPL-3.0", SourceURL: "https://github.com/libretro/Mesen", DLL: "mesen_libretro.dll"},
	"snes":    {Name: "bsnes", Display: "bsnes", Licence: "GPL-3.0", SourceURL: "https://github.com/libretro/bsnes-libretro", DLL: "bsnes_libretro.dll"},
	"genesis": {Name: "blastem", Display: "BlastEm", Licence: "GPL-3.0", SourceURL: "https://github.com/libretro/blastem", DLL: "blastem_libretro.dll"},
	"sms":     {Name: "gearsystem", Display: "Gearsystem", Licence: "GPL-3.0", SourceURL: "https://github.com/drhelius/Gearsystem", DLL: "gearsystem_libretro.dll"},
	"n64":     {Name: "mupen64plus_next", Display: "Mupen64Plus-Next", Licence: "GPL-2.0", SourceURL: "https://github.com/libretro/mupen64plus-libretro-nx", DLL: "mupen64plus_next_libretro.dll", NeedsGL: true},
	"ps1": {Name: "swanstation", Display: "SwanStation", Licence: "GPL-3.0", SourceURL: "https://github.com/libretro/swanstation", DLL: "swanstation_libretro.dll",
		NeedsBIOS: []string{"scph5501.bin", "scph5500.bin", "scph5502.bin"}},
}

var mgba = Core{Name: "mgba", Display: "mGBA", Licence: "MPL-2.0", SourceURL: "https://github.com/libretro/mgba", DLL: "mgba_libretro.dll"}

// For returns the default core for a console.
func For(platform string) (Core, bool) {
	c, ok := defaults[platform]
	return c, ok
}

// Pinned reports whether a core has a build this release can fetch.
func (c Core) Pinned() bool { return c.URL != "" && len(c.SHA256) == 64 && c.SizeBytes > 0 }

var (
	ErrNotPinned        = errors.New("no pinned build of this core yet")
	ErrChecksumMismatch = errors.New("the download did not match its expected checksum")
	ErrNotInstalled     = errors.New("this console's core is not installed")
)

// Path is where an installed core lives under dir.
func (c Core) Path(dir string) string { return filepath.Join(dir, c.Name, c.DLL) }

/*
 * Resolve finds the DLL that plays a console.
 *
 * An override wins: a person may point a console at a core they already
 * have (the ADR's advanced option), and it is how the player is tested
 * before any build is pinned. Otherwise the installed default, which exists
 * only once Install has finished — a partial install counts as absent.
 */
func Resolve(platform, dir string, overrides map[string]string) (string, Core, error) {
	c, ok := For(platform)
	if !ok {
		return "", Core{}, fmt.Errorf("no core plays %q", platform)
	}
	if p := strings.TrimSpace(overrides[platform]); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, c, nil
		}
		return "", c, fmt.Errorf("the core chosen for %s is not at %s", platform, p)
	}
	p := c.Path(dir)
	if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Size() > 0 {
		return p, c, nil
	}
	return "", c, ErrNotInstalled
}

var client = &http.Client{Timeout: 0}

// StallTimeout abandons a download that has delivered nothing for a minute.
const StallTimeout = 60 * time.Second

/*
 * Install fetches a core's pinned build into dir: downloaded to a temporary
 * file, checked against its SHA-256, and only then is the DLL taken out and
 * renamed into place, so the file exists under its real name only once it is
 * whole and right. Refused with ErrNotPinned until a build is recorded.
 */
func Install(ctx context.Context, c Core, dir string, progress func(done, total int64)) error {
	if !c.Pinned() {
		return ErrNotPinned
	}
	target := filepath.Dir(c.Path(dir))
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(target, "download-*.part")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %s", resp.Status)
	}
	stall := time.AfterFunc(StallTimeout, cancel)
	defer stall.Stop()
	h := sha256.New()
	var done int64
	buf := make([]byte, 128<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			stall.Reset(StallTimeout)
			if _, err := tmp.Write(buf[:n]); err != nil {
				return err
			}
			h.Write(buf[:n])
			done += int64(n)
			if done > c.SizeBytes {
				return fmt.Errorf("%w: longer than pinned", ErrChecksumMismatch)
			}
			if progress != nil {
				progress(done, c.SizeBytes)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != c.SHA256 {
		return fmt.Errorf("%w: got %s", ErrChecksumMismatch, got)
	}
	return extract(tmp.Name(), c.DLL, c.Path(dir))
}

// extract takes one named file out of a verified zip and renames it into
// place. The archive's bytes were checked against the pin before this runs,
// so its entry names are the ones the pinned build has.
func extract(zipPath, name, dst string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if filepath.Base(f.Name) != name || f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.CreateTemp(filepath.Dir(dst), "core-*.tmp")
		if err != nil {
			rc.Close()
			return err
		}
		_, cerr := io.Copy(out, rc)
		rc.Close()
		out.Close()
		if cerr != nil {
			os.Remove(out.Name())
			return cerr
		}
		if err := os.Rename(out.Name(), dst); err != nil {
			os.Remove(out.Name())
			return err
		}
		return nil
	}
	return fmt.Errorf("%s is not in the archive", name)
}
