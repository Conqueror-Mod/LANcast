package store

import (
	"context"
	"testing"
)

/*
 * Sensitive marks and what they inherit (ADR 0051).
 *
 * The feature is one boolean on a row, so the tests are not about the boolean.
 * They are about the three things that decide whether it is trustworthy: that a
 * folder covers what is inside it, that unmarking the folder does not silently
 * unmark a photo somebody marked on its own, and that a scan cannot clear
 * either — which is the locked-fields rule, and it has the same standing here
 * as it has everywhere else in this project.
 */

// A picture library: one gallery, one nested gallery, photos in both.
type tree struct {
	library                 int64
	folder, inner           int64
	photoA, photoB, photoIn int64
	loose                   int64
}

func makeTree(t *testing.T, s *Store) tree {
	t.Helper()
	ctx := context.Background()
	lib, err := s.CreateLibrary(ctx, "Pictures", "picture", `C:\pics`)
	if err != nil {
		t.Fatal(err)
	}
	var out tree
	out.library = lib.ID

	row := func(kind, path, title string, parent *int64) int64 {
		t.Helper()
		res, err := s.db.Exec(`
			INSERT INTO media_item (library_id, kind, path, title, sort_title,
				parent_id, added_at, updated_at, missing)
			VALUES (?, ?, ?, ?, ?, ?, 1, 1, 0)`,
			lib.ID, kind, path, title, title, parent)
		if err != nil {
			t.Fatal(err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}

	out.folder = row("gallery", `C:\pics\Folder`, "Folder", nil)
	out.inner = row("gallery", `C:\pics\Folder\Inner`, "Inner", &out.folder)
	out.photoA = row("photo", `C:\pics\Folder\a.jpg`, "a", &out.folder)
	out.photoB = row("photo", `C:\pics\Folder\b.jpg`, "b", &out.folder)
	out.photoIn = row("photo", `C:\pics\Folder\Inner\c.jpg`, "c", &out.inner)
	out.loose = row("photo", `C:\pics\loose.jpg`, "loose", nil)
	return out
}

func obscured(t *testing.T, s *Store, id int64) bool {
	t.Helper()
	var v int
	if err := s.db.QueryRow(
		`SELECT sensitive_effective FROM media_item WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v != 0
}

// The whole point: mark the folder, and everything under it is obscured
// without anybody marking it — including a photo two levels down, and
// including the sub-folder itself.
func TestMarkingAFolderCoversWhatIsInside(t *testing.T) {
	s := openTestStore(t)
	tr := makeTree(t, s)

	if err := s.SetSensitive(context.Background(), tr.folder, true); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		id   int64
		name string
	}{
		{tr.folder, "the folder"},
		{tr.inner, "the sub-folder"},
		{tr.photoA, "a photo in it"},
		{tr.photoIn, "a photo in the sub-folder"},
	} {
		if !obscured(t, s, c.id) {
			t.Errorf("%s was not obscured", c.name)
		}
	}
	// And nothing outside it was touched, or marking one folder would blank
	// the library.
	if obscured(t, s, tr.loose) {
		t.Error("a photo outside the marked folder was obscured")
	}
}

// Unmarking puts it back.
func TestUnmarkingAFolderRestoresIt(t *testing.T) {
	s := openTestStore(t)
	tr := makeTree(t, s)
	ctx := context.Background()

	if err := s.SetSensitive(ctx, tr.folder, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSensitive(ctx, tr.folder, false); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{tr.folder, tr.inner, tr.photoA, tr.photoIn} {
		if obscured(t, s, id) {
			t.Errorf("item %d stayed obscured after the folder was unmarked", id)
		}
	}
}

/*
 * The case that decides whether one column would have done.
 *
 * A photo marked on its own, inside a folder that is also marked. Unmarking the
 * folder must leave the photo obscured — the person marked that photo, and
 * clearing one decision as a side effect of clearing a different one is exactly
 * what the locked-fields rule exists to prevent.
 */
func TestUnmarkingAFolderKeepsAPhotoMarkedOnItsOwn(t *testing.T) {
	s := openTestStore(t)
	tr := makeTree(t, s)
	ctx := context.Background()

	if err := s.SetSensitive(ctx, tr.photoA, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSensitive(ctx, tr.folder, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSensitive(ctx, tr.folder, false); err != nil {
		t.Fatal(err)
	}

	if !obscured(t, s, tr.photoA) {
		t.Error("a photo marked on its own was cleared by unmarking the folder above it")
	}
	if obscured(t, s, tr.photoB) {
		t.Error("a photo that was only ever covered by the folder stayed obscured")
	}
}

/*
 * A photo that arrives after the mark.
 *
 * This is the ordering an incremental propagation gets wrong, and the reason
 * the resolution is a recompute: the folder was marked last week and the photo
 * was copied into it this morning, so nothing propagated to it and nothing ever
 * would have. RefreshSensitivity runs at the end of every scan.
 */
func TestAPhotoAddedLaterInheritsTheMark(t *testing.T) {
	s := openTestStore(t)
	tr := makeTree(t, s)
	ctx := context.Background()

	if err := s.SetSensitive(ctx, tr.folder, true); err != nil {
		t.Fatal(err)
	}

	res, err := s.db.Exec(`
		INSERT INTO media_item (library_id, kind, path, title, sort_title,
			parent_id, added_at, updated_at, missing)
		VALUES (?, 'photo', ?, 'new', 'new', ?, 1, 1, 0)`,
		tr.library, `C:\pics\Folder\new.jpg`, tr.folder)
	if err != nil {
		t.Fatal(err)
	}
	newID, _ := res.LastInsertId()

	if obscured(t, s, newID) {
		t.Fatal("the new photo was already obscured before anything resolved it — " +
			"this test is not testing what it claims to")
	}
	if err := s.RefreshSensitivity(ctx, tr.library); err != nil {
		t.Fatal(err)
	}
	if !obscured(t, s, newID) {
		t.Error("a photo added to a marked folder was not obscured by the scan")
	}
}

/*
 * A rescan does not re-litigate the decision.
 *
 * The permanent test, in the manner of the locked-fields one.
 * RefreshSensitivity is the only thing a scan calls, and it must be incapable
 * of removing a mark — it resolves what the marks imply and has no opinion
 * about the marks themselves.
 */
func TestAScanCannotClearAMark(t *testing.T) {
	s := openTestStore(t)
	tr := makeTree(t, s)
	ctx := context.Background()

	if err := s.SetSensitive(ctx, tr.folder, true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := s.RefreshSensitivity(ctx, tr.library); err != nil {
			t.Fatal(err)
		}
	}

	var mark *int
	if err := s.db.QueryRow(
		`SELECT sensitive FROM media_item WHERE id = ?`, tr.folder).Scan(&mark); err != nil {
		t.Fatal(err)
	}
	if mark == nil || *mark != 1 {
		t.Error("a rescan cleared the mark — LANcast has become the thing it replaces")
	}
	if !obscured(t, s, tr.photoA) {
		t.Error("a rescan stopped obscuring what the mark covers")
	}
}

// With nothing marked the library resolves to nothing obscured, and says so
// without walking the tree. The cheap path is the one every library takes.
func TestAnUnmarkedLibraryResolvesToNothing(t *testing.T) {
	s := openTestStore(t)
	tr := makeTree(t, s)
	ctx := context.Background()

	if err := s.RefreshSensitivity(ctx, tr.library); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{tr.folder, tr.photoA, tr.loose} {
		if obscured(t, s, id) {
			t.Errorf("item %d was obscured in a library with no marks", id)
		}
	}
	if on, err := s.SensitiveMarksExist(ctx, tr.library); err != nil || on {
		t.Errorf("SensitiveMarksExist = %v, %v; want false", on, err)
	}
}

// And a resolved value left by a mark that has since been removed is cleared
// rather than left behind, which is the one thing the cheap path still has to do.
func TestTheCheapPathClearsWhatAnOldMarkLeft(t *testing.T) {
	s := openTestStore(t)
	tr := makeTree(t, s)
	ctx := context.Background()

	if _, err := s.db.Exec(
		`UPDATE media_item SET sensitive_effective = 1 WHERE id = ?`, tr.photoA); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshSensitivity(ctx, tr.library); err != nil {
		t.Fatal(err)
	}
	if obscured(t, s, tr.photoA) {
		t.Error("a resolved value outlived the mark that produced it")
	}
}

// The mark is reported separately from what it resolves to, so Unmark can be
// offered on the folder that carries it rather than on every photo inside.
func TestTheItemSaysWhetherTheMarkIsItsOwn(t *testing.T) {
	s := openTestStore(t)
	tr := makeTree(t, s)
	ctx := context.Background()

	if err := s.SetSensitive(ctx, tr.folder, true); err != nil {
		t.Fatal(err)
	}
	folder, err := s.GetItem(ctx, tr.folder, "")
	if err != nil {
		t.Fatal(err)
	}
	photo, err := s.GetItem(ctx, tr.photoA, "")
	if err != nil {
		t.Fatal(err)
	}
	if !folder.Sensitive || !folder.SensitiveOwn {
		t.Errorf("the marked folder reads sensitive=%v own=%v; want both true",
			folder.Sensitive, folder.SensitiveOwn)
	}
	if !photo.Sensitive || photo.SensitiveOwn {
		t.Errorf("a photo inside it reads sensitive=%v own=%v; want true and false",
			photo.Sensitive, photo.SensitiveOwn)
	}
}
