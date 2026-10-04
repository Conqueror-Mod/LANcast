package games

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/*
 * The Xbox reader, against the bytes and strings of a real install:
 * Minecraft for Windows in D:\XboxGames, whose package family name Windows
 * reports (Get-AppxPackage) as Microsoft.MinecraftUWP_8wekyb3d8bbwe.
 */

const msPublisher = "CN=Microsoft Corporation, O=Microsoft Corporation, L=Redmond, S=Washington, C=US"

// The hash Windows itself appends for Microsoft. If this is wrong every Xbox
// game launches nothing, and nothing says so.
func TestPublisherIDMatchesWindows(t *testing.T) {
	if got := PublisherID(msPublisher); got != "8wekyb3d8bbwe" {
		t.Errorf("PublisherID = %q, want 8wekyb3d8bbwe (what Windows reports for Microsoft)", got)
	}
}

// D:\.GamingRoot on the machine this was built against, byte for byte.
var realGamingRoot = []byte{
	'R', 'G', 'B', 'X', 1, 0, 0, 0,
	'X', 0, 'b', 0, 'o', 0, 'x', 0, 'G', 0, 'a', 0, 'm', 0, 'e', 0, 's', 0, 0, 0,
}

func TestGamingRootNamesItsFolder(t *testing.T) {
	got := GamingRootFolders(realGamingRoot)
	if len(got) != 1 || got[0] != "XboxGames" {
		t.Errorf("folders = %q, want [XboxGames]", got)
	}
}

// Another program's file: anything unexpected is no folders, never a panic.
func TestGamingRootSurvivesNonsense(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte("RGBX"), []byte("NOPE\x01\x00\x00\x00X\x00"), {'R', 'G', 'B', 'X', 9, 0, 0, 0, 'A'}} {
		_ = GamingRootFolders(raw) // must not panic
	}
	if got := GamingRootFolders([]byte("NOPE\x01\x00\x00\x00X\x00\x00\x00")); got != nil {
		t.Errorf("wrong magic gave %q, want nothing", got)
	}
}

const minecraftConfig = `<?xml version="1.0" encoding="utf-8"?>
<Game configVersion="1">
  <Identity Name="Microsoft.MinecraftUWP" Publisher="` + msPublisher + `" Version="1.26.5203.0" />
  <ShellVisuals DefaultDisplayName="Minecraft for Windows" PublisherDisplayName="Microsoft Studios"/>
  <ExecutableList>
    <Executable Name="Minecraft.Windows.exe" TargetDeviceFamily="PC" Id="Game" />
  </ExecutableList>
</Game>`

const minecraftManifest = `<?xml version="1.0" encoding="UTF-8"?>
<Package xmlns="http://schemas.microsoft.com/appx/manifest/foundation/windows10">
  <Identity Name="Microsoft.MinecraftUWP" Publisher="` + msPublisher + `" Version="1.26.5203.0" ProcessorArchitecture="x64" />
</Package>`

func TestMinecraftIsReadAsWindowsNamesIt(t *testing.T) {
	g, ok := ParseXboxGame([]byte(minecraftConfig), []byte(minecraftManifest), "Minecraft for Windows", `D:\XboxGames\Minecraft for Windows`)
	if !ok {
		t.Fatal("Minecraft was not read as a game")
	}
	if g.ID != "xbox:Microsoft.MinecraftUWP_8wekyb3d8bbwe!Game" {
		t.Errorf("ID = %q, want the family name Windows reports plus !Game", g.ID)
	}
	if g.Name != "Minecraft for Windows" || g.Source != SourceXbox {
		t.Errorf("got %+v", g)
	}
}

// A resource reference is a key into a table this does not read; the folder
// the Xbox app named after the game is the honest name.
func TestAResourceNameFallsBackToTheFolder(t *testing.T) {
	cfg := strings.Replace(minecraftConfig, `DefaultDisplayName="Minecraft for Windows"`, `DefaultDisplayName="ms-resource:AppName"`, 1)
	g, ok := ParseXboxGame([]byte(cfg), []byte(minecraftManifest), "Minecraft for Windows", "x")
	if !ok || g.Name != "Minecraft for Windows" {
		t.Errorf("got %+v, want the folder's name", g)
	}
}

