package games

import (
	"os"
	"path/filepath"
	"testing"
)

/*
 * What Epic's manifests say is installed.
 *
 * Every shape below is one a real launcher writes. The manifest that motivated
 * the reader is Neon Abyss, and two of its details are the kind a fixture
 * invented from documentation would have missed: the install location carries
 * *mixed separators* — `D:\Epic Library/NeonAbyss`, backslash then forward
 * slash in one path — and a base game is told from downloadable content only by
 * MainGameAppName pointing back at itself.
 */

// realManifest is Neon Abyss as the launcher actually wrote it, trimmed to the
// keys this package reads. A manifest carries 52.
const realManifest = `{
  "AppName": "a26f991a5e6c4e9c9572fc200cbea47f",
  "DisplayName": "Neon Abyss",
  "InstallLocation": "D:\\Epic Library/NeonAbyss",
  "LaunchExecutable": "NeonAbyss.exe",
  "InstallSize": 1024414217,
  "AppCategories": ["public", "games", "applications"],
  "MainGameAppName": "a26f991a5e6c4e9c9572fc200cbea47f",
  "bIsIncompleteInstall": false
}`

func TestARealManifestReadsAsAnInstalledGame(t *testing.T) {
	g, ok := ParseEpicManifest([]byte(realManifest))
	if !ok {
		t.Fatal("the manifest a real launcher wrote was not read as a game")
	}
	if g.Name != "Neon Abyss" {
		t.Errorf("name = %q", g.Name)
	}
	if g.Source != SourceEpic {
		t.Errorf("source = %q, want epic", g.Source)
	}
	if g.ID != "epic:a26f991a5e6c4e9c9572fc200cbea47f" {
		t.Errorf("id = %q, want the namespaced AppName", g.ID)
	}
	if g.SizeBytes != 1024414217 {
		t.Errorf("size = %d", g.SizeBytes)
	}
}

func TestTheMixedSeparatorsAreCleaned(t *testing.T) {
	/*
	 * Verbatim from the real file. The launch executable is joined onto this
	 * path and it is compared against other paths, and a value that half the
	 * code agrees on is the kind of thing that works until two of them meet.
	 */
	g, ok := ParseEpicManifest([]byte(realManifest))
	if !ok {
		t.Fatal("not read as a game")
	}
	/*
	 * Asserted as a literal Windows path rather than through `filepath`, which
	 * is what made this test lie.
	 *
	 * `filepath.Clean` is platform-dependent: on Windows it agreed with the
	 * parser and this passed, and on Linux neither side touched the backslashes
	 * so it compared one mangled value against another. CI caught it. A
	 * manifest always holds a Windows path whatever is reading it, so the
	 * expected value is the same string everywhere.
	 */
	if got := g.InstallPath; got != `D:\Epic Library\NeonAbyss` {
		t.Errorf("install path = %q, want the separators normalised", got)
	}
}

