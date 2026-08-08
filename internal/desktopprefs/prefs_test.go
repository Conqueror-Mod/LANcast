package desktopprefs

import (
	"os"
	"path/filepath"
	"testing"
)

// First run has no file, and the answer is both options off. A default that
// arrived by accident is the thing this package exists to avoid.
func TestFirstRunIsBothOff(t *testing.T) {
	p, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.CloseToTray || p.OpenAtLogin {
		t.Errorf("defaults = %+v, want both false", p)
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Prefs{CloseToTray: true, OpenAtLogin: true}
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

// A preferences file that cannot be parsed must not stop the app opening. The
// caller is told so the failure has a voice, but it gets defaults and carries
// on — refusing to launch over one tickbox trades the product for a setting.
func TestMalformedFileYieldsDefaultsAndAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err == nil {
		t.Error("a malformed file returned no error; the failure needs a voice")
	}
	if p.CloseToTray || p.OpenAtLogin {
		t.Errorf("malformed file yielded %+v, want defaults", p)
	}
}

// Save replaces the file rather than writing into it, so an interrupted write
// cannot leave a file that parses as "both options off" — which would look
// exactly like the user's settings being ignored.
func TestSaveReplacesAtomicallyAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, Prefs{CloseToTray: true}); err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, Prefs{OpenAtLogin: true}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("left a temp file behind: %s", e.Name())
		}
	}
	got, _ := Load(dir)
	if got != (Prefs{OpenAtLogin: true}) {
		t.Errorf("after second save = %+v, want only OpenAtLogin", got)
	}
}

// No directory is survivable rather than fatal: the window falls back to its own
// profile location, and losing a preference is better than refusing to open.
func TestEmptyDirLoadsDefaults(t *testing.T) {
	p, err := Load("")
	if err != nil || p != (Prefs{}) {
		t.Errorf("Load(\"\") = %+v, %v; want defaults and no error", p, err)
	}
	if err := Save("", Prefs{}); err == nil {
		t.Error("Save(\"\") returned no error; writing nowhere should say so")
	}
}
