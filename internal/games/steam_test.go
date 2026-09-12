package games

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// Every appid and title in this file is invented. Real ones would be a
// maintenance trap the first time Valve renamed something, and there is no
// reason for a test fixture to name anybody's actual library.

func TestVDFReadsNestedBlocksAndEscapes(t *testing.T) {
	const src = `
// a comment Steam sometimes writes
"AppState"
{
	"appid"		"700010"
	"name"		"Quoted \"Title\" Here"
	"installdir"	"Some\\Game Folder"
	"UserConfig"
	{
		"language"	"english"
	}
}
`
	root, err := parseVDF(strings.NewReader(src))
	if err != nil {
		t.Fatalf("parseVDF: %v", err)
	}
	app := root.sub("appstate")
	if app == nil {
		t.Fatal("no AppState block")
	}
	if got := app.val("name"); got != `Quoted "Title" Here` {
		t.Errorf("name = %q, want the unescaped quotes", got)
	}
	// The one escape that matters constantly: a Windows path.
	if got := app.val("installdir"); got != `Some\Game Folder` {
		t.Errorf("installdir = %q, want a single backslash", got)
	}
	if app.sub("userconfig").val("language") != "english" {
		t.Error("nested block was not readable")
	}
}

func TestVDFKeysAreCaseInsensitive(t *testing.T) {
	// Steam's casing has not been stable across client versions, and a reader
	// that matched case would fail by finding nothing.
	root, err := parseVDF(strings.NewReader(`"appstate" { "AppID" "5" "SIZEONDISK" "7" }`))
	if err != nil {
		t.Fatalf("parseVDF: %v", err)
	}
	app := root.sub("appstate")
	if app.val("appid") != "5" || app.val("sizeondisk") != "7" {
		t.Error("keys did not match regardless of case")
	}
}

func TestVDFRefusesAnUnclosedBlock(t *testing.T) {
	if _, err := parseVDF(strings.NewReader(`"AppState" { "appid" "1"`)); err == nil {
		t.Fatal("an unterminated block parsed cleanly")
	}
}

