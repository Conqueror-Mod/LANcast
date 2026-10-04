package games

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"
)

/*
 * Games installed through the Xbox app, read from the folders it writes.
 *
 * The same shape of local read as the others: no sign-in, no Xbox Live, no
 * network. The Xbox app installs PC Game Pass and Microsoft Store games into
 * an `XboxGames` folder on whichever drives it was pointed at, and says which
 * folders those are in a `.GamingRoot` file at the root of each drive. Every
 * game folder carries the two files a packaged game must ship:
 *
 *   - `Content\MicrosoftGame.config`, the game's own description — its display
 *     name and the id of the executable to start;
 *   - `Content\appxmanifest.xml`, the package identity — its name and its
 *     publisher, which together name the package to Windows.
 *
 * **No artwork**, as with Epic, for ADR 0066's reason: what a game ships is a
 * square store logo, and a square beside 2:3 posters is a wrong picture where
 * a letter is an honest one.
 *
 * Verified against a real install — Minecraft for Windows, in D:\XboxGames —
 * whose package family name Windows reports as the one PackageFamilyName
 * computes below.
 */

// GamingRootFolders reads a `.GamingRoot` file: the folders, relative to the
// root of its drive, that the Xbox app installs games into.
//
// The format is small and undocumented, so it is read defensively: the bytes
// `RGBX`, a little-endian count, then that many NUL-terminated UTF-16 folder
// names. Anything that does not fit is an empty answer, never a panic — this
// is a file another program writes.
func GamingRootFolders(raw []byte) []string {
	if len(raw) < 8 || string(raw[:4]) != "RGBX" {
		return nil
	}
	count := int(binary.LittleEndian.Uint32(raw[4:8]))
	rest := raw[8:]
	var out []string
	for i := 0; i < count && len(rest) >= 2; i++ {
		var units []uint16
		for len(rest) >= 2 {
			u := binary.LittleEndian.Uint16(rest)
			rest = rest[2:]
			if u == 0 {
				break
			}
			units = append(units, u)
		}
		if name := strings.TrimSpace(string(utf16.Decode(units))); name != "" {
			out = append(out, name)
		}
	}
	return out
}

/*
 * PublisherID is the 13-character hash Windows appends to a package's name to
 * make its family name — `8wekyb3d8bbwe` for Microsoft.
 *
 * The first eight bytes of the SHA-256 of the publisher string in UTF-16
 * little-endian, read as 64 bits, padded with one zero bit to 65, and spelled
 * five bits at a time in Crockford's base-32 alphabet. Computed rather than
 * looked up in the registry so the family name comes from the same two files
 * as everything else here, and so it is a pure function a test can pin to the
 * value Windows reports.
 */
func PublisherID(publisher string) string {
	units := utf16.Encode([]rune(publisher))
	buf := make([]byte, 2*len(units))
	for i, u := range units {
		binary.LittleEndian.PutUint16(buf[2*i:], u)
	}
	sum := sha256.Sum256(buf)
	bits := binary.BigEndian.Uint64(sum[:8])
	const alphabet = "0123456789abcdefghjkmnpqrstvwxyz"
	out := make([]byte, 13)
	for i := 0; i < 13; i++ {
		// 65 bits, the 65th a zero at the bottom: group i is bits 5i..5i+4 of
		// that, so the last group takes the lowest four bits and the pad.
		shift := 59 - 5*i
		var v uint64
		if shift >= 0 {
			v = bits >> uint(shift)
		} else {
			v = bits << uint(-shift)
		}
		out[i] = alphabet[v&31]
	}
	return string(out)
}

// PackageFamilyName joins a package's name and its publisher's hash, which is
// how Windows names a package that may be installed in any version.
func PackageFamilyName(name, publisher string) string {
	return name + "_" + PublisherID(publisher)
}

type xboxGameConfig struct {
	Identity struct {
		Name      string `xml:"Name,attr"`
		Publisher string `xml:"Publisher,attr"`
	} `xml:"Identity"`
	ShellVisuals struct {
		DefaultDisplayName string `xml:"DefaultDisplayName,attr"`
	} `xml:"ShellVisuals"`
	Executables []struct {
		ID string `xml:"Id,attr"`
	} `xml:"ExecutableList>Executable"`
}

