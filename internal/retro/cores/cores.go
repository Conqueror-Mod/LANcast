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
// # Where the pin points
//
// libretro's own stable release, not a mirror. Per-core zips exist only under
// the buildbot's nightly/…/latest/, rebuilt every night, so a checksum
// recorded for one would stop verifying by the next morning. A stable release
// never changes, but ships every core in one 230 MB .7z. So the whole archive
// is the pinned download, checked against its SHA-256, and each default
// core's DLL is then checked against a SHA-256 of its own as it comes out. The
// archive is deleted afterwards; only the seven DLLs are kept.
//
// The pins were measured on 2026-10-08 from RetroArch 1.22.2's
// windows/x86_64 RetroArch_cores.7z, and every DLL taken from it by this
// package was byte-identical to 7-Zip's own extraction.
package cores

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bodgit/sevenzip"
)

// Core is one emulator core and where its pinned build comes from.
type Core struct {
	Name      string // libretro's name: "mgba", "mesen", …
	Display   string // "mGBA"
	Licence   string // SPDX id, checked against the upstream repository
	SourceURL string // the source a GPL binary must be offered with
	DLL       string // the file's name once installed
	// ArchivePath is where the DLL sits inside the pinned archive, and
	// SHA256 and SizeBytes are the DLL's own, not the archive's.
	ArchivePath string
	SHA256      string
	SizeBytes   int64
	// NeedsGL marks a core that renders through OpenGL.
	NeedsGL bool
	// NeedsBIOS names the BIOS files a core needs from the person, by file
	// name, in the system directory. LANcast never supplies one.
	NeedsBIOS []string
	// BIOSSize is the exact size of a BIOS image for this console. SwanStation
	// finds its BIOS by content, not name, so a file of this size is what
	// counts as "there is one" — a name check would refuse a BIOS saved under
	// any name but the three it lists.
	BIOSSize int64
}

// Archive is a pinned download that holds cores.
type Archive struct {
	URL       string
	SHA256    string
	SizeBytes int64
	Version   string
}

// Stable is RetroArch's stable release, the only archive cores come from.
var Stable = Archive{
	URL:       "https://buildbot.libretro.com/stable/1.22.2/windows/x86_64/RetroArch_cores.7z",
	SHA256:    "86b871e11b9b4772ac644b40a38f2c8e9449da1f355eae7da08aa061148547b0",
	SizeBytes: 229761684,
	Version:   "1.22.2",
}

const inArchive = "RetroArch-Win64/cores/"

/*
 * Defaults: one core per console, chosen for accuracy and for a licence a
 * commercially licensed LANcast could at least point to — GPL or more
 * permissive. Non-commercial cores (Snes9x, Genesis Plus GX, PicoDrive) are
 * never defaults. Licences were read from each upstream repository on
 * 2026-10-08.
 */
var defaults = map[string]Core{
	"gb":  mgba,
	"gbc": mgba,
	"gba": mgba,
	"nes": {Name: "mesen", Display: "Mesen", Licence: "GPL-3.0", SourceURL: "https://github.com/libretro/Mesen", DLL: "mesen_libretro.dll",
		ArchivePath: inArchive + "mesen_libretro.dll", SizeBytes: 3709440,
		SHA256: "3c95a20cbdd8882e2fe12ca26ee88dde809c12f8cfd11f45e1436b17000ce972"},
	"snes": {Name: "bsnes", Display: "bsnes", Licence: "GPL-3.0", SourceURL: "https://github.com/libretro/bsnes-libretro", DLL: "bsnes_libretro.dll",
		ArchivePath: inArchive + "bsnes_libretro.dll", SizeBytes: 5609890,
		SHA256: "af085c35d344df3024a81588f4491bcbe342554282f2e509fd07222470a7e080"},
	"genesis": {Name: "blastem", Display: "BlastEm", Licence: "GPL-3.0", SourceURL: "https://github.com/libretro/blastem", DLL: "blastem_libretro.dll",
		ArchivePath: inArchive + "blastem_libretro.dll", SizeBytes: 648843,
		SHA256: "ad8f1e7cb540d621f202462e5b30b7e74d19c3999b9e715a643218240abb15ce"},
	"sms": {Name: "gearsystem", Display: "Gearsystem", Licence: "GPL-3.0", SourceURL: "https://github.com/drhelius/Gearsystem", DLL: "gearsystem_libretro.dll",
		ArchivePath: inArchive + "gearsystem_libretro.dll", SizeBytes: 1499136,
		SHA256: "5446712afbbfb2acab63995c6ab6c471a9ff45e3b3af077a6a92708cae46019e"},
	"n64": {Name: "mupen64plus_next", Display: "Mupen64Plus-Next", Licence: "GPL-2.0", SourceURL: "https://github.com/libretro/mupen64plus-libretro-nx", DLL: "mupen64plus_next_libretro.dll",
		ArchivePath: inArchive + "mupen64plus_next_libretro.dll", SizeBytes: 8031485,
		SHA256:  "bd9146900abe380878e7c054a60eaf426779438c230bf5856f99b88847d7dd30",
		NeedsGL: true},
	"ps1": {Name: "swanstation", Display: "SwanStation", Licence: "GPL-3.0", SourceURL: "https://github.com/libretro/swanstation", DLL: "swanstation_libretro.dll",
		ArchivePath: inArchive + "swanstation_libretro.dll", SizeBytes: 3896832,
		SHA256:    "25b16255af154058b1d28aee279788de3a601362648bb469147130bc3b553353",
		NeedsBIOS: []string{"scph5501.bin", "scph5500.bin", "scph5502.bin"}, BIOSSize: 512 << 10},
}