func TestLibraryFoldersReadsBothShapes(t *testing.T) {
	current := `
"libraryfolders"
{
	"0"
	{
		"path"		"C:\\Program Files (x86)\\Steam"
		"label"		""
	}
	"1"
	{
		"path"		"D:\\SteamLibrary"
	}
}
`
	older := `
"LibraryFolders"
{
	"TimeNextStatsReport"	"1700000000"
	"ContentStatsID"	"12345"
	"1"		"D:\\SteamLibrary"
}
`
	for _, tc := range []struct {
		name string
		src  string
		want []string
	}{
		{"current shape, a block per folder", current, []string{
			filepath.Clean(`C:\Program Files (x86)\Steam`),
			filepath.Clean(`D:\SteamLibrary`),
		}},
		{"older shape, the path as the value", older, []string{filepath.Clean(`D:\SteamLibrary`)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseLibraryFolders(strings.NewReader(tc.src))
			if err != nil {
				t.Fatalf("ParseLibraryFolders: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("folder %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestLibraryFoldersIgnoresTheBookkeepingKeys(t *testing.T) {
	// ContentStatsID is a number beside the numbered folders, and reading it as
	// a path would invent a library.
	got, err := ParseLibraryFolders(strings.NewReader(`"libraryfolders" { "ContentStatsID" "99" }`))
	if err != nil {
		t.Fatalf("ParseLibraryFolders: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want nothing", got)
	}
}

// manifest writes an .acf with the fields the reader cares about.
func manifest(appid, name, installdir string, stateFlags int, size, lastPlayed int64) string {
	return `"AppState"
{
	"appid"		"` + appid + `"
	"name"		"` + name + `"
	"installdir"	"` + installdir + `"
	"StateFlags"	"` + strconv.Itoa(stateFlags) + `"
	"SizeOnDisk"	"` + strconv.FormatInt(size, 10) + `"
	"LastPlayed"	"` + strconv.FormatInt(lastPlayed, 10) + `"
}`
}

func TestAppManifestStateFlags(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags int
		want  bool
	}{
		{"fully installed", 4, true},
		{"installed and queued for an update", 4 | 2, true},
		// 1024 (update started) + 2 (update required), without 4. The value
		// that matters: a game being downloaded has a name and a size like any
		// other, and listing it offers a Play button for something that cannot
		// start.
		{"update started, not yet installed", 1026, false},
		{"nothing set", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := manifest("700010", "Invented Game", "Invented Game", tc.flags, 1024, 0)
			_, ok, err := ParseAppManifest(strings.NewReader(src), `D:\SteamLibrary`)
			if err != nil {
				t.Fatalf("ParseAppManifest: %v", err)
			}
			if ok != tc.want {
				t.Errorf("installed = %v, want %v", ok, tc.want)
			}
		})
	}
}

func TestAppManifestSkipsTheRedistributables(t *testing.T) {
	// It carries an ordinary manifest and is not a game.
	src := manifest("228980", "Steamworks Common Redistributables", "Steamworks Shared", 4, 1024, 0)
	_, ok, err := ParseAppManifest(strings.NewReader(src), `D:\SteamLibrary`)
	if err != nil {
		t.Fatalf("ParseAppManifest: %v", err)
	}
	if ok {
		t.Error("a tool was listed as a game")
	}
}

func TestAppManifestRefusesAnInstallDirThatEscapesItsLibrary(t *testing.T) {
	src := manifest("700011", "Escaping Game", `..\..\..\Windows`, 4, 1024, 0)
	_, ok, err := ParseAppManifest(strings.NewReader(src), `D:\SteamLibrary`)
	if err != nil {
		t.Fatalf("ParseAppManifest: %v", err)
	}
	if ok {
		t.Error("a manifest pointing outside its library folder was accepted")
	}
}

func TestAppManifestReadsTheFields(t *testing.T) {
	src := manifest("700012", "Invented Game", "Invented Game", 4, 4096, 1_700_000_000)
	g, ok, err := ParseAppManifest(strings.NewReader(src), `D:\SteamLibrary`)
	if err != nil || !ok {
		t.Fatalf("ParseAppManifest: ok=%v err=%v", ok, err)
	}
	if g.ID != "700012" || g.Name != "Invented Game" {
		t.Errorf("identity = %q/%q", g.ID, g.Name)
	}
	if g.SizeBytes != 4096 || g.LastPlayed != 1_700_000_000 {
		t.Errorf("size/lastPlayed = %d/%d", g.SizeBytes, g.LastPlayed)
	}
	want := filepath.Join(`D:\SteamLibrary`, "steamapps", "common", "Invented Game")
	if g.InstallPath != want {
		t.Errorf("InstallPath = %q, want %q", g.InstallPath, want)
	}
}

func TestLaunchURI(t *testing.T) {
	got, err := LaunchURI("700010")
	if err != nil {
		t.Fatalf("LaunchURI: %v", err)
	}
	if got != "steam://rungameid/700010" {
		t.Errorf("URI = %q", got)
	}
	// The page hands over an appid and nothing else (ADR 0066). Anything that
	// is not a number is either a bug or an attempt to smuggle something into
	// the URI, and both are refused here.
	for _, bad := range []string{"", "abc", "700010 && calc", "700010/../x", "../../x"} {
		if _, err := LaunchURI(bad); err == nil {
			t.Errorf("LaunchURI(%q) was accepted", bad)
		}
	}
}

// steamTree builds a fixture Steam installation and returns its root.
func steamTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	second := filepath.Join(t.TempDir(), "SteamLibrary")

	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// libraryfolders.vdf must survive being written with Windows escaping on
	// every platform, so the separators are escaped as Steam writes them.
	esc := strings.ReplaceAll(second, `\`, `\\`)
	write(filepath.Join(root, "steamapps", "libraryfolders.vdf"),
		`"libraryfolders" { "0" { "path" "`+strings.ReplaceAll(root, `\`, `\\`)+`" } "1" { "path" "`+esc+`" } }`)

	write(filepath.Join(root, "steamapps", "appmanifest_700010.acf"),
		manifest("700010", "Beta Game", "Beta Game", 4, 2048, 1_700_000_100))
	write(filepath.Join(root, "steamapps", "appmanifest_228980.acf"),
		manifest("228980", "Steamworks Common Redistributables", "Steamworks Shared", 4, 10, 0))
	write(filepath.Join(root, "steamapps", "appmanifest_700013.acf"),
		manifest("700013", "Half Downloaded Game", "Half Downloaded Game", 1026, 99, 0))
	write(filepath.Join(second, "steamapps", "appmanifest_700011.acf"),
		manifest("700011", "Alpha Game", "Alpha Game", 4, 4096, 0))

	// Artwork: one game in the current layout, one in the older one, and one
	// with none at all.
	write(filepath.Join(root, "appcache", "librarycache", "700010", "library_600x900.jpg"), "jpeg")
	write(filepath.Join(root, "appcache", "librarycache", "700011_library_600x900.jpg"), "jpeg")
	return root
}

func TestScanRootReadsEveryLibrary(t *testing.T) {
	res := ScanRoot(steamTree(t))
	if res.Status != StatusOK {
		t.Fatalf("status = %q, err = %q", res.Status, res.Err)
	}
	if len(res.Games) != 2 {
		var names []string
		for _, g := range res.Games {
			names = append(names, g.Name)
		}
		t.Fatalf("got %v, want the two installed games", names)
	}
	// Sorted by name, so the second library's game comes first.
	if res.Games[0].Name != "Alpha Game" || res.Games[1].Name != "Beta Game" {
		t.Errorf("order = %q, %q", res.Games[0].Name, res.Games[1].Name)
	}
	if res.Games[1].PosterPath == "" {
		t.Error("the current artwork layout was not found")
	}
	if res.Games[0].PosterPath == "" {
		t.Error("the older artwork layout was not found")
	}
}

func TestScanRootWithoutArtworkIsStillAGame(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "steamapps"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := manifest("700014", "Unseen Game", "Unseen Game", 4, 64, 0)
	if err := os.WriteFile(filepath.Join(root, "steamapps", "appmanifest_700014.acf"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	res := ScanRoot(root)
	if res.Status != StatusOK || len(res.Games) != 1 {
		t.Fatalf("status = %q, games = %d", res.Status, len(res.Games))
	}
	if res.Games[0].PosterPath != "" || res.Games[0].HeaderPath != "" {
		t.Error("artwork was invented for a game Steam has not cached any for")
	}
}

func TestScanRootSaysNotInstalledRatherThanEmpty(t *testing.T) {
	// The distinction the whole Status type exists for: no Steam is a sentence
	// the page can say, an empty list is not.
	if res := ScanRoot(""); res.Status != StatusNotInstalled {
		t.Errorf("status = %q, want %q", res.Status, StatusNotInstalled)
	}
}

func TestScanRootReportsAnUnreadableLibraryList(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a directory in place of a file is the portable way to make a read fail")
	}
	root := t.TempDir()
	// A directory where libraryfolders.vdf should be: reading it fails, and
	// that must be StatusError rather than a quietly empty library.
	if err := os.MkdirAll(filepath.Join(root, "steamapps", "libraryfolders.vdf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if res := ScanRoot(root); res.Status != StatusError {
		t.Errorf("status = %q, want %q", res.Status, StatusError)
	}
}
