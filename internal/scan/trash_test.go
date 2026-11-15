package scan

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

/*
 * The conditions under which a scan may delete what it marked missing.
 *
 * "Scanning marks missing, never deletes" is about an unmounted drive, and a
 * setting that empties the trash after every scan is exactly the shape that
 * rule fears. These are the cases where it must refuse — each one a way a
 * library can look empty while being entirely intact.
 */

func TestAFailedScanMayNotEmptyTheTrash(t *testing.T) {
	v := MayEmptyTrash(Progress{State: StateFailed, FilesSeen: 900})
	if v.Allowed {
		t.Error("a failed scan was trusted to say what is missing")
	}
}

func TestASkippedLocationStopsIt(t *testing.T) {
	/*
	 * The multi-root case, and the subtle one.
	 *
	 * A skipped location's files are never marked missing, so its own rows are
	 * safe — but rows marked missing by an *earlier* scan are still in the
	 * trash, and this scan is in no position to say whether they came back.
	 */
	v := MayEmptyTrash(Progress{
		FilesSeen:    900,
		RootsSkipped: []SkippedRoot{{ID: 2, Path: "Y:/Media"}},
	})
	if v.Allowed {
		t.Error("the trash was emptied while a location could not be read")
	}
}

func TestAnEmptyWalkStopsIt(t *testing.T) {
	// A share remounted at the wrong path reads fine and holds nothing. The
	// walk is honest and the conclusion would not be.
	v := MayEmptyTrash(Progress{FilesSeen: 0})
	if v.Allowed {
		t.Error("a walk that saw nothing was trusted to say everything is missing")
	}
	if v.Reason == "" {
		t.Error("a refusal with no reason cannot be read in a log")
	}
}

func TestAnOrdinaryScanMayEmptyIt(t *testing.T) {
	v := MayEmptyTrash(Progress{FilesSeen: 1542, ItemsMissing: 62})
	if !v.Allowed {
		t.Errorf("an ordinary scan was refused: %s", v.Reason)
	}
}

/*
 * And it happens, which a rule-only test cannot show.
 *
 * The verdict function is pure and the wiring is where this could silently do
 * nothing — a switch somebody turned on that never fires is indistinguishable
 * from one that fired and found nothing.
 */
func TestAScanEmptiesTheTrashWhenAskedTo(t *testing.T) {
	sc, st := newScanner(t)
	root := t.TempDir()
	writeFile(t, root, "Kept (2020).mkv", 16)
	gone := writeFile(t, root, "Gone (2019).mkv", 16)

	lib, err := st.CreateLibrary(context.Background(), "Media", "movie", root)
	if err != nil {
		t.Fatal(err)
	}
	scanAndWait(t, sc, *lib)

	// The file leaves; the next scan marks its row missing rather than removing
	// it, which is the behaviour this setting acts on and must not change.
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	scanAndWait(t, sc, *lib)

	n, err := st.TrashCount(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("trash holds %d rows, want the one missing film", n)
	}

	// Off by default: a scan with nobody asking must leave it alone.
	scanAndWait(t, sc, *lib)
	if n, _ := st.TrashCount(context.Background(), lib.ID); n != 1 {
		t.Errorf("the trash was emptied without being asked; %d rows left", n)
	}

	sc.EmptyTrashWhen(func() bool { return true })
	scanAndWait(t, sc, *lib)
	if n, _ := st.TrashCount(context.Background(), lib.ID); n != 0 {
		t.Errorf("the trash still holds %d rows after a scan that was asked to empty it", n)
	}

	// And the film that is still there was not taken with it: emptying the
	// trash must remove the missing rows and only those.
	_, total, err := st.ListItems(context.Background(),
		store.ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Errorf("library lists %d items, want the one whose file is still on disk", total)
	}
}