func TestAGameWithNothingToStartIsNotAGame(t *testing.T) {
	cfg := strings.Replace(minecraftConfig, `Id="Game"`, `Id=""`, 1)
	if _, ok := ParseXboxGame([]byte(cfg), []byte(minecraftManifest), "f", "x"); ok {
		t.Error("a config with no executable id was offered as a game")
	}
}

// A package name that is not a name is refused when read, because what it
// becomes is an argument to a shell.
func TestAnUnsafePackageNameIsRefused(t *testing.T) {
	// XML-escaped, so each reaches the package name rather than breaking the
	// file: a broken manifest falls back to the config's identity, which is a
	// different, safe path.
	for _, bad := range []string{`Evil\..\x`, `a&quot; &amp; calc`, `name with space`} {
		man := strings.Replace(minecraftManifest, `Name="Microsoft.MinecraftUWP"`, `Name="`+bad+`"`, 1)
		if _, ok := ParseXboxGame([]byte(minecraftConfig), []byte(man), "f", "x"); ok {
			t.Errorf("package name %q was accepted", bad)
		}
	}
}

func TestLaunchURIForXbox(t *testing.T) {
	got, err := LaunchURI("xbox:Microsoft.MinecraftUWP_8wekyb3d8bbwe!Game")
	if err != nil || got != `shell:AppsFolder\Microsoft.MinecraftUWP_8wekyb3d8bbwe!Game` {
		t.Errorf("LaunchURI = %q, %v", got, err)
	}
	for _, bad := range []string{
		`xbox:Microsoft.MinecraftUWP_8wekyb3d8bbwe!Game" & calc`,
		`xbox:..\..\Windows\System32\calc`,
		`xbox:Microsoft.MinecraftUWP_8wekyb3d8bbwe`,
	} {
		if _, err := LaunchURI(bad); err == nil {
			t.Errorf("LaunchURI(%q) was accepted", bad)
		}
	}
}

/*
 * A fixture tree shaped like D:\XboxGames: a real game, a game whose config is
 * spelled the other way (Minecraft ships MicrosoftGame.Config), a folder that
 * is not a game, and a half-written download with no config yet.
 */
func TestScanXboxFolders(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`Minecraft for Windows/Content/MicrosoftGame.Config`, minecraftConfig)
	write(`Minecraft for Windows/Content/appxmanifest.xml`, minecraftManifest)
	write(`Minecraft for Windows/Content/data/blob.bin`, strings.Repeat("x", 1000))
	other := strings.ReplaceAll(minecraftConfig, "Microsoft.MinecraftUWP", "Contoso.Other")
	other = strings.Replace(other, "Minecraft for Windows", "Other Game", 1)
	write(`Other Game/Content/microsoftgame.config`, other)
	write(`Other Game/Content/appxmanifest.xml`, strings.ReplaceAll(minecraftManifest, "Microsoft.MinecraftUWP", "Contoso.Other"))
	write(`Not A Game/readme.txt`, "hello")
	write(`Downloading/Content/partial.bin`, "x")

	res := ScanXboxFolders([]string{root})
	if res.Status != StatusOK || len(res.Games) != 2 {
		t.Fatalf("got %+v, want two games", res)
	}
	if res.Games[0].Name != "Minecraft for Windows" || res.Games[1].Name != "Other Game" {
		t.Errorf("names = %q, %q", res.Games[0].Name, res.Games[1].Name)
	}
	if res.Games[0].SizeBytes < 1000 {
		t.Errorf("size = %d, want the folder's bytes counted", res.Games[0].SizeBytes)
	}
}

func TestNoGamingRootIsNotInstalled(t *testing.T) {
	if res := ScanXboxFolders(nil); res.Status != StatusNotInstalled {
		t.Errorf("status = %q, want not-installed", res.Status)
	}
}

func TestSplitIDKnowsXbox(t *testing.T) {
	src, own, ok := SplitID("xbox:Microsoft.MinecraftUWP_8wekyb3d8bbwe!Game")
	if !ok || src != SourceXbox || own != "Microsoft.MinecraftUWP_8wekyb3d8bbwe!Game" {
		t.Errorf("SplitID = %q %q %v", src, own, ok)
	}
}
