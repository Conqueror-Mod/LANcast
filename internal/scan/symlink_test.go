package scan

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

/*
 * The scanner imports a link only when it resolves inside the location being
 * walked (linkRefusal). The API re-checks every path regardless; this is what
 * keeps a link it would refuse from becoming a tile that cannot play, and from
 * being probed and fingerprinted on the way.
 */
func TestScanSkipsALinkOutOfTheLibraryAndKeepsOneInside(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)

	real := writeFile(t, root, "Films/Blade Runner (1982).mkv", 10)
	secret := writeFile(t, t.TempDir(), "secret.mkv", 10)

	inside := filepath.Join(root, "Collection", "Alien (1979).mkv")
	alien := writeFile(t, root, "Films/Alien (1979).mkv", 20)
	outside := filepath.Join(root, "Films", "Heat (1995).mkv")
	dangling := filepath.Join(root, "Films", "Ronin (1998).mkv")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(alien, inside); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	if err := os.Symlink(secret, outside); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "nothing.mkv"), dangling); err != nil {
		t.Fatal(err)
	}

	p := scanAndWait(t, sc, lib)

	known, err := st.KnownFiles(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{real, alien, inside} {
		if _, ok := known[want]; !ok {
			t.Errorf("%s was not imported", want)
		}
	}
	for _, refused := range []string{outside, dangling} {
		if _, ok := known[refused]; ok {
			t.Errorf("%s was imported — a link out of the library, or to nothing", refused)
		}
	}
	// The link inside is recorded with its file's size, not the link's.
	if f := known[inside]; f.SizeBytes == nil || *f.SizeBytes != 20 {
		t.Errorf("linked file size = %v, want 20 (the file's, not the link's)", f.SizeBytes)
	}

	reasons := map[string]string{}
	for _, is := range p.Issues {
		reasons[filepath.ToSlash(is.Path)] = is.Reason
	}
	if reasons["Films/Heat (1995).mkv"] != "a link to a file outside this library" {
		t.Errorf("issues = %v, want the outside link named", p.Issues)
	}
	if reasons["Films/Ronin (1998).mkv"] != "a link to nothing" {
		t.Errorf("issues = %v, want the dangling link named", p.Issues)
	}
}

// A link that was imported before this check, and points out, is marked
// missing by the next scan — never deleted.
func TestScanMarksAPreviouslyImportedOutsideLinkMissing(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)
	ctx := context.Background()

	secret := writeFile(t, t.TempDir(), "secret.mkv", 10)
	link := filepath.Join(root, "Heat (1995).mkv")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	roots, err := st.ListRoots(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertItem(ctx, store.ScanFile{
		LibraryID: lib.ID, RootID: roots[0].ID, Path: link, Kind: "movie",
		Title: "Heat", SortTitle: "heat", Container: "mkv", SizeBytes: 10, MTime: 1,
	}); err != nil {
		t.Fatal(err)
	}

	scanAndWait(t, sc, lib)

	known, err := st.KnownFiles(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := known[link]
	if !ok {
		t.Fatal("the row was deleted; a scan marks missing, never deletes")
	}
	if !f.Missing {
		t.Error("the row is not marked missing")
	}
}