/*
 * A scan is not finished until the trash is.
 *
 * `State` leaving `StateRunning` is how everything else learns a scan is over —
 * the client polls it, and so does every test that waits for one. Emptying the
 * trash *after* that flip meant a scan announced itself finished and then went
 * on deleting rows, so an observer could refresh in the gap and see rows that
 * were already condemned.
 *
 * That is this project's most-repeated bug in a new place: the request
 * succeeds, the server is right, and only the picture is stale — except here
 * the server was briefly wrong too, because it said finished before it was.
 *
 * It surfaced as `TestAScanEmptiesTheTrashWhenAskedTo` failing on CI and never
 * on a desk, which is the shape of a race that needs a slow machine. Waiting
 * for a scan and hoping cannot test the ordering, so this observes from inside
 * the window: the predicate is consulted at the moment the decision is made,
 * and the scan must still call itself running then.
 */
func TestAScanIsNotFinishedUntilTheTrashIs(t *testing.T) {
	sc, st := newScanner(t)
	root := t.TempDir()
	writeFile(t, root, "Kept (2020).mkv", 16)
	gone := writeFile(t, root, "Gone (2019).mkv", 16)

	lib, err := st.CreateLibrary(context.Background(), "Media", "movie", root)
	if err != nil {
		t.Fatal(err)
	}
	scanAndWait(t, sc, *lib)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	scanAndWait(t, sc, *lib)

	var during State
	var asked bool
	sc.EmptyTrashWhen(func() bool {
		asked = true
		// Read the way a client reads it. Everything that changes what the
		// library holds is still ahead of this point.
		during = sc.Status(lib.ID).State
		return true
	})
	scanAndWait(t, sc, *lib)

	if !asked {
		t.Fatal("the scan never consulted the setting, so this proves nothing")
	}
	if during != StateRunning {
		t.Errorf("the scan reported %q while it was still deciding whether to "+
			"empty the trash — anything polling for completion can refresh in "+
			"that gap and see rows that are already condemned", during)
	}
	if n, _ := st.TrashCount(context.Background(), lib.ID); n != 0 {
		t.Errorf("the trash still holds %d rows", n)
	}
}

/*
 * One folder the walk could not list, and the trash is left alone (R08-2).
 *
 * The location itself reads fine, so the scan succeeds and is not "skipped";
 * the folder inside it is, and every known file under it is unseen and marked
 * missing. That is a fact about the walk. Emptying the trash on it would delete
 * those films with their history because of an ACL or one SMB hiccup.
 */
func TestAnUnreadableFolderStopsIt(t *testing.T) {
	sc, st := newScanner(t)
	root := t.TempDir()
	writeFile(t, root, "Kept (2020).mkv", 16)
	writeFile(t, root, "Films B-D/Brazil (1985).mkv", 16)
	writeFile(t, root, "Films B-D/Dune (1984).mkv", 16)

	lib, err := st.CreateLibrary(context.Background(), "Media", "movie", root)
	if err != nil {
		t.Fatal(err)
	}
	scanAndWait(t, sc, *lib)
	_, before, err := st.ListItems(context.Background(), store.ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	if before != 3 {
		t.Fatalf("library lists %d items before the folder went unreadable, want 3", before)
	}

	sub := filepath.Join(root, "Films B-D")
	makeUnreadable(t, sub)
	if _, err := os.ReadDir(sub); err == nil {
		// Root, or an administrator whose privileges override the deny: the
		// folder still lists, so this machine cannot stage the failure.
		t.Skip("the folder is still listable here, so the failure cannot be staged")
	}

	sc.EmptyTrashWhen(func() bool { return true })
	p := scanAndWait(t, sc, *lib)

	if p.Unreadable == 0 {
		t.Errorf("the scan did not count the folder it could not list: %+v", p.Issues)
	}
	if v := MayEmptyTrash(p); v.Allowed {
		t.Error("a scan that could not list a folder was trusted to say what is missing")
	}
	// Marked missing, as the walk honestly found — and not deleted.
	if n, _ := st.TrashCount(context.Background(), lib.ID); n != 2 {
		t.Errorf("trash holds %d rows, want the two films under the unreadable folder", n)
	}
	// Missing rows are listed unless the filter excludes them.
	_, total, err := st.ListItems(context.Background(), store.ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Errorf("library holds %d rows after the scan, want all 3: the trash was emptied", total)
	}
}
