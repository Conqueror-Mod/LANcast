package main

import (
	"testing"

	"lancast/internal/desktopprefs"
	"lancast/internal/games"
)

// The bindings are tested rather than the reader: internal/games has its own
// suite for what a manifest means. What matters here is the boundary — that the
// setting is enforced in the process, and that an app id from the page is
// checked against what is installed before anything is launched.

func enabledDir(t *testing.T, on bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := desktopprefs.Save(dir, desktopprefs.Prefs{Games: on}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func call1(t *testing.T, b map[string]any, name, arg string) map[string]any {
	t.Helper()
	fn, ok := b[name].(func(string) map[string]any)
	if !ok {
		t.Fatalf("%s is not a one-argument binding", name)
	}
	return fn(arg)
}

func TestGamesListRefusedWhenSwitchedOff(t *testing.T) {
	b := gamesBindings(enabledDir(t, false))
	fn, ok := b["lancastGames"].(func() map[string]any)
	if !ok {
		t.Fatal("lancastGames is missing")
	}
	if got := fn()["status"]; got != "disabled" {
		t.Errorf("status = %v, want disabled", got)
	}
}

func TestEveryGamesBindingRefusesWhenSwitchedOff(t *testing.T) {
	// The check lives in the process, not in the page: a page that asked anyway
	// is refused here.
	dir := enabledDir(t, false)
	b := gamesBindings(dir)

	for _, name := range []string{"lancastLaunchGame", "lancastOpenGameFolder"} {
		if res := call1(t, b, name, "700010"); res["ok"] != false {
			t.Errorf("%s = %v, want a refusal", name, res)
		}
	}

	setFlags, ok := b["lancastSetGameFlags"].(func(string, bool, bool) map[string]any)
	if !ok {
		t.Fatal("lancastSetGameFlags is missing")
	}
	if res := setFlags("700010", true, false); res["ok"] != false {
		t.Errorf("lancastSetGameFlags = %v, want a refusal", res)
	}
	// And nothing was written.
	if p, _ := games.LoadPrefs(dir); len(p.Hidden) != 0 {
		t.Errorf("hidden = %v, want nothing written while switched off", p.Hidden)
	}

	art, ok := b["lancastGameArt"].(func(string, string) map[string]any)
	if !ok {
		t.Fatal("lancastGameArt is missing")
	}
	if res := art("700010", "poster"); res["ok"] != false {
		t.Errorf("lancastGameArt = %v, want a refusal", res)
	}
}

func TestUnreadablePreferencesMeanOff(t *testing.T) {
	// Fails closed. The cost of being wrong the other way is listing somebody's
	// games against their setting.
	if gamesEnabled("") != false {
		t.Error("no preferences directory should mean off")
	}
}

func TestLaunchRefusesWhatIsNotInstalled(t *testing.T) {
	b := gamesBindings(enabledDir(t, true))
	// The rescan is the check: whatever the page believes, only an id found on
	// this disk right now reaches a URI. This id is invented and cannot be
	// installed anywhere.
	res := call1(t, b, "lancastLaunchGame", "999999999")
	if res["ok"] != false {
		t.Errorf("launch = %v, want a refusal", res)
	}
}

func TestLaunchRefusesAnIdThatIsNotAnId(t *testing.T) {
	b := gamesBindings(enabledDir(t, true))
	for _, bad := range []string{"", "abc", "700010 && calc", "../../x"} {
		if res := call1(t, b, "lancastLaunchGame", bad); res["ok"] != false {
			t.Errorf("launch(%q) = %v, want a refusal", bad, res)
		}
	}
}

func TestOpenFolderRefusesWhatIsNotInstalled(t *testing.T) {
	b := gamesBindings(enabledDir(t, true))
	if res := call1(t, b, "lancastOpenGameFolder", "999999999"); res["ok"] != false {
		t.Errorf("open folder = %v, want a refusal", res)
	}
}

func TestSetGameFlagsRoundTrip(t *testing.T) {
	dir := enabledDir(t, true)
	b := gamesBindings(dir)
	setFlags := b["lancastSetGameFlags"].(func(string, bool, bool) map[string]any)

	if res := setFlags("700010", true, false); res["ok"] != true {
		t.Fatalf("setFlags = %v", res)
	}
	p, err := games.LoadPrefs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !p.IsHidden("700010") || p.IsFavourite("700010") {
		t.Errorf("prefs = %+v, want hidden only", p)
	}

	// Both flags every time, so sending false for both is how something is
	// unhidden — no separate clear, nothing to half-apply.
	if res := setFlags("700010", false, true); res["ok"] != true {
		t.Fatalf("setFlags = %v", res)
	}
	p, _ = games.LoadPrefs(dir)
	if p.IsHidden("700010") || !p.IsFavourite("700010") {
		t.Errorf("prefs = %+v, want favourite only", p)
	}
}

func TestSetGameFlagsWorksForAGameThatIsNotInstalled(t *testing.T) {
	// Deliberate: hiding is a note about a game, not an act on the machine. A
	// game uninstalled today may be back next week, and dropping the note
	// because it is absent would quietly unhide it.
	dir := enabledDir(t, true)
	b := gamesBindings(dir)
	setFlags := b["lancastSetGameFlags"].(func(string, bool, bool) map[string]any)
	if res := setFlags("999999999", true, false); res["ok"] != true {
		t.Errorf("setFlags = %v, want it recorded anyway", res)
	}
}

func TestSetGameFlagsNeedsAGame(t *testing.T) {
	b := gamesBindings(enabledDir(t, true))
	setFlags := b["lancastSetGameFlags"].(func(string, bool, bool) map[string]any)
	if res := setFlags("", true, false); res["ok"] != false {
		t.Errorf("setFlags with no id = %v, want a refusal", res)
	}
}

func TestOpenFolderNeedsAFolder(t *testing.T) {
	if err := openFolder(""); err == nil {
		t.Error("an empty path should not be opened")
	}
	if err := openFolder(t.TempDir() + "/not-there"); err == nil {
		t.Error("a missing folder should not be opened")
	}
}
