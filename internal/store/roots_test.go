package store

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
)

// ---- overlap rules ----------------------------------------------------------

// The one that a string-prefix implementation gets wrong. `/mnt/films` is a
// prefix of `/mnt/films2` and contains none of it, so a prefix test refuses a
// perfectly good second location for sharing a name with the first.
func TestSiblingRootsWithASharedPrefixDoNotOverlap(t *testing.T) {
	if rootsOverlap(filepath.FromSlash("/mnt/films"), filepath.FromSlash("/mnt/films2")) {
		t.Error("/mnt/films and /mnt/films2 reported as overlapping")
	}
}

func TestNestedRootsOverlapInBothDirections(t *testing.T) {
	parent := filepath.FromSlash("/mnt/media")
	child := filepath.FromSlash("/mnt/media/kids")
	if !rootsOverlap(parent, child) {
		t.Error("parent then child not detected")
	}
	if !rootsOverlap(child, parent) {
		t.Error("child then parent not detected — the check must be symmetric")
	}
}

func TestIdenticalRootsOverlap(t *testing.T) {
	p := filepath.FromSlash("/mnt/media")
	if !rootsOverlap(p, p) {
		t.Error("a root does not overlap itself")
	}
}

// Trailing separators and interior dots are spelling, not structure.
func TestOverlapIgnoresPathSpelling(t *testing.T) {
	a := filepath.FromSlash("/mnt/media/")
	b := filepath.FromSlash("/mnt/media/kids/..")
	if !rootsOverlap(a, b) {
		t.Errorf("%q and %q not detected as the same directory", a, b)
	}
}

func TestUnrelatedRootsDoNotOverlap(t *testing.T) {
	if rootsOverlap(filepath.FromSlash("/mnt/films"), filepath.FromSlash("/srv/music")) {
		t.Error("unrelated roots reported as overlapping")
	}
}

// Windows treats D:\Media and d:\media as one directory; a case-sensitive
// comparison would accept both and then scan the same files twice under two
// ids. Elsewhere they really are two directories and refusing the second would
// be a limitation invented by us.
func TestOverlapFollowsThePlatformOnCase(t *testing.T) {
	a, b := `D:\Media`, `d:\media`
	if runtime.GOOS == "windows" {
		if !rootsOverlap(a, b) {
			t.Error("case-different paths not detected as the same directory on Windows")
		}
		return
	}
	if rootsOverlap("/mnt/Media", "/mnt/media") {
		t.Error("case-different paths treated as one directory off Windows")
	}
}

// ---- root CRUD --------------------------------------------------------------

func libFixture(t *testing.T) (*Store, context.Context, *Library) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "roots.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "Films", "movie", filepath.FromSlash("/mnt/films"))
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	return st, ctx, lib
}

// Creating a library creates its first root, in one transaction. A library with
// no location is a row nothing can scan, resolve or repoint.
func TestCreateLibraryCreatesItsFirstRoot(t *testing.T) {
	st, ctx, lib := libFixture(t)
	roots, err := st.ListRoots(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("roots = %d, want 1", len(roots))
	}
	if roots[0].Path != filepath.FromSlash("/mnt/films") {
		t.Errorf("root path = %q", roots[0].Path)
	}
}

