package games

import (
	"os"
	"path/filepath"
	"testing"
)

/*
 * GOG and EA (the rules, against fixture folders): what counts as a game, what
 * it is called, and what starts it. The shapes are the real ones from a machine
 * with Coromon from GOG and skate. from the EA app installed.
 */

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const coromonInfo = `{
    "gameId": "1950069341",
    "name": "Coromon",
    "playTasks": [
        {"category": "document", "isPrimary": false, "name": "Manual", "path": "manual.pdf", "type": "FileTask"},
        {"category": "game", "isPrimary": false, "name": "Safe mode", "path": "coromon_safe.exe", "type": "FileTask"},
        {"category": "game", "isPrimary": true, "name": "Coromon", "path": "coromon.exe", "type": "FileTask"}
    ],
    "rootGameId": "1950069341"
}`

func TestGOGReadsTheGameAndItsPrimaryTask(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Coromon")
	writeFile(t, filepath.Join(dir, "goggame-1950069341.info"), coromonInfo)
	writeFile(t, filepath.Join(dir, "coromon.exe"), "MZ")

	r := ScanGOG([]GOGInstall{{GameID: "1950069341", Path: dir}})
	if r.Status != StatusOK || len(r.Games) != 1 {
		t.Fatalf("result = %+v, want one game", r)
	}
	g := r.Games[0]
	if g.ID != "gog:1950069341" || g.Name != "Coromon" || g.Source != SourceGOG {
		t.Errorf("game = %+v", g)
	}
	if g.Executable != filepath.Join(dir, "coromon.exe") {
		t.Errorf("executable = %q, want the primary game task, not safe mode or the manual", g.Executable)
	}
}

// With no task marked primary, the first *game* task runs; a manual listed
// before it is a document, not the game.
func TestGOGWithoutAPrimaryRunsTheFirstGameTask(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Old")
	writeFile(t, filepath.Join(dir, "goggame-42.info"), `{"gameId": "42", "name": "Old", "playTasks": [
		{"category": "document", "path": "manual.pdf", "type": "FileTask"},
		{"category": "game", "path": "old.exe", "type": "FileTask"}]}`)
	r := ScanGOG([]GOGInstall{{GameID: "42", Path: dir}})
	if len(r.Games) != 1 || r.Games[0].Executable != filepath.Join(dir, "old.exe") {
		t.Errorf("games = %+v, want old.exe", r.Games)
	}
}

// DLC registers like a game and names the game it belongs to; a stale key
// names a folder that is no longer there; neither is a tile.
func TestGOGSkipsDLCAndStaleEntries(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Coromon")
	writeFile(t, filepath.Join(dir, "goggame-1950069341.info"), coromonInfo)
	// The DLC has a real info file of its own: what excludes it is dependsOn.
	writeFile(t, filepath.Join(dir, "goggame-1111111111.info"), `{"gameId": "1111111111", "name": "Coromon Soundtrack"}`)
	r := ScanGOG([]GOGInstall{
		{GameID: "1950069341", Path: dir},
		{GameID: "1111111111", Path: dir, DependsOn: "1950069341"},
		{GameID: "2222222222", Path: filepath.Join(t.TempDir(), "gone")},
		{GameID: "not-a-number", Path: dir},
	})
	if len(r.Games) != 1 || r.Games[0].ID != "gog:1950069341" {
		t.Errorf("games = %+v, want only Coromon", r.Games)
	}
	if got := ScanGOG(nil); got.Status != StatusNotInstalled {
		t.Errorf("no installs = %q, want not-installed", got.Status)
	}
}

const skateManifest = `<?xml version='1.0' encoding='utf-8'?>
<DiPManifest version="4.0">
  <contentIDs><contentID>1184493</contentID></contentIDs>
  <gameTitles>
    <gameTitle locale="zh_CN">极速滑板</gameTitle>
    <gameTitle locale="en_US">skate.</gameTitle>
  </gameTitles>
</DiPManifest>`

