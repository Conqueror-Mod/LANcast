package media

import (
	"path/filepath"
	"testing"
)

var retroRoot = filepath.FromSlash("/games")

func rp(rel string) string { return filepath.Join(retroRoot, filepath.FromSlash(rel)) }

// Every extension that names one console, by itself, in a folder that says
// nothing — the extension must be enough.
func TestPlatformFromExtension(t *testing.T) {
	cases := map[string]string{
		".nes": PlatformNES, ".sfc": PlatformSNES, ".smc": PlatformSNES,
		".z64": PlatformN64, ".v64": PlatformN64, ".n64": PlatformN64,
		".gb": PlatformGB, ".gbc": PlatformGBC, ".gba": PlatformGBA,
		".md": PlatformGenesis, ".gen": PlatformGenesis, ".smd": PlatformGenesis,
		".sms": PlatformSMS,
		".cue": PlatformPS1, ".chd": PlatformPS1, ".pbp": PlatformPS1,
	}
	for ext, want := range cases {
		t.Run(ext, func(t *testing.T) {
			if got := Platform(retroRoot, rp("Misc/Game (USA)"+ext)); got != want {
				t.Errorf("Platform(%s) = %q, want %q", ext, got, want)
			}
		})
	}
}

// The extension outranks the folder: an N64 dump filed under the wrong
// console folder is still an N64 dump.
func TestPlatformExtensionBeatsFolder(t *testing.T) {
	if got := Platform(retroRoot, rp("SNES/Super Mario 64 (USA).z64")); got != PlatformN64 {
		t.Errorf("got %q, want n64", got)
	}
}

// A .bin names nothing by itself. It is left unknown rather than guessed.
func TestPlatformBinWithoutFolderIsUnknown(t *testing.T) {
	if got := Platform(retroRoot, rp("Sonic the Hedgehog (USA, Europe).bin")); got != "" {
		t.Errorf("got %q, want unknown", got)
	}
}

func TestPlatformBinUnderGenesisFolder(t *testing.T) {
	if got := Platform(retroRoot, rp("Sega Genesis/Sonic the Hedgehog (USA, Europe).bin")); got != PlatformGenesis {
		t.Errorf("got %q, want genesis", got)
	}
}

