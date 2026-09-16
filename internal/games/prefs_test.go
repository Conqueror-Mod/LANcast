package games

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrefsRoundTrip(t *testing.T) {
	dir := t.TempDir()

	var p Prefs
	p.Set("700010", false, true)
	p.Set("700011", true, false)
	if err := SavePrefs(dir, p); err != nil {
		t.Fatalf("SavePrefs: %v", err)
	}

	back, err := LoadPrefs(dir)
	if err != nil {
		t.Fatalf("LoadPrefs: %v", err)
	}
	if !back.IsFavourite("700010") || back.IsHidden("700010") {
		t.Error("the favourite did not survive")
	}
	if !back.IsHidden("700011") || back.IsFavourite("700011") {
		t.Error("the hidden game did not survive")
	}
}

func TestPrefsUnsetRemoves(t *testing.T) {
	var p Prefs
	p.Set("700010", true, true)
	p.Set("700010", false, false)
	if p.IsHidden("700010") || p.IsFavourite("700010") {
		t.Error("clearing both flags left the id behind")
	}
	if len(p.Hidden) != 0 || len(p.Favourites) != 0 {
		t.Errorf("lists = %v / %v, want empty", p.Hidden, p.Favourites)
	}
}

func TestPrefsDoNotDuplicate(t *testing.T) {
	var p Prefs
	p.Set("700010", true, false)
	p.Set("700010", true, false)
	if len(p.Hidden) != 1 {
		t.Errorf("hidden = %v, want one entry", p.Hidden)
	}
}

func TestPrefsMissingFileIsTheFirstRun(t *testing.T) {
	p, err := LoadPrefs(t.TempDir())
	if err != nil {
		t.Fatalf("LoadPrefs: %v", err)
	}
	if len(p.Hidden) != 0 || len(p.Favourites) != 0 {
		t.Error("a missing file should mean no hidden and no favourites")
	}
}

func TestPrefsMalformedFileFallsBackToDefaults(t *testing.T) {
	// Refusing to list anybody's games over an unparsable list of hidden ones
	// would trade the feature for a footnote. The caller is told; it is not
	// fatal.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, PrefsFileName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPrefs(dir)
	if err == nil {
		t.Error("a malformed file should have a voice")
	}
	if len(p.Hidden) != 0 || len(p.Favourites) != 0 {
		t.Error("defaults should come back alongside the error")
	}
}

func TestSavePrefsLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	if err := SavePrefs(dir, Prefs{Hidden: []string{"700010"}}); err != nil {
		t.Fatalf("SavePrefs: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != PrefsFileName {
		t.Errorf("directory holds %v, want only %s", entries, PrefsFileName)
	}
}

/*
 * Preferences written before ids were namespaced.
 *
 * games.json is written by one machine for itself and nothing migrates it, so
 * when ids gained a `steam:` prefix every lookup silently missed: a hidden game
 * came back, a favourite left the top row, and a game already answered for was
 * asked again which display to open on.
 *
 * Found against a real client, not by reading the code — its games.json held
 * `"displays": {"4162040": "\\.\DISPLAY3"}`, a bare appid written months
 * before the prefix existed.
 */

func TestPreferencesWrittenBeforeNamespacingStillApply(t *testing.T) {
	p := Prefs{
		Hidden:     []string{"700012"},
		Favourites: []string{"440"},
		Displays:   map[string]string{"4162040": `\.\DISPLAY3`},
	}
	if !p.IsHidden(SteamID("700012")) {
		t.Error("a game hidden before the prefix existed came back")
	}
	if !p.IsFavourite(SteamID("440")) {
		t.Error("a favourite from before the prefix existed was dropped")
	}
	if got := p.DisplayFor(SteamID("4162040")); got != `\.\DISPLAY3` {
		t.Errorf("display = %q; the picker would ask again about a game already answered for", got)
	}
}

func TestTheNewSpellingIsPreferred(t *testing.T) {
	// Once a preference is rewritten under the namespaced key, that is the one
	// that answers — the old key must not shadow it.
	p := Prefs{Displays: map[string]string{
		"4162040":       `\.\DISPLAY3`,
		"steam:4162040": `\.\DISPLAY1`,
	}}
	if got := p.DisplayFor(SteamID("4162040")); got != `\.\DISPLAY1` {
		t.Errorf("display = %q, want the namespaced entry", got)
	}
}

func TestOnlySteamHasALegacySpelling(t *testing.T) {
	/*
	 * Epic and Battle.net ids never existed unprefixed, so stripping their
	 * prefix would invent a key. A Battle.net key is a display name and an Epic
	 * AppName is hex — either could collide with a bare Steam appid somebody
	 * really does have stored.
	 */
	if got := legacyID(BattleNetID("Hearthstone")); got != "" {
		t.Errorf("legacyID = %q, want none for a launcher that never had one", got)
	}
	if got := legacyID(EpicID("a26f991a")); got != "" {
		t.Errorf("legacyID = %q, want none", got)
	}
	if got := legacyID(SteamID("440")); got != "440" {
		t.Errorf("legacyID = %q, want 440", got)
	}
	// A bare id is already the legacy spelling; it has no older one.
	if got := legacyID("440"); got != "" {
		t.Errorf("legacyID = %q, want none for an already-bare id", got)
	}
}
