package media

import (
	"path/filepath"
	"regexp"
	"strings"
)

/*
 * Retro games (ADR 0073): which console a ROM belongs to, read from its name.
 *
 * This is filename guessing, so it lives here and nowhere else. The DAT
 * lookup that later identifies a ROM by its hash needs the platform first —
 * a CRC is only unique within one console's DAT — and the platform is the one
 * thing the bytes alone do not reliably say.
 *
 * Extension first, folder second. Most ROM extensions name exactly one
 * console. A few do not: `.bin` is a Genesis cartridge *and* a PlayStation
 * disc track, and `.zip` is anything. Those resolve only by a folder above the
 * file, and stay unknown when no folder says. An unknown platform is an honest
 * answer that a person can fix by moving the file; a guessed one is a wrong
 * title that looks right.
 */

// Platform identifiers, as stored in media_item.platform and accepted by the
// API's filter. Short and lowercase because they are a wire format.
const (
	PlatformNES     = "nes"
	PlatformSNES    = "snes"
	PlatformN64     = "n64"
	PlatformGB      = "gb"
	PlatformGBC     = "gbc"
	PlatformGBA     = "gba"
	PlatformGenesis = "genesis"
	PlatformSMS     = "sms"
	PlatformPS1     = "ps1"
)

// Platforms lists every platform a retro library recognises, in the order a
// console filter shows them.
var Platforms = []string{
	PlatformNES, PlatformSNES, PlatformN64,
	PlatformGB, PlatformGBC, PlatformGBA,
	PlatformSMS, PlatformGenesis, PlatformPS1,
}

// IsPlatform reports whether s is a platform this build knows.
func IsPlatform(s string) bool {
	for _, p := range Platforms {
		if p == s {
			return true
		}
	}
	return false
}

// romExts are the extensions that name exactly one console.
var romExts = map[string]string{
	".nes": PlatformNES,
	".sfc": PlatformSNES, ".smc": PlatformSNES,
	".z64": PlatformN64, ".v64": PlatformN64, ".n64": PlatformN64,
	".gb":  PlatformGB,
	".gbc": PlatformGBC,
	".gba": PlatformGBA,
	".md":  PlatformGenesis, ".gen": PlatformGenesis, ".smd": PlatformGenesis,
	".sms": PlatformSMS,
	// A disc's entry file. The tracks it lists are not entries of their own —
	// see IsDiscTrack.
	".cue": PlatformPS1, ".chd": PlatformPS1, ".pbp": PlatformPS1,
}

// ambiguousExts are scanned in a retro library but name no console by
// themselves. An `.m3u` is a game that spans several discs, and is placed by
// its folder or by the first disc it lists. `.7z` is deliberately absent: the standard library cannot open
// it, so a 7z ROM could be listed and never identified or played.
var ambiguousExts = map[string]bool{".bin": true, ".zip": true, ".m3u": true}

// folderPlatforms maps a normalised folder name to the console it names.
// Normalised by normalizeDirName, so "Nintendo - Nintendo 64", "nintendo_64"
// and "Nintendo64" all reach the same key. The libretro names are included
// because a library laid out the way RetroArch lays one out is common.
var folderPlatforms = map[string]string{}

func init() {
	for p, names := range map[string][]string{
		PlatformNES:     {"nes", "famicom", "nintendo entertainment system", "nintendo - nintendo entertainment system"},
		PlatformSNES:    {"snes", "sfc", "super nintendo", "super famicom", "super nintendo entertainment system", "nintendo - super nintendo entertainment system"},
		PlatformN64:     {"n64", "nintendo 64", "nintendo - nintendo 64"},
		PlatformGB:      {"gb", "game boy", "gameboy", "nintendo - game boy"},
		PlatformGBC:     {"gbc", "game boy color", "gameboy color", "nintendo - game boy color"},
		PlatformGBA:     {"gba", "game boy advance", "gameboy advance", "nintendo - game boy advance"},
		PlatformGenesis: {"genesis", "megadrive", "mega drive", "sega genesis", "sega mega drive", "sega - mega drive - genesis"},
		PlatformSMS:     {"sms", "master system", "sega master system", "mark iii", "sega - master system - mark iii"},
		PlatformPS1:     {"ps1", "psx", "playstation", "sony playstation", "playstation 1", "sony - playstation"},
	} {
		for _, n := range names {
			folderPlatforms[normalizeDirName(n)] = p
		}
	}
}