var mgba = Core{Name: "mgba", Display: "mGBA", Licence: "MPL-2.0", SourceURL: "https://github.com/libretro/mgba", DLL: "mgba_libretro.dll",
	ArchivePath: inArchive + "mgba_libretro.dll", SizeBytes: 2814433,
	SHA256: "0209adf404a8e0525f66a5dbc9cbc62be2733ce48d0a113a25b94df91118e8eb"}

// For returns the default core for a console.
func For(platform string) (Core, bool) {
	c, ok := defaults[platform]
	return c, ok
}

// All is every default core once, in a stable order: mGBA plays three
// consoles and is fetched once.
func All() []Core {
	seen := map[string]bool{}
	var out []Core
	for _, p := range []string{"gba", "nes", "snes", "genesis", "sms", "n64", "ps1", "gb", "gbc"} {
		c := defaults[p]
		if !seen[c.Name] {
			seen[c.Name] = true
			out = append(out, c)
		}
	}
	return out
}

// Pinned reports whether a core has a build this release can fetch.
func (c Core) Pinned() bool {
	return c.ArchivePath != "" && len(c.SHA256) == 64 && c.SizeBytes > 0 && Stable.pinned()
}

func (a Archive) pinned() bool { return a.URL != "" && len(a.SHA256) == 64 && a.SizeBytes > 0 }

var (
	ErrNotPinned        = errors.New("no pinned build of this core yet")
	ErrChecksumMismatch = errors.New("the download did not match its expected checksum")
	ErrNotInstalled     = errors.New("this console's core is not installed")
)

/*
 * HasBIOS reports whether the system directory holds something that can be
 * this console's BIOS. It cannot tell a US BIOS from a Japanese one; the core
 * can, and says so when the game fails to load, and that message reaches the
 * person. What it catches is the common case: no BIOS at all, which otherwise
 * reads as "the core could not load this game".
 */
func (c Core) HasBIOS(sysdir string) bool {
	if len(c.NeedsBIOS) == 0 {
		return true
	}
	entries, err := os.ReadDir(sysdir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Type().IsRegular() {
			if info, err := e.Info(); err == nil && (c.BIOSSize == 0 || info.Size() == c.BIOSSize) {
				return true
			}
		}
	}
	return false
}

// Path is where an installed core lives under dir.
func (c Core) Path(dir string) string { return filepath.Join(dir, c.Name, c.DLL) }

/*
 * Resolve finds the DLL that plays a console.
 *
 * An override wins: a person may point a console at a core they already
 * have (the ADR's advanced option). Otherwise the installed default, which
 * exists only once InstallAll has checked it — a partial install counts as
 * absent, and so does a DLL of the wrong size, which is what a build pinned
 * by an earlier release looks like after the pin moves.
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
	if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Size() > 0 && (c.SizeBytes == 0 || st.Size() == c.SizeBytes) {
		return p, c, nil
	}
	return "", c, ErrNotInstalled
}

// installed reports whether a core's pinned build is already in place, by
// its bytes rather than its size: InstallAll is the one place that decides
// whether 230 MB needs fetching, and a few megabytes of hashing is cheap.
func installed(c Core, dir string) bool {
	f, err := os.Open(c.Path(dir))
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return hex.EncodeToString(h.Sum(nil)) == c.SHA256
}

// Progress is where an install has got to. Stage is "download" (Done and
// Total in bytes), then "unpack" (Done and Total in cores), then "done".
type Progress struct {
	Stage string `json:"stage"`
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}

var client = &http.Client{Timeout: 0}

// StallTimeout abandons a download that has delivered nothing for a minute.
const StallTimeout = 60 * time.Second

/*
 * InstallAll fetches every core in want that is not already in place.
 *
 * The archive is downloaded beside the cores, checked against its pin, and
 * only then opened. Each DLL is copied out to a temporary file, checked
 * against its own pin, and renamed into place, so a core exists under its
 * real name only once it is whole and right. One that fails its check fails
 * the install and leaves nothing behind; the ones before it are already
 * correct and stay. The archive is deleted however it ends, and one left by
 * a client that was killed mid-download is swept first.
 */