type appxIdentity struct {
	Identity struct {
		Name      string `xml:"Name,attr"`
		Publisher string `xml:"Publisher,attr"`
	} `xml:"Identity"`
}

/*
 * ParseXboxGame reads one game folder's two files and says whether they
 * describe a game that can be started.
 *
 * folderName is the name of the game's own folder, which the Xbox app names
 * after the game and which is the name used when the config's display name is
 * a resource reference (`ms-resource:…`) — a key into a resource table this
 * does not read, and not a name anybody wants to see.
 *
 * The package identity comes from the manifest, and the config's Identity is
 * only a fallback: the manifest is what Windows registered, so it is what the
 * family name has to agree with.
 */
func ParseXboxGame(configRaw, manifestRaw []byte, folderName, installPath string) (Game, bool) {
	var cfg xboxGameConfig
	if err := xml.Unmarshal(configRaw, &cfg); err != nil {
		return Game{}, false
	}
	name, publisher := cfg.Identity.Name, cfg.Identity.Publisher
	var man appxIdentity
	if err := xml.Unmarshal(manifestRaw, &man); err == nil && man.Identity.Name != "" {
		name, publisher = man.Identity.Name, man.Identity.Publisher
	}
	if name == "" || publisher == "" || len(cfg.Executables) == 0 || cfg.Executables[0].ID == "" {
		return Game{}, false
	}
	display := strings.TrimSpace(cfg.ShellVisuals.DefaultDisplayName)
	if display == "" || strings.HasPrefix(strings.ToLower(display), "ms-resource:") {
		display = folderName
	}
	aumid := PackageFamilyName(name, publisher) + "!" + cfg.Executables[0].ID
	if !validAUMID.MatchString(aumid) {
		return Game{}, false
	}
	return Game{
		ID:          XboxID(aumid),
		Source:      SourceXbox,
		Name:        display,
		InstallPath: installPath,
		// The Xbox app records no last-played time in these files.
		LastPlayed: 0,
	}, true
}

/*
 * validAUMID is the shape of an application user model id: a package name,
 * an underscore, the 13-character publisher hash, `!`, an application id.
 *
 * It is what reaches `shell:AppsFolder\…`, so it is checked when the game is
 * read and again before anything is launched (LaunchURI) — a package name with
 * a backslash or a quote in it would otherwise be a path or an argument rather
 * than a name.
 */
var validAUMID = regexp.MustCompile(`^[A-Za-z0-9.\-]{3,50}_[0-9a-hjkmnp-tv-z]{13}![A-Za-z][A-Za-z0-9.]{0,63}$`)

/*
 * ScanXboxFolders reads every game in the given install folders.
 *
 * Folder-taking, like ScanRoot and ScanEpicManifests, so every rule is tested
 * against a fixture tree. A folder that is missing a file, or whose files do
 * not parse, is skipped: a game mid-download has a Content directory that is
 * still being written, and one bad folder must not empty the grid.
 *
 * The size is the folder's, walked: neither file records one, and the grid
 * sorts by it. A walk of a large game is a few thousand directory entries,
 * which is the same order of work as Steam's library folders.
 */
func ScanXboxFolders(folders []string) Result {
	if len(folders) == 0 {
		return Result{Status: StatusNotInstalled}
	}
	var games []Game
	for _, root := range folders {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(root, e.Name())
			content := filepath.Join(dir, "Content")
			cfg, err := readCaseless(content, "MicrosoftGame.config")
			if err != nil {
				continue
			}
			man, _ := readCaseless(content, "appxmanifest.xml")
			g, ok := ParseXboxGame(cfg, man, e.Name(), dir)
			if !ok {
				continue
			}
			g.SizeBytes = dirSize(dir)
			games = append(games, g)
		}
	}
	sort.SliceStable(games, func(i, j int) bool {
		return strings.ToLower(games[i].Name) < strings.ToLower(games[j].Name)
	})
	return Result{Status: StatusOK, Games: games}
}

// readCaseless reads dir/name, matching the name without regard to case:
// Minecraft ships `MicrosoftGame.Config`, and the documentation spells it
// `MicrosoftGame.config`. Windows does not care; a fixture tree on Linux does.
func readCaseless(dir, name string) ([]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(e.Name(), name) {
			return os.ReadFile(filepath.Join(dir, e.Name()))
		}
	}
	return nil, fmt.Errorf("%s: %w", name, fs.ErrNotExist)
}

func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}
