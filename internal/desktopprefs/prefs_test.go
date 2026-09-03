package desktopprefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// First run has no file, and the answer is both options off. A default that
// arrived by accident is the thing this package exists to avoid.
func TestFirstRunIsBothOff(t *testing.T) {
	p, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.CloseToTray || p.DevTools {
		t.Errorf("defaults = %+v, want both false", p)
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Prefs{CloseToTray: true, DevTools: true}
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
	if p.CloseToTray || p.DevTools {
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
	if err := Save(dir, Prefs{DevTools: true}); err != nil {
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
	if got != (Prefs{DevTools: true}) {
		t.Errorf("after second save = %+v, want only DevTools", got)
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

/*
 * "Open at login" is not kept here, and that is the point.
 *
 * It used to be a field, written and never read: the run key is what actually
 * starts anything at login, the settings page reads that, and this file only
 * recorded what somebody last asked for through this one program. The server's
 * tray writes the run key too and never touched this file, so the two drifted —
 * found on a real install as `open_at_login: true` beside no run-key entry at
 * all.
 *
 * A second copy of a fact, kept by one of its two owners, can only go out of
 * date. This asserts the copy is gone rather than trusting nobody re-adds it.
 */
func TestOpenAtLoginIsNotStoredHere(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, Prefs{CloseToTray: true, DevTools: true}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "desktop.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "open_at_login") {
		t.Errorf("the preferences file records open_at_login again:\n%s\n\n"+
			"the run key is the fact; a copy here can only disagree with it", b)
	}
}

/*
 * The window placement survives a round trip through the file.
 *
 * It is the first field here that is a struct rather than a bool, and the
 * first that is optional — a pointer, so "never recorded" and "recorded at
 * 0,0" are different things. A file written by an older build has no `window`
 * key at all and must still load.
 */
func TestWindowPlacementRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := Prefs{
		CloseToTray: true,
		Window: &WindowPlacement{
			Monitor: `\.\DISPLAY2`, X: 100, Y: 50,
			Width: 1280, Height: 720, Maximized: true,
		},
	}
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Window == nil {
		t.Fatal("Window is nil after a round trip")
	}
	if *got.Window != *want.Window {
		t.Errorf("Window = %+v, want %+v", *got.Window, *want.Window)
	}
	if !got.CloseToTray {
		t.Error("CloseToTray was lost")
	}
}

// A preferences file from before this existed must load, with no placement.
func TestPrefsWithoutAWindowKeyStillLoad(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName),
		[]byte(`{"close_to_tray":true,"devtools":false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Window != nil {
		t.Errorf("Window = %+v, want nil — nothing was ever recorded", got.Window)
	}
	if !got.CloseToTray {
		t.Error("CloseToTray was lost")
	}
}
