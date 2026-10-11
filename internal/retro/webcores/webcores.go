/*
Package webcores fetches the WebAssembly cores a browser plays games with
([ADR 0077](../../../docs/adr/0077-a-browser-can-play-a-game.md)).

The server fetches them on an administrator's request and serves them only to
its own signed-in accounts. LANcast never hosts or ships a core: the licence
reading ADR 0077 accepted is that the server operator fetches GPL binaries for
their own household, exactly as the desktop client does for itself (ADR 0073).

# What a web core is

Not a bare core. Each is RetroArch compiled with one core built in, as a
`{core}_libretro.js` loader and a `{core}_libretro.wasm` module, which is how
libretro's own web player loads them. The page-side host (ADR 0077 part b)
drives that module.

# Where the pin points

libretro's stable 1.22.2 emscripten build, the same release the desktop cores
are pinned to. It ships only as one 749 MB .7z, so, as with the desktop cores,
the archive is the pinned download, each of the eight files is checked against
a pin of its own as it comes out, and the archive is deleted afterwards. The
pins were measured on 2026-10-11 from that archive. None of the eight files
uses threads (no SharedArrayBuffer, no workers), so the page needs no
cross-origin isolation.

# Which cores, and which not

Four, one per console the browser plays (ADR 0077 Decision 4):

| console | core | licence |
|---|---|---|
| Game Boy, Color, Advance | mGBA | MPL-2.0 |
| Master System | Gearsystem | GPL-3.0 |
| NES | FCEUmm | GPL-2.0 |
| Mega Drive | ClownMDEmu | AGPL-3.0 |

SNES has a web build only in snes9x, which withholds commercial rights, and
the other Mega Drive web cores (Genesis Plus GX, PicoDrive) forbid commercial
use. None of them is listed, so none is ever extracted: nothing outside the
four below reaches the disk.
*/
package webcores

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"lancast/internal/retro/cores"
)

// Archive is the pinned emscripten build of RetroArch 1.22.2.
var Archive = cores.Archive{
	URL:       "https://buildbot.libretro.com/stable/1.22.2/emscripten/RetroArch.7z",
	SHA256:    "00e17fda8792ee15a9d9ea4fc769e9f937e525c722ae10dabba2c993941f55f4",
	SizeBytes: 749072539,
	Version:   "1.22.2",
}

const inArchive = "retroarch/"

// Core is one web core: its loader and its module, and the consoles it plays.
type Core struct {
	Name      string   // libretro's name: "fceumm", "mgba", …
	Display   string   // "FCEUmm"
	Licence   string   // SPDX id, read from the upstream repository
	SourceURL string   // the source a GPL binary must be offered with
	Consoles  []string // media platform values: "nes", "gba", …
	JS, Wasm  cores.File
}

func core(name, display, licence, source string, consoles []string,
	jsSHA string, jsSize int64, wasmSHA string, wasmSize int64) Core {
	file := func(ext, sha string, size int64) cores.File {
		base := name + "_libretro." + ext
		return cores.File{
			ArchivePath: inArchive + base,
			Dest:        filepath.Join(name, base),
			SHA256:      sha,
			SizeBytes:   size,
			Label:       display + " (" + ext + ")",
		}
	}
	return Core{
		Name: name, Display: display, Licence: licence, SourceURL: source, Consoles: consoles,
		JS:   file("js", jsSHA, jsSize),
		Wasm: file("wasm", wasmSHA, wasmSize),
	}
}