func InstallAll(ctx context.Context, a Archive, dir string, want []Core, progress func(Progress)) error {
	if progress == nil {
		progress = func(Progress) {}
	}
	var need []Core
	for _, c := range want {
		if c.ArchivePath == "" || len(c.SHA256) != 64 || c.SizeBytes <= 0 || !a.pinned() {
			return fmt.Errorf("%s: %w", c.Display, ErrNotPinned)
		}
		if !installed(c, dir) {
			need = append(need, c)
		}
	}
	if len(need) == 0 {
		progress(Progress{Stage: "done"})
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if stale, _ := filepath.Glob(filepath.Join(dir, "download-*.part")); len(stale) > 0 {
		for _, s := range stale {
			os.Remove(s)
		}
	}
	tmp, err := os.CreateTemp(dir, "download-*.part")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	err = download(ctx, a, tmp, progress)
	tmp.Close()
	if err != nil {
		return err
	}
	if err := unpack(ctx, tmp.Name(), dir, need, progress); err != nil {
		return err
	}
	progress(Progress{Stage: "done"})
	return nil
}

func download(ctx context.Context, a Archive, to *os.File, progress func(Progress)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
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
	last := time.Time{}
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			stall.Reset(StallTimeout)
			if _, err := to.Write(buf[:n]); err != nil {
				return err
			}
			h.Write(buf[:n])
			done += int64(n)
			if done > a.SizeBytes {
				return fmt.Errorf("%w: longer than pinned", ErrChecksumMismatch)
			}
			if time.Since(last) > 100*time.Millisecond || done == a.SizeBytes {
				last = time.Now()
				progress(Progress{Stage: "download", Done: done, Total: a.SizeBytes})
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != a.SHA256 {
		return fmt.Errorf("%w: got %s", ErrChecksumMismatch, got)
	}
	return nil
}

// unpack takes each needed core out of a verified archive. Entries are
// visited in the archive's own order: it is solid, so asking for them in any
// other would decompress the start of the block again for each.
func unpack(ctx context.Context, archive, dir string, need []Core, progress func(Progress)) error {
	r, err := sevenzip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()
	byPath := map[string]Core{}
	for _, c := range need {
		byPath[c.ArchivePath] = c
	}
	total := int64(len(byPath))
	progress(Progress{Stage: "unpack", Done: 0, Total: total})
	var done int64
	for _, f := range r.File {
		c, ok := byPath[strings.ReplaceAll(f.Name, `\`, "/")]
		if !ok || f.FileInfo().IsDir() {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := place(f, c, dir); err != nil {
			return fmt.Errorf("%s: %w", c.Display, err)
		}
		delete(byPath, c.ArchivePath)
		done++
		progress(Progress{Stage: "unpack", Done: done, Total: total})
	}
	var missing []string
	for p := range byPath {
		missing = append(missing, p)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("not in the archive: %s", strings.Join(missing, ", "))
	}
	return nil
}

func place(f *sevenzip.File, c Core, dir string) error {
	dst := c.Path(dir)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.CreateTemp(filepath.Dir(dst), "core-*.tmp")
	if err != nil {
		return err
	}
	h := sha256.New()
	// One byte past the pin is enough to know it is wrong.
	n, cerr := io.Copy(io.MultiWriter(out, h), io.LimitReader(rc, c.SizeBytes+1))
	out.Close()
	switch {
	case cerr != nil:
	case n != c.SizeBytes:
		cerr = fmt.Errorf("%w: %d bytes, pinned %d", ErrChecksumMismatch, n, c.SizeBytes)
	case hex.EncodeToString(h.Sum(nil)) != c.SHA256:
		cerr = fmt.Errorf("%w: got %s", ErrChecksumMismatch, hex.EncodeToString(h.Sum(nil)))
	}
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
