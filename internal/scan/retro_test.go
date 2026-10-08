package scan

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

func retroFixture(t *testing.T, st *store.Store) (store.Library, string) {
	t.Helper()
	root := t.TempDir()
	lib, err := st.CreateLibrary(context.Background(), "Games", "retro", root)
	if err != nil {
		t.Fatal(err)
	}
	return *lib, root
}

func retroItems(t *testing.T, st *store.Store, lib store.Library) map[string]store.Item {
	t.Helper()
	items, _, err := st.ListItems(context.Background(), store.ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]store.Item{}
	for _, it := range items {
		out[filepath.Base(it.Path)] = it
	}
	return out
}

func TestRetroLibraryScansROMsWithPlatforms(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := retroFixture(t, st)

	writeFile(t, root, "N64/Super Mario 64 (USA).z64", 10)
	writeFile(t, root, "N64/Mario Kart 64 (USA).v64", 10)
	writeFile(t, root, "SNES/Super Metroid (Japan, USA) (En,Ja).sfc", 10)
	writeFile(t, root, "Genesis/Sonic the Hedgehog (USA, Europe).bin", 10)
	writeFile(t, root, "Unsorted/Mystery (USA).zip", 10)
	// A disc: the cue is the game, its tracks are not.
	writeFile(t, root, "PS1/Final Fantasy VII (USA) (Disc 1)/Final Fantasy VII (USA) (Disc 1).cue", 10)
	writeFile(t, root, "PS1/Final Fantasy VII (USA) (Disc 1)/Final Fantasy VII (USA) (Disc 1) (Track 1).bin", 10)
	writeFile(t, root, "PS1/Final Fantasy VII (USA) (Disc 1)/Final Fantasy VII (USA) (Disc 1) (Track 2).bin", 10)
	// Not ROMs: a save file, a readme, and a film somebody left here.
	writeFile(t, root, "N64/Super Mario 64 (USA).srm", 10)
	writeFile(t, root, "readme.txt", 10)
	writeFile(t, root, "trailer.mkv", 10)

	p := scanAndWait(t, sc, lib)
	if p.FilesSeen != 6 {
		t.Errorf("FilesSeen = %d, want 6", p.FilesSeen)
	}
	if p.SkippedKind != 1 {
		t.Errorf("SkippedKind = %d, want 1 (the film)", p.SkippedKind)
	}

	got := retroItems(t, st, lib)
	want := map[string]struct{ title, platform string }{
		"Super Mario 64 (USA).z64":               {"Super Mario 64", "n64"},
		"Mario Kart 64 (USA).v64":                {"Mario Kart 64", "n64"},
		"Super Metroid (Japan, USA) (En,Ja).sfc": {"Super Metroid", "snes"},
		"Sonic the Hedgehog (USA, Europe).bin":   {"Sonic the Hedgehog", "genesis"},
		"Mystery (USA).zip":                      {"Mystery", ""},
		"Final Fantasy VII (USA) (Disc 1).cue":   {"Final Fantasy VII", "ps1"},
	}
	if len(got) != len(want) {
		t.Errorf("got %d items, want %d: %v", len(got), len(want), romNames(got))
	}
	for name, w := range want {
		it, ok := got[name]
		if !ok {
			t.Errorf("missing %q", name)
			continue
		}
		if it.Kind != "rom" {
			t.Errorf("%q kind = %q, want rom", name, it.Kind)
		}
		if it.Title != w.title {
			t.Errorf("%q title = %q, want %q", name, it.Title, w.title)
		}
		gotPlatform := ""
		if it.Platform != nil {
			gotPlatform = *it.Platform
		}
		if gotPlatform != w.platform {
			t.Errorf("%q platform = %q, want %q", name, gotPlatform, w.platform)
		}
		if it.ParentID != nil {
			t.Errorf("%q has a parent; a ROM is a top-level game", name)
		}
	}
}

// A rescan of an unchanged retro library changes nothing. `reinterpreted`
// needs a case for every kind a parse can produce: without one, a ROM fell
// to the default, compared "rom" with "other", and every rescan re-recorded
// every game — the bug music had for a whole release.
func TestRetroRescanIsANoOp(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := retroFixture(t, st)
	writeFile(t, root, "N64/Super Mario 64 (USA).z64", 10)
	writeFile(t, root, "GBA/Metroid Fusion (USA).gba", 10)

	scanAndWait(t, sc, lib)
	for _, it := range retroItems(t, st, lib) {
		if err := st.MarkROMChecked(context.Background(), it.ID); err != nil {
			t.Fatal(err)
		}
	}

	p := scanAndWait(t, sc, lib)
	if p.ItemsChanged != 0 {
		t.Errorf("ItemsChanged = %d on an unchanged rescan, want 0", p.ItemsChanged)
	}
	if n, _ := st.PendingROMCount(context.Background()); n != 0 {
		t.Errorf("%d ROMs re-queued by a rescan that changed nothing", n)
	}
}

