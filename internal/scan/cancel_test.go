package scan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"lancast/internal/store"
)

// cancelAfter makes the scanner cancel this library's scan once the walk has
// handled n files, so the cancel lands at an exact point mid-walk.
func cancelAfter(sc *Scanner, n int32) {
	var handled int32
	sc.afterFile = func(id int64) {
		if atomic.AddInt32(&handled, 1) == n {
			sc.Cancel(id)
		}
	}
}

func waitDone(t *testing.T, sc *Scanner, lib store.Library) Progress {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if p := sc.Status(lib.ID); p.State != StateRunning {
			return p
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("scan did not finish within 15s")
	return Progress{}
}

func missingTitles(t *testing.T, st *store.Store, lib store.Library) map[string]bool {
	t.Helper()
	items, _, err := st.ListItems(context.Background(), store.ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, it := range items {
		if it.Missing {
			out[it.Title] = true
		}
	}
	return out
}

/*
 * The rule this whole change has to keep: a cancelled scan marks nothing it
 * had not reached as missing.
 *
 * Twenty films are scanned in full, then two are deleted from disk and a
 * second scan is cancelled after three files. The walk never finished the
 * location, so the two deletions must not be recorded — and nor must the
 * fifteen files it never reached, which is the disaster this guards against:
 * a cancel that was read as "everything not seen is gone".
 *
 * Then the control: a full scan with nothing cancelled marks exactly the two
 * deleted files missing, so the test is proven able to see a deletion.
 */
func TestACancelledScanMarksNothingMissing(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)
	for i := 0; i < 20; i++ {
		writeFile(t, root, fmt.Sprintf("Film %02d (2001).mkv", i), 10)
	}
	first := scanAndWait(t, sc, lib)
	if first.FilesSeen != 20 {
		t.Fatalf("fixture: first scan saw %d files, want 20", first.FilesSeen)
	}

	for _, gone := range []string{"Film 05 (2001).mkv", "Film 17 (2001).mkv"} {
		if err := os.Remove(filepath.Join(root, gone)); err != nil {
			t.Fatal(err)
		}
	}

	cancelAfter(sc, 3)
	if _, err := sc.Start(lib); err != nil {
		t.Fatal(err)
	}
	p := waitDone(t, sc, lib)
	if p.State != StateCancelled {
		t.Fatalf("State = %v, want cancelled", p.State)
	}
	if p.FilesSeen >= 18 {
		t.Fatalf("FilesSeen = %d; the cancel did not stop the walk early", p.FilesSeen)
	}
	if m := missingTitles(t, st, lib); len(m) != 0 {
		t.Fatalf("a cancelled scan marked %d item(s) missing: %v", len(m), m)
	}
	if p.ItemsMissing != 0 {
		t.Errorf("ItemsMissing = %d, want 0", p.ItemsMissing)
	}

	// Control: the same library, scanned to the end.
	sc.afterFile = nil
	full := scanAndWait(t, sc, lib)
	if full.State != StateIdle {
		t.Fatalf("control scan State = %v", full.State)
	}
	m := missingTitles(t, st, lib)
	if len(m) != 2 || !m["Film 05"] || !m["Film 17"] {
		t.Fatalf("control: missing = %v, want exactly Film 05 and Film 17", m)
	}
}

/*
 * A location the walk finished before the cancel was reconciled as normal;
 * the one it was in when cancelled was not.
 *
 * Reconciliation is per location (#228), and this is that rule meeting a
 * cancel: what was finished is true, what was interrupted says nothing.
 */
func TestACancelKeepsFinishedLocationsAndLeavesTheInterruptedOne(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)
	_, second := twoRoots(t, sc, st, lib, root)
	for i := 0; i < 4; i++ {
		writeFile(t, root, fmt.Sprintf("First %d (2001).mkv", i), 10)
		writeFile(t, second, fmt.Sprintf("Second %d (2002).mkv", i), 10)
	}
	scanAndWait(t, sc, lib)

	if err := os.Remove(filepath.Join(root, "First 2 (2001).mkv")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(second, "Second 3 (2002).mkv")); err != nil {
		t.Fatal(err)
	}

	// The first location has three files left; cancel on the second file of
	// the second location.
	cancelAfter(sc, 5)
	if _, err := sc.Start(lib); err != nil {
		t.Fatal(err)
	}
	p := waitDone(t, sc, lib)
	if p.State != StateCancelled {
		t.Fatalf("State = %v, want cancelled", p.State)
	}
	m := missingTitles(t, st, lib)
	if !m["First 2"] {
		t.Error("the first location was walked to the end, so its deletion should be recorded")
	}
	if m["Second 3"] {
		t.Error("the interrupted location's deletion was recorded; its walk never finished")
	}
	if len(m) != 1 {
		t.Errorf("missing = %v, want only First 2", m)
	}
}

// A cancelled scan never empties the trash: it is no more evidence about
// what is missing than a failed one.
func TestACancelledScanDoesNotEmptyTheTrash(t *testing.T) {
	v := MayEmptyTrash(Progress{State: StateCancelled, FilesSeen: 500})
	if v.Allowed {
		t.Fatal("a cancelled scan was allowed to empty the trash")
	}
}

func TestCancelWithNothingRunning(t *testing.T) {
	sc, st := newScanner(t)
	lib, _ := fixture(t, sc, st)
	if sc.Cancel(lib.ID) {
		t.Error("Cancel reported stopping a scan that was not running")
	}
	if ids := sc.CancelAll(); len(ids) != 0 {
		t.Errorf("CancelAll stopped %v with nothing running", ids)
	}
}

// After a cancel, the library scans again normally: the cancel was for that
// scan, not for the library.
func TestAScanAfterACancelRunsToTheEnd(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)
	for i := 0; i < 6; i++ {
		writeFile(t, root, fmt.Sprintf("Film %d (2001).mkv", i), 10)
	}
	cancelAfter(sc, 2)
	if _, err := sc.Start(lib); err != nil {
		t.Fatal(err)
	}
	if p := waitDone(t, sc, lib); p.State != StateCancelled {
		t.Fatalf("State = %v, want cancelled", p.State)
	}
	sc.afterFile = nil
	p := scanAndWait(t, sc, lib)
	if p.State != StateIdle || p.FilesSeen != 6 {
		t.Fatalf("after a cancel: State %v, FilesSeen %d; want idle and 6", p.State, p.FilesSeen)
	}
	if sc.Cancel(lib.ID) {
		t.Error("a finished scan left a cancel behind")
	}
}
