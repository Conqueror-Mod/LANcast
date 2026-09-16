package games

import (
	"os"
	"path/filepath"
	"testing"
)

/*
 * Which installed programs are Blizzard games.
 *
 * The entries below are shaped exactly as the uninstall registry holds them on
 * the machine this was built against. Two of them are the ones that matter:
 * **Hearthstone**, the game, and **Battle.net**, which is published by Blizzard
 * Entertainment, has an install location, and is not a game — the same shape of
 * mistake Steam's redistributables deny list exists for. Offering it would put
 * a tile on the grid whose Play button opens the launcher.
 */

func realPrograms() []InstalledProgram {
	return []InstalledProgram{
		{Key: "Hearthstone", Name: "Hearthstone",
			InstallLocation: `C:\Program Files (x86)\Hearthstone`,
			Publisher:       "Blizzard Entertainment", EstimatedSizeKB: 4_000_000},
		{Key: "Battle.net", Name: "Battle.net",
			InstallLocation: `C:\Program Files (x86)\Battle.net`,
			Publisher:       "Blizzard Entertainment", EstimatedSizeKB: 300_000},
		// Everything else on a real machine.
		{Key: "Steam", Name: "Steam", InstallLocation: `C:\Program Files (x86)\Steam`,
			Publisher: "Valve Corporation"},
		{Key: "{7B2E...}", Name: "Microsoft Edge", InstallLocation: `C:\Program Files\Edge`,
			Publisher: "Microsoft Corporation"},
	}
}

func TestOnlyBlizzardGamesAreOffered(t *testing.T) {
	games := BlizzardGames(realPrograms())
	if len(games) != 1 {
		t.Fatalf("got %d games, want only Hearthstone: %+v", len(games), games)
	}
	g := games[0]
	if g.Name != "Hearthstone" {
		t.Errorf("name = %q", g.Name)
	}
	if g.Source != SourceBattleNet {
		t.Errorf("source = %q", g.Source)
	}
	if g.ID != "battlenet:Hearthstone" {
		t.Errorf("id = %q, want the namespaced registry key", g.ID)
	}
	if g.SizeBytes != 4_000_000*1024 {
		t.Errorf("size = %d, want the registry's kilobytes as bytes", g.SizeBytes)
	}
}

func TestTheLauncherIsNotAGame(t *testing.T) {
	/*
	 * Stated on its own because it is the failure a reader written from the
	 * publisher rule alone would ship: Battle.net passes every other check.
	 */
	for _, g := range BlizzardGames(realPrograms()) {
		if g.Name == "Battle.net" || g.ID == "battlenet:Battle.net" {
			t.Error("Battle.net itself was offered as a game")
		}
	}
}

func TestTheLauncherIsExcludedByKeyNotByName(t *testing.T) {
	/*
	 * A display name is localised; a registry key is not. Matching the name
	 * would let the launcher through on any machine not set to English, which
	 * is a bug nobody here would ever see.
	 */
	translated := []InstalledProgram{
		{Key: "Battle.net", Name: "Battle.net (Kampfnetz)",
			InstallLocation: `C:\Program Files (x86)\Battle.net`,
			Publisher:       "Blizzard Entertainment"},
	}
	if got := BlizzardGames(translated); len(got) != 0 {
		t.Errorf("a renamed launcher was offered as a game: %+v", got)
	}
}

func TestAnEntryWithNoInstallLocationIsSkipped(t *testing.T) {
	// A leftover from an uninstall that did not finish. It cannot be launched
	// or measured, so it is not a tile.
	left := []InstalledProgram{
		{Key: "Overwatch", Name: "Overwatch", InstallLocation: "  ",
			Publisher: "Blizzard Entertainment"},
	}
	if got := BlizzardGames(left); len(got) != 0 {
		t.Errorf("an entry with no install location was offered: %+v", got)
	}
}

func TestAnUnnamedEntryFallsBackToItsKey(t *testing.T) {
	// DisplayName is not guaranteed. The key is, and a tile with the key on it
	// is better than a tile with nothing.
	got := BlizzardGames([]InstalledProgram{
		{Key: "Diablo IV", Name: "", InstallLocation: `C:\Games\D4`,
			Publisher: "Blizzard Entertainment"},
	})
	if len(got) != 1 || got[0].Name != "Diablo IV" {
		t.Fatalf("got %+v, want the key used as the name", got)
	}
}

func TestBlizzardGamesComeBackInNameOrder(t *testing.T) {
	// One order, decided here, so the grid, a test and anything reading this
	// next all agree.
	got := BlizzardGames([]InstalledProgram{
		{Key: "c", Name: "StarCraft II", InstallLocation: `C:\a`, Publisher: "Blizzard Entertainment"},
		{Key: "a", Name: "Diablo IV", InstallLocation: `C:\b`, Publisher: "Blizzard Entertainment"},
		{Key: "b", Name: "hearthstone", InstallLocation: `C:\c`, Publisher: "Blizzard Entertainment"},
	})
	want := []string{"Diablo IV", "hearthstone", "StarCraft II"}
	for i, w := range want {
		if got[i].Name != w {
			t.Fatalf("order = %v, want %v", []Game{got[0], got[1], got[2]}, want)
		}
	}
}

/*
 * Choosing what to run, which is the part Battle.net cannot do with a URI.
 */

func TestTheGamesOwnLauncherIsPreferred(t *testing.T) {
	/*
	 * What the Start Menu shortcut runs, and what performs the game's update
	 * check. On the real machine that is `Hearthstone Beta Launcher.exe`,
	 * sitting beside `Hearthstone.exe` — so "the first executable" and "the one
	 * named after the folder" both pick the wrong one.
	 */
	dir := t.TempDir()
	for _, n := range []string{"Hearthstone.exe", "Hearthstone Beta Launcher.exe", "Uninstaller.exe"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := BattleNetLaunchTarget(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "Hearthstone Beta Launcher.exe" {
		t.Errorf("chose %q, want the launcher", filepath.Base(got))
	}
}

func TestAnExecutableNamedAfterTheFolderIsNext(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Diablo III")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"Diablo III.exe", "BlizzardError.exe"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := BattleNetLaunchTarget(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "Diablo III.exe" {
		t.Errorf("chose %q", filepath.Base(got))
	}
}

func TestAmbiguityIsRefusedRatherThanGuessed(t *testing.T) {
	/*
	 * Four executables, none of them obviously the game. Guessing here starts
	 * an uninstaller, and "I could not tell which" is a better answer than
	 * that.
	 */
	dir := t.TempDir()
	for _, n := range []string{"a.exe", "b.exe", "Uninstall.exe", "crashreport.exe"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := BattleNetLaunchTarget(dir); err == nil {
		t.Errorf("guessed %q instead of refusing", filepath.Base(got))
	}
}

func TestASingleExecutableIsUnambiguous(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "TheGame.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := BattleNetLaunchTarget(dir)
	if err != nil || filepath.Base(got) != "TheGame.exe" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestADirectoryWithNoExecutableIsAnError(t *testing.T) {
	if _, err := BattleNetLaunchTarget(t.TempDir()); err == nil {
		t.Error("an empty directory produced a launch target")
	}
}