func TestWhatIsNotOfferedAsAGame(t *testing.T) {
	/*
	 * Each of these is a manifest a real library contains. The engine is the
	 * one that makes the category check necessary — an Unreal Engine install
	 * sits in this directory looking exactly like a game.
	 */
	cases := []struct {
		name string
		body string
	}{
		{"an engine or a plugin, which is not a game", `{
			"AppName":"e1","DisplayName":"Unreal Engine","InstallLocation":"D:/UE",
			"AppCategories":["public","engines","applications"],
			"MainGameAppName":"e1","bIsIncompleteInstall":false}`},
		{"downloadable content, which belongs to a game already listed", `{
			"AppName":"dlc1","DisplayName":"Neon Abyss - Deep Sea","InstallLocation":"D:/Epic/NeonAbyss",
			"AppCategories":["public","games","applications"],
			"MainGameAppName":"a26f991a5e6c4e9c9572fc200cbea47f","bIsIncompleteInstall":false}`},
		{"a download that has not finished", `{
			"AppName":"p1","DisplayName":"Half Arrived","InstallLocation":"D:/Epic/Half",
			"AppCategories":["public","games","applications"],
			"MainGameAppName":"p1","bIsIncompleteInstall":true}`},
		{"a manifest with no install location", `{
			"AppName":"n1","DisplayName":"Nowhere","InstallLocation":"",
			"AppCategories":["public","games","applications"],
			"MainGameAppName":"n1","bIsIncompleteInstall":false}`},
		{"a manifest with no name", `{
			"AppName":"n2","DisplayName":"","InstallLocation":"D:/Epic/X",
			"AppCategories":["public","games","applications"],
			"MainGameAppName":"n2","bIsIncompleteInstall":false}`},
		{"something that is not JSON at all", `{ this is not json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if g, ok := ParseEpicManifest([]byte(tc.body)); ok {
				t.Errorf("offered %q as an installed game", g.Name)
			}
		})
	}
}

func TestAManifestWithNoMainGameNameIsStillAGame(t *testing.T) {
	/*
	 * Not every manifest carries MainGameAppName. Treating its absence as
	 * "this is downloadable content" would hide games; the rule is that it
	 * excludes only when it names *something else*.
	 */
	body := `{"AppName":"g9","DisplayName":"Older Title","InstallLocation":"D:/Epic/Older",
		"AppCategories":["public","games"],"bIsIncompleteInstall":false}`
	if _, ok := ParseEpicManifest([]byte(body)); !ok {
		t.Error("a manifest without MainGameAppName was excluded")
	}
}

func TestScanningADirectoryOfManifests(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("A1B2.item", realManifest)
	write("C3D4.item", `{"AppName":"e1","DisplayName":"Unreal Engine","InstallLocation":"D:/UE",
		"AppCategories":["engines"],"MainGameAppName":"e1"}`)
	// Not a manifest. The launcher keeps other things in this directory, and a
	// reader that took every file would fail on the first one.
	write("notes.txt", "ignore me")
	// A manifest mid-write, or from a newer launcher. Skipped, not fatal.
	write("BAD1.item", `{ truncated`)

	res, err := ScanEpicManifests(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusOK {
		t.Fatalf("status = %q, want ok", res.Status)
	}
	if len(res.Games) != 1 || res.Games[0].Name != "Neon Abyss" {
		t.Fatalf("games = %+v, want only Neon Abyss", res.Games)
	}
}

func TestNoManifestDirectoryIsNotInstalledRatherThanAnError(t *testing.T) {
	// Epic not being present is the ordinary case on most machines, and it must
	// not read as something being wrong with LANcast.
	res, err := ScanEpicManifests(filepath.Join(t.TempDir(), "nothing-here"))
	if err != nil {
		t.Fatalf("err = %v, want none", err)
	}
	if res.Status != StatusNotInstalled {
		t.Errorf("status = %q, want not-installed", res.Status)
	}
}

func TestAnEmptyManifestDirectoryIsInstalledWithNothingInIt(t *testing.T) {
	// The other half of the distinction Status exists for: Epic is here and has
	// no games, which is a different sentence from Epic not being here.
	res, err := ScanEpicManifests(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusOK {
		t.Errorf("status = %q, want ok with an empty list", res.Status)
	}
	if len(res.Games) != 0 {
		t.Errorf("games = %+v, want none", res.Games)
	}
}

func TestAnInstallLocationIsCanonicalOnEveryPlatform(t *testing.T) {
	/*
	 * The rule, stated directly, because the version that used `filepath` was
	 * right on Windows and a no-op on Linux — and the CI that builds on Linux
	 * is the only place that could tell.
	 *
	 * Every expectation here is a Windows path, on every operating system,
	 * because that is what a manifest holds regardless of what is reading it.
	 */
	cases := []struct{ in, want string }{
		{`D:\Epic Library/NeonAbyss`, `D:\Epic Library\NeonAbyss`},
		{`D:/Epic Library/NeonAbyss`, `D:\Epic Library\NeonAbyss`},
		{`D:\Epic Library\NeonAbyss`, `D:\Epic Library\NeonAbyss`},
		{`D:\Epic Library\\NeonAbyss`, `D:\Epic Library\NeonAbyss`},
		{`D:\Epic Library\NeonAbyss\`, `D:\Epic Library\NeonAbyss`},
		{`  D:/Games/Thing  `, `D:\Games\Thing`},
		// A UNC path keeps its leading pair; collapsing that would point it at
		// a different machine, which is the one case where two separators mean
		// something.
		{`\\nas\games\Thing`, `\\nas\games\Thing`},
	}
	for _, tc := range cases {
		if got := windowsPath(tc.in); got != tc.want {
			t.Errorf("windowsPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