// IsROM reports whether a path is a file a retro library indexes: a known ROM
// or disc extension, or one of the ambiguous ones a folder may resolve.
func IsROM(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, known := romExts[ext]
	return known || ambiguousExts[ext]
}

// PlatformOfExt is the console an extension alone names, or "". Exported for
// the hashing pass, which opens a zip and asks the same question of the file
// inside it — so the guess about that name is still made here.
func PlatformOfExt(name string) string {
	return romExts[strings.ToLower(filepath.Ext(name))]
}

// Platform returns the console a ROM belongs to, or "" when nothing says.
//
// The extension wins whenever it names a console. A folder decides only for
// an ambiguous extension, and the nearest folder that names a console is the
// one believed — "PS1/Genesis rips/x.bin" is a Genesis cartridge. Folders are
// read only between root and the file, so a library rooted inside a folder
// called "N64" does not make every zip in it an N64 game.
func Platform(root, path string) string {
	if p := PlatformOfExt(path); p != "" {
		return p
	}
	if !IsROM(path) {
		return ""
	}
	return platformFromDirs(root, path)
}

func platformFromDirs(root, path string) string {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := len(parts) - 2; i >= 0; i-- {
		if p, ok := folderPlatforms[normalizeDirName(parts[i])]; ok {
			return p
		}
	}
	return ""
}

// IsDisc reports whether a file is a whole disc image: what an .m3u lists
// when a game spans several discs.
func IsDisc(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cue", ".chd", ".pbp":
		return true
	}
	return false
}

// IsDiscTrack reports whether a file is one track of a disc rather than a game.
//
// A PlayStation game is a `.cue` and the `.bin` tracks it lists, and only the
// cue is the game: a row per track would put a nine-track disc in the grid
// nine times. Decided by the folder rather than by reading the cue, because
// the walk visits the .bin without knowing whether a cue beside it names it —
// and a .bin under a PlayStation folder is a track in every layout that
// exists. A .bin whose folder says nothing stays an entry of unknown platform,
// so it is listed rather than silently dropped.
func IsDiscTrack(root, path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".bin") &&
		platformFromDirs(root, path) == PlatformPS1
}

/*
 * reROMTag is one trailing No-Intro or GoodTools tag: "(USA)", "(Rev 1)",
 * "[!]", "(Disc 1)". Everything from the first one onward is release
 * metadata, not the title. Only a tag preceded by a space counts, so a title
 * with brackets in its middle keeps them.
 */
var reROMTag = regexp.MustCompile(`\s+[\(\[][^\)\]]*[\)\]]`)

// romRegions are the No-Intro region words. A tag is a region tag when every
// comma-separated word in it is one of these, which is how "(USA, Europe)" is
// told apart from "(Rev 1)" and "(En,Fr,De)".
var romRegions = map[string]bool{
	"world": true, "usa": true, "europe": true, "japan": true, "asia": true,
	"australia": true, "brazil": true, "canada": true, "china": true,
	"france": true, "germany": true, "hong kong": true, "italy": true,
	"korea": true, "netherlands": true, "spain": true, "sweden": true,
	"taiwan": true, "uk": true, "russia": true, "scandinavia": true,
	"unknown": true,
}

// ROMTitle is the title in a ROM's filename or DAT name: everything before
// the first tag. "Super Mario 64 (USA).z64" is "Super Mario 64".
//
// Not run through clean: its release-group and quality vocabulary is a video
// convention, and a game called "Remastered" or "4K" would lose its name.
func ROMTitle(name string) string {
	base := name
	if IsROM(name) {
		base = strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	}
	if loc := reROMTag.FindStringIndex(base); loc != nil {
		base = base[:loc[0]]
	}
	return strings.TrimSpace(base)
}

// ROMRegion returns the region tag of a ROM's filename or DAT name, verbatim
// ("USA", "USA, Europe"), or "" when it carries none.
func ROMRegion(name string) string {
	base := name
	if IsROM(name) {
		base = strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	}
	for _, tag := range reROMTag.FindAllString(base, -1) {
		inner := strings.TrimSpace(tag)
		inner = inner[1 : len(inner)-1]
		all := inner != ""
		for _, w := range strings.Split(inner, ",") {
			if !romRegions[strings.ToLower(strings.TrimSpace(w))] {
				all = false
				break
			}
		}
		if all {
			return inner
		}
	}
	return ""
}