// libretro's own folder names, which a RetroArch-shaped library uses.
func TestPlatformLibretroFolderNames(t *testing.T) {
	cases := map[string]string{
		"Nintendo - Nintendo 64":      PlatformN64,
		"Sega - Mega Drive - Genesis": PlatformGenesis,
		"Sony - PlayStation":          PlatformPS1,
		"Nintendo - Game Boy Color":   PlatformGBC,
	}
	for dir, want := range cases {
		t.Run(dir, func(t *testing.T) {
			if got := Platform(retroRoot, rp(dir+"/x.zip")); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

// The nearest folder that names a console is the one believed.
func TestPlatformNearestFolderWins(t *testing.T) {
	if got := Platform(retroRoot, rp("PS1/Genesis rips/Genesis/x.zip")); got != PlatformGenesis {
		t.Errorf("got %q, want genesis", got)
	}
}

// Folders above the library root are not read: a library rooted inside a
// folder called N64 does not make every zip in it an N64 game.
func TestPlatformIgnoresFoldersAboveRoot(t *testing.T) {
	root := filepath.FromSlash("/N64/library")
	if got := Platform(root, filepath.Join(root, "x.zip")); got != "" {
		t.Errorf("got %q, want unknown", got)
	}
}

func TestPlatformNotAROM(t *testing.T) {
	if got := Platform(retroRoot, rp("N64/readme.txt")); got != "" {
		t.Errorf("got %q, want unknown", got)
	}
}

// A .bin under a PlayStation folder is a track of the disc its .cue
// describes, and is not a game of its own.
func TestDiscTrackUnderPlayStationFolder(t *testing.T) {
	if !IsDiscTrack(retroRoot, rp("PS1/Final Fantasy VII (USA) (Disc 1)/Final Fantasy VII (USA) (Disc 1) (Track 1).bin")) {
		t.Error("a PS1 .bin was not treated as a disc track")
	}
	if IsDiscTrack(retroRoot, rp("PS1/Final Fantasy VII (USA) (Disc 1)/Final Fantasy VII (USA) (Disc 1).cue")) {
		t.Error("the .cue is the game, not a track")
	}
	if IsDiscTrack(retroRoot, rp("Genesis/Sonic.bin")) {
		t.Error("a Genesis .bin is a cartridge, not a disc track")
	}
	if IsDiscTrack(retroRoot, rp("Sonic.bin")) {
		t.Error("an unplaced .bin should be listed, not dropped")
	}
}

func TestIsScannableRetro(t *testing.T) {
	for _, p := range []string{"a.z64", "a.zip", "a.bin", "a.cue", "a.gba"} {
		if !IsScannable(p, LibraryRetro) {
			t.Errorf("%s not scannable in a retro library", p)
		}
	}
	for _, p := range []string{"a.mkv", "a.mp3", "a.jpg", "a.7z", "a.txt", "a.sav", "a.srm"} {
		if IsScannable(p, LibraryRetro) {
			t.Errorf("%s scannable in a retro library", p)
		}
	}
	// And the reverse: a movie library does not take ROMs.
	if IsScannable("a.z64", LibraryMovie) {
		t.Error("a ROM is scannable in a movie library")
	}
}

func TestROMTitle(t *testing.T) {
	cases := map[string]string{
		"Super Mario 64 (USA).z64":                                "Super Mario 64",
		"007 - The World Is Not Enough (Europe) (En,Fr,De).z64":   "007 - The World Is Not Enough",
		"Legend of Zelda, The - A Link to the Past (USA) [!].sfc": "Legend of Zelda, The - A Link to the Past",
		"Final Fantasy VII (USA) (Disc 1).cue":                    "Final Fantasy VII",
		"Pokemon - Emerald Version (USA, Europe) (Rev 1).gba":     "Pokemon - Emerald Version",
		"Untagged Homebrew.nes":                                   "Untagged Homebrew",
		"Ms. Pac-Man (USA)":                                       "Ms. Pac-Man",
		"Remastered 4K Adventure (USA).sfc":                       "Remastered 4K Adventure",
		"F-Zero (Japan)(Rev A).sfc":                               "F-Zero",
		"Banjo-Kazooie [Hack by Someone] (USA).z64":               "Banjo-Kazooie",
	}
	for in, want := range cases {
		if got := ROMTitle(in); got != want {
			t.Errorf("ROMTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestROMRegion(t *testing.T) {
	cases := map[string]string{
		"Super Mario 64 (USA).z64":                              "USA",
		"Sonic the Hedgehog (USA, Europe).md":                   "USA, Europe",
		"007 - The World Is Not Enough (Europe) (En,Fr,De).z64": "Europe",
		"Pokemon - Emerald Version (Rev 1) (USA).gba":           "USA",
		"Untagged Homebrew.nes":                                 "",
		"Game (Beta) (Proto).nes":                               "",
	}
	for in, want := range cases {
		if got := ROMRegion(in); got != want {
			t.Errorf("ROMRegion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseInRetroLibrary(t *testing.T) {
	info := Parse(retroRoot, rp("N64/Super Mario 64 (USA).v64"), LibraryRetro)
	if info.Kind != KindROM || info.Title != "Super Mario 64" || info.Platform != PlatformN64 {
		t.Errorf("got %+v", info)
	}
	// The season/episode heuristics must not run on a ROM name.
	info = Parse(retroRoot, rp("SNES/Star Fox S01E02 (USA).sfc"), LibraryRetro)
	if info.Kind != KindROM || info.Season != 0 {
		t.Errorf("a ROM was read as an episode: %+v", info)
	}
}

// An .m3u is a game in a retro library; it names no console by itself.
func TestM3UIsAnAmbiguousROM(t *testing.T) {
	if !IsScannable("Game.m3u", LibraryRetro) {
		t.Error(".m3u not scannable in a retro library")
	}
	if got := Platform(retroRoot, rp("PS1/Game (USA).m3u")); got != PlatformPS1 {
		t.Errorf("platform = %q, want ps1 from the folder", got)
	}
	if got := Platform(retroRoot, rp("Game (USA).m3u")); got != "" {
		t.Errorf("platform = %q, want unknown", got)
	}
	for p, want := range map[string]bool{"a.cue": true, "a.CHD": true, "a.pbp": true, "a.bin": false, "a.m3u": false} {
		if IsDisc(p) != want {
			t.Errorf("IsDisc(%s) = %v", p, !want)
		}
	}
}
