package scan

import (
	"context"
	"os"
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