func TestAddRootGivesALibraryASecondLocation(t *testing.T) {
	st, ctx, lib := libFixture(t)
	if _, err := st.AddRoot(ctx, lib.ID, filepath.FromSlash("/mnt/family")); err != nil {
		t.Fatalf("AddRoot: %v", err)
	}
	roots, err := st.ListRoots(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 {
		t.Fatalf("roots = %d, want 2", len(roots))
	}
	// Oldest first, so Library.Path keeps reporting the original.
	if roots[0].Path != filepath.FromSlash("/mnt/films") {
		t.Errorf("roots are not oldest-first: %q", roots[0].Path)
	}
}

func TestAddRootRefusesANestedLocation(t *testing.T) {
	st, ctx, lib := libFixture(t)
	_, err := st.AddRoot(ctx, lib.ID, filepath.FromSlash("/mnt/films/kids"))
	if !errors.Is(err, ErrRootOverlaps) {
		t.Errorf("err = %v, want ErrRootOverlaps", err)
	}
}

// A root that *contains* an existing one is the same ambiguity approached from
// the other side, and is refused the same way.
func TestAddRootRefusesAParentOfAnExistingLocation(t *testing.T) {
	st, ctx, lib := libFixture(t)
	_, err := st.AddRoot(ctx, lib.ID, filepath.FromSlash("/mnt"))
	if !errors.Is(err, ErrRootOverlaps) {
		t.Errorf("err = %v, want ErrRootOverlaps", err)
	}
}

// Across libraries too. media_item.path is UNIQUE, so two libraries pointed at
// one directory would have their items fight rather than coexist — which
// library a directory belongs to is a question with one answer.
func TestAddRootRefusesALocationOwnedByAnotherLibrary(t *testing.T) {
	st, ctx, lib := libFixture(t)
	other, err := st.CreateLibrary(ctx, "Shows", "show", filepath.FromSlash("/mnt/shows"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.AddRoot(ctx, lib.ID, filepath.FromSlash("/mnt/shows/kids"))
	if !errors.Is(err, ErrRootOverlaps) {
		t.Errorf("err = %v, want ErrRootOverlaps", err)
	}
	_ = other
}

func TestAddRootAcceptsAnUnrelatedLocation(t *testing.T) {
	st, ctx, lib := libFixture(t)
	if _, err := st.AddRoot(ctx, lib.ID, filepath.FromSlash("/srv/animation")); err != nil {
		t.Errorf("AddRoot: %v", err)
	}
}

func TestAddRootToANonexistentLibrary(t *testing.T) {
	st, ctx, _ := libFixture(t)
	if _, err := st.AddRoot(ctx, 9999, filepath.FromSlash("/srv/x")); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// ---- removal ----------------------------------------------------------------

// Explicit removal deletes, where a vanished drive marks missing. The rule
// governs what the server may *infer*, not what the user may ask for.
func TestRemoveRootDeletesItsItems(t *testing.T) {
	st, ctx, lib := libFixture(t)
	second, err := st.AddRoot(ctx, lib.ID, filepath.FromSlash("/mnt/family"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO media_item (library_id, root_id, kind, path, title, sort_title, container, added_at, updated_at)
		VALUES (?, ?, 'movie', ?, 'F', 'f', 'mkv', 100, 100)`,
		lib.ID, second.ID, filepath.FromSlash("/mnt/family/f.mkv")); err != nil {
		t.Fatal(err)
	}

	if n, err := st.CountItemsInRoot(ctx, second.ID); err != nil || n != 1 {
		t.Fatalf("CountItemsInRoot = %d, %v, want 1", n, err)
	}
	if err := st.RemoveRoot(ctx, second.ID); err != nil {
		t.Fatalf("RemoveRoot: %v", err)
	}
	if n := countRows(t, st.db, `SELECT COUNT(*) FROM media_item`); n != 0 {
		t.Errorf("items remaining = %d, want 0 — removal cascades", n)
	}
}

// A library with no locations cannot be scanned, resolved or repointed. The
// honest way to remove the final one is to delete the library.
func TestRemoveRootRefusesTheLastOne(t *testing.T) {
	st, ctx, lib := libFixture(t)
	roots, err := st.ListRoots(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveRoot(ctx, roots[0].ID); !errors.Is(err, ErrLastRoot) {
		t.Errorf("err = %v, want ErrLastRoot", err)
	}
}

func TestRemoveRootNotFound(t *testing.T) {
	st, ctx, _ := libFixture(t)
	if err := st.RemoveRoot(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// ---- per-root reconciliation ------------------------------------------------

// The property the whole design turns on: a scan that walked one location must
// compare against what *that* location held, never against the library.
// Otherwise an unplugged drive marks the present root's files missing.
func TestKnownFilesInRootSeesOnlyItsOwnRoot(t *testing.T) {
	st, ctx, lib := libFixture(t)
	first, err := st.ListRoots(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.AddRoot(ctx, lib.ID, filepath.FromSlash("/mnt/family"))
	if err != nil {
		t.Fatal(err)
	}
	ins := func(rootID int64, path string) {
		if _, err := st.db.ExecContext(ctx, `
			INSERT INTO media_item (library_id, root_id, kind, path, title, sort_title, container, added_at, updated_at)
			VALUES (?, ?, 'movie', ?, 'T', 't', 'mkv', 100, 100)`,
			lib.ID, rootID, filepath.FromSlash(path)); err != nil {
			t.Fatal(err)
		}
	}
	ins(first[0].ID, "/mnt/films/a.mkv")
	ins(first[0].ID, "/mnt/films/b.mkv")
	ins(second.ID, "/mnt/family/c.mkv")

	got, err := st.KnownFilesInRoot(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("KnownFilesInRoot returned %d, want 1", len(got))
	}
	if _, ok := got[filepath.FromSlash("/mnt/family/c.mkv")]; !ok {
		t.Errorf("wrong file returned: %v", got)
	}

	// And the library-wide view still sees everything, since the browse layer
	// asks about a library rather than a location.
	all, err := st.KnownFiles(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("KnownFiles returned %d, want 3", len(all))
	}
}

// RootForItem is what containment will resolve against, and it must answer with
// the item's own root rather than the library's first.
func TestRootForItemAnswersWithTheItemsOwnRoot(t *testing.T) {
	st, ctx, lib := libFixture(t)
	second, err := st.AddRoot(ctx, lib.ID, filepath.FromSlash("/mnt/family"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := st.db.ExecContext(ctx, `
		INSERT INTO media_item (library_id, root_id, kind, path, title, sort_title, container, added_at, updated_at)
		VALUES (?, ?, 'movie', ?, 'F', 'f', 'mkv', 100, 100)`,
		lib.ID, second.ID, filepath.FromSlash("/mnt/family/f.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()

	root, err := st.RootForItem(ctx, id)
	if err != nil {
		t.Fatalf("RootForItem: %v", err)
	}
	if root.ID != second.ID {
		t.Errorf("root = %d, want %d — resolving against the first root is the bug this prevents",
			root.ID, second.ID)
	}
	if root.Path != filepath.FromSlash("/mnt/family") {
		t.Errorf("root path = %q", root.Path)
	}
}

// ---- repointing -------------------------------------------------------------

// Repointing is per location: a library with two roots has one move while the
// other stays. Picking by ordinal would rewrite whichever was created first.
func TestRepointRootMovesOnlyThatLocation(t *testing.T) {
	st, ctx, lib := libFixture(t)
	second, err := st.AddRoot(ctx, lib.ID, filepath.FromSlash("/mnt/family"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `
		INSERT INTO media_item (library_id, root_id, kind, path, title, sort_title, container, added_at, updated_at)
		VALUES (?, ?, 'movie', ?, 'F', 'f', 'mkv', 100, 100)`,
		lib.ID, second.ID, filepath.FromSlash("/mnt/family/f.mkv")); err != nil {
		t.Fatal(err)
	}

	if err := st.RepointRoot(ctx, second.ID, filepath.FromSlash("/srv/family")); err != nil {
		t.Fatalf("RepointRoot: %v", err)
	}

	roots, err := st.ListRoots(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if roots[0].Path != filepath.FromSlash("/mnt/films") {
		t.Errorf("the untouched root moved: %q", roots[0].Path)
	}
	if roots[1].Path != filepath.FromSlash("/srv/family") {
		t.Errorf("root path = %q, want /srv/family", roots[1].Path)
	}

	var itemPath string
	if err := st.db.QueryRowContext(ctx,
		`SELECT path FROM media_item WHERE root_id = ?`, second.ID).Scan(&itemPath); err != nil {
		t.Fatal(err)
	}
	if itemPath != filepath.FromSlash("/srv/family/f.mkv") {
		t.Errorf("item path = %q — contents must travel with the root", itemPath)
	}
}

// A repoint can land a root inside another one just as easily as adding it
// there could, and the resulting ambiguity is identical.
func TestRepointRootRefusesToLandInsideAnother(t *testing.T) {
	st, ctx, lib := libFixture(t)
	second, err := st.AddRoot(ctx, lib.ID, filepath.FromSlash("/mnt/family"))
	if err != nil {
		t.Fatal(err)
	}
	err = st.RepointRoot(ctx, second.ID, filepath.FromSlash("/mnt/films/family"))
	if !errors.Is(err, ErrRootOverlaps) {
		t.Errorf("err = %v, want ErrRootOverlaps", err)
	}
}

// Moving a root to where it already is must not trip the overlap check against
// itself.
func TestRepointRootToItsOwnPathIsAllowed(t *testing.T) {
	st, ctx, lib := libFixture(t)
	roots, err := st.ListRoots(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RepointRoot(ctx, roots[0].ID, filepath.FromSlash("/mnt/films")); err != nil {
		t.Errorf("RepointRoot to the same path: %v", err)
	}
}