// all is every web core, in the order a settings page lists them.
var all = []Core{
	core("fceumm", "FCEUmm", "GPL-2.0", "https://github.com/libretro/libretro-fceumm",
		[]string{"nes"},
		"30a9a923d7a4fe660f6797be14b87e638d7857431f2786e0e6f5dcea49a61735", 259690,
		"f035f172f9bf9a2ed9a8fd59708a9ae2bd3f4fea9a87a97fcd6c48b21b846e6a", 3068127),
	core("mgba", "mGBA", "MPL-2.0", "https://github.com/libretro/mgba",
		[]string{"gb", "gbc", "gba"},
		"4d4d2dc2d55f9cb4a09389c6df1915a727256daeb26daf265e6fa7254ab1c353", 262061,
		"e83885d857ce8cfb353a2139d8b00f467c9122c0cb3889813f736f140b6deb84", 3166423),
	core("gearsystem", "Gearsystem", "GPL-3.0", "https://github.com/drhelius/Gearsystem",
		[]string{"sms"},
		"f220ba93594c3c41b7fd4893eeb126d6fb4419866f30494366b33f4a643f77d9", 261190,
		"df29b8c814d5fd06073b94fb1277634c54b73ec8c51673099fa09db77288e925", 2593646),
	core("clownmdemu", "ClownMDEmu", "AGPL-3.0", "https://github.com/Clownacy/clownmdemu-libretro",
		[]string{"genesis"},
		"d943a62049455fc71f49ccc6cd40203c19e00269185592c0effdcd802e4fd510", 260079,
		"5da5e41b712efed6e241a496986c4248ccf773766f886be9a8babc6f2833a5a3", 2769068),
}

// All is every web core.
func All() []Core { return append([]Core(nil), all...) }

// ByName finds a core by libretro name.
func ByName(name string) (Core, bool) {
	for _, c := range all {
		if c.Name == name {
			return c, true
		}
	}
	return Core{}, false
}

// For finds the core that plays a console in the browser. SNES, N64 and
// PlayStation have none, by decision (ADR 0077).
func For(platform string) (Core, bool) {
	for _, c := range all {
		for _, p := range c.Consoles {
			if p == platform {
				return c, true
			}
		}
	}
	return Core{}, false
}

// Consoles is every console the browser can play, sorted.
func Consoles() []string {
	var out []string
	for _, c := range all {
		out = append(out, c.Consoles...)
	}
	sort.Strings(out)
	return out
}

// Files is a core's two files.
func (c Core) Files() []cores.File { return []cores.File{c.JS, c.Wasm} }

// SizeBytes is what the core takes on disk once installed.
func (c Core) SizeBytes() int64 { return c.JS.SizeBytes + c.Wasm.SizeBytes }

/*
 * Present reports whether both of a core's files are in place at their pinned
 * sizes.
 *
 * By size, not by hash: this answers "does the browser play NES here" on
 * every status request, and hashing three megabytes per core per request to
 * answer it would be the wrong cost. Install hashes, so a file of the right
 * size and wrong bytes is still repaired the next time somebody installs.
 */
func (c Core) Present(dir string) bool {
	for _, f := range c.Files() {
		st, err := os.Stat(filepath.Join(dir, f.Dest))
		if err != nil || st.IsDir() || st.Size() != f.SizeBytes {
			return false
		}
	}
	return true
}

/*
 * Path is where one of a core's files is installed, if the name and kind are
 * ones this package knows and the file is present.
 *
 * The only way a request's words become a path: the name must be a listed
 * core and the kind "js" or "wasm", so nothing a caller sends can name a file
 * outside the install directory or a core that was never pinned.
 */
func Path(dir, name, kind string) (string, cores.File, bool) {
	c, ok := ByName(name)
	if !ok {
		return "", cores.File{}, false
	}
	var f cores.File
	switch kind {
	case "js":
		f = c.JS
	case "wasm":
		f = c.Wasm
	default:
		return "", cores.File{}, false
	}
	p := filepath.Join(dir, f.Dest)
	st, err := os.Stat(p)
	if err != nil || st.IsDir() || st.Size() != f.SizeBytes {
		return "", cores.File{}, false
	}
	return p, f, true
}

// Install fetches every web core that is not already in place.
func Install(ctx context.Context, dir string, progress func(cores.Progress)) error {
	return install(ctx, Archive, dir, all, progress)
}

func install(ctx context.Context, a cores.Archive, dir string, want []Core, progress func(cores.Progress)) error {
	var files []cores.File
	for _, c := range want {
		files = append(files, c.Files()...)
	}
	return cores.Fetch(ctx, a, dir, files, progress)
}