// A ROM that goes away is marked missing, never deleted.
func TestRetroMissingROMIsMarkedNotDeleted(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := retroFixture(t, st)
	path := writeFile(t, root, "N64/Super Mario 64 (USA).z64", 10)
	scanAndWait(t, sc, lib)

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	scanAndWait(t, sc, lib)

	it, ok := retroItems(t, st, lib)["Super Mario 64 (USA).z64"]
	if !ok {
		t.Fatal("the row was deleted")
	}
	if !it.Missing {
		t.Error("the row was not marked missing")
	}
}

func romNames(m map[string]store.Item) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// A multi-disc game is one row: its .m3u. The discs it lists are not games of
// their own, whether they sit beside it or in a subfolder below it, and an
// .m3u that lists no disc is not a game at all.
func TestRetroMultiDiscGameIsOneRow(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := retroFixture(t, st)

	// Beside: discs and list in one folder.
	writeFile(t, root, "PS1/FF7/Final Fantasy VII (USA).m3u", 0)
	m3u := filepath.Join(root, "PS1", "FF7", "Final Fantasy VII (USA).m3u")
	if err := os.WriteFile(m3u, []byte("Final Fantasy VII (USA) (Disc 1).cue\nFinal Fantasy VII (USA) (Disc 2).cue\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "PS1/FF7/Final Fantasy VII (USA) (Disc 1).cue", 10)
	writeFile(t, root, "PS1/FF7/Final Fantasy VII (USA) (Disc 1).bin", 10)
	writeFile(t, root, "PS1/FF7/Final Fantasy VII (USA) (Disc 2).cue", 10)
	writeFile(t, root, "PS1/FF7/Final Fantasy VII (USA) (Disc 2).bin", 10)

	// Below: the list in the game's folder, the discs hidden under it.
	m3u2 := filepath.Join(root, "PS1", "MGS", "Metal Gear Solid (USA).m3u")
	writeFile(t, root, "PS1/MGS/.hidden/Metal Gear Solid (USA) (Disc 1).chd", 10)
	writeFile(t, root, "PS1/MGS/.hidden/Metal Gear Solid (USA) (Disc 2).chd", 10)
	if err := os.WriteFile(m3u2, []byte(".hidden/Metal Gear Solid (USA) (Disc 1).chd\n.hidden/Metal Gear Solid (USA) (Disc 2).chd\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A list of nothing playable is not a game.
	notes := filepath.Join(root, "PS1", "notes.m3u")
	if err := os.WriteFile(notes, []byte("readme.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	scanAndWait(t, sc, lib)
	got := retroItems(t, st, lib)
	if len(got) != 2 {
		t.Fatalf("got %d rows, want the two lists: %v", len(got), romNames(got))
	}
	for name, title := range map[string]string{
		"Final Fantasy VII (USA).m3u": "Final Fantasy VII",
		"Metal Gear Solid (USA).m3u":  "Metal Gear Solid",
	} {
		it, ok := got[name]
		if !ok {
			t.Errorf("missing %s", name)
			continue
		}
		if it.Title != title || it.Platform == nil || *it.Platform != "ps1" {
			t.Errorf("%s: title %q platform %v", name, it.Title, it.Platform)
		}
	}
}

// A disc that was a game before its list appeared is marked missing, not
// deleted, once the list covers it.
func TestRetroDiscBecomesPartOfANewList(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := retroFixture(t, st)
	writeFile(t, root, "PS1/Game (USA) (Disc 1).cue", 10)
	writeFile(t, root, "PS1/Game (USA) (Disc 2).cue", 10)
	scanAndWait(t, sc, lib)
	if n := len(retroItems(t, st, lib)); n != 2 {
		t.Fatalf("before the list: %d rows, want 2", n)
	}

	if err := os.WriteFile(filepath.Join(root, "PS1", "Game (USA).m3u"),
		[]byte("Game (USA) (Disc 1).cue\nGame (USA) (Disc 2).cue\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scanAndWait(t, sc, lib)
	present := 0
	for name, it := range retroItems(t, st, lib) {
		if !it.Missing {
			present++
			if name != "Game (USA).m3u" {
				t.Errorf("%s is still a game beside its list", name)
			}
		}
	}
	if present != 1 {
		t.Errorf("%d present rows, want the list alone", present)
	}
}