func TestEAReadsGamesAndLeavesOutTheEAApp(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Skate")
	writeFile(t, filepath.Join(dir, "__Installer", "installerdata.xml"), skateManifest)
	writeFile(t, filepath.Join(dir, "Skate.exe"), "MZ")
	other := filepath.Join(t.TempDir(), "SomethingElse")
	writeFile(t, filepath.Join(other, "__Installer", "installerdata.xml"), skateManifest)

	r := EAGames([]InstalledProgram{
		{Key: "{7B68}", Name: "skate.", Publisher: "Electronic Arts", InstallLocation: dir + string(filepath.Separator),
			DisplayIcon: `"` + filepath.Join(dir, "Skate.exe") + `"`, EstimatedSizeKB: 2048},
		// The EA app itself: same publisher, no folder, no manifest.
		{Key: "{b346}", Name: "EA app", Publisher: "Electronic Arts"},
		// A manifest, but not EA's entry.
		{Key: "{xxxx}", Name: "Other", Publisher: "Someone Else", InstallLocation: other},
	})
	if r.Status != StatusOK || len(r.Games) != 1 {
		t.Fatalf("result = %+v, want skate. only", r)
	}
	g := r.Games[0]
	if g.ID != "ea:1184493" || g.Name != "skate." || g.Source != SourceEA {
		t.Errorf("game = %+v; want the content id and the English title", g)
	}
	if g.Executable != filepath.Join(dir, "Skate.exe") || g.InstallPath != dir || g.SizeBytes != 2048*1024 {
		t.Errorf("executable %q, path %q, size %d", g.Executable, g.InstallPath, g.SizeBytes)
	}
	if got := EAGames(nil); got.Status != StatusNotInstalled {
		t.Errorf("no programs = %q, want not-installed", got.Status)
	}
}

func TestIconExecutable(t *testing.T) {
	for in, want := range map[string]string{
		`"D:\EA Library\Skate\Skate.exe"`: `D:\EA Library\Skate\Skate.exe`,
		`D:\Game\Game.exe,0`:              `D:\Game\Game.exe`,
		`"D:\Game\Game.exe",12`:           `D:\Game\Game.exe`,
		`D:\Game\game.ico`:                "",
		``:                                "",
	} {
		if got := iconExecutable(in); got != want {
			t.Errorf("iconExecutable(%q) = %q, want %q", in, got, want)
		}
	}
}

// The last check before a process: inside the install folder, an .exe, there.
func TestExecutableTargetIsContained(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Game", "game.exe"), "MZ")
	writeFile(t, filepath.Join(dir, "Game", "readme.txt"), "")
	writeFile(t, filepath.Join(dir, "evil.exe"), "MZ")
	in := filepath.Join(dir, "Game")

	ok := Game{Source: SourceEA, InstallPath: in, Executable: filepath.Join(in, "game.exe")}
	if _, err := ExecutableTarget(ok); err != nil {
		t.Errorf("a contained executable was refused: %v", err)
	}
	for name, exe := range map[string]string{
		"outside the folder": filepath.Join(dir, "evil.exe"),
		"climbing out":       filepath.Join(in, "..", "evil.exe"),
		"not an executable":  filepath.Join(in, "readme.txt"),
		"not there":          filepath.Join(in, "missing.exe"),
		"nothing recorded":   "",
	} {
		g := ok
		g.Executable = exe
		if _, err := ExecutableTarget(g); err == nil {
			t.Errorf("%s: accepted %q", name, exe)
		}
	}
}

func TestGalaxyArgsMatchGOGsOwnShortcut(t *testing.T) {
	args, err := GalaxyArgs("1950069341", `D:\GOG Library\Coromon`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/command=runGame", "/gameId=1950069341", `/path="D:\GOG Library\Coromon"`}
	if len(args) != len(want) {
		t.Fatalf("args = %q", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, args[i], want[i])
		}
	}
	for _, bad := range []struct{ id, path string }{
		{"19500 /evil", `D:\X`}, {"1950069341", `D:\X" /command=uninstall`}, {"1950069341", ""},
	} {
		if _, err := GalaxyArgs(bad.id, bad.path); err == nil {
			t.Errorf("GalaxyArgs(%q, %q) was accepted", bad.id, bad.path)
		}
	}
}

func TestCommandExecutable(t *testing.T) {
	for in, want := range map[string]string{
		`"C:\Program Files\GOG Galaxy\GalaxyClient.exe" /urlProtocol="%1"`: `C:\Program Files\GOG Galaxy\GalaxyClient.exe`,
		`C:\Galaxy\GalaxyClient.exe %1`:                                    `C:\Galaxy\GalaxyClient.exe`,
		`"unterminated`:                                                    "",
	} {
		if got := commandExecutable(in); got != want {
			t.Errorf("commandExecutable(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGOGAndEAIDsRoundTrip(t *testing.T) {
	for _, id := range []string{GOGID("1950069341"), EAID("1184493")} {
		if _, own, ok := SplitID(id); !ok || own == "" {
			t.Errorf("SplitID(%q) failed", id)
		}
	}
	if SourceGOG.Label() != "GOG" || SourceEA.Label() != "EA" {
		t.Errorf("labels %q %q", SourceGOG.Label(), SourceEA.Label())
	}
	for _, id := range []string{GOGID("1950069341"), EAID("1184493")} {
		if _, err := LaunchURI(id); err == nil {
			t.Errorf("LaunchURI(%q) built a URI; these start from a file", id)
		}
	}
}
