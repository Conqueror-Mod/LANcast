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
