package artwork

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

/*
 * What a cache limit is allowed to delete.
 *
 * The order is the whole design, and it is chosen by what can be recovered: an
 * orphan is waste nothing will ask for, a derived size is one decode away from
 * coming back, and a live original needs a provider, a key and a network.
 *
 * So the case that matters most is the one where the cap is impossible to meet
 * — a library whose live originals alone exceed it. Deleting one would turn a
 * disk-space setting into "some of your posters are gone now", which is not
 * what anybody means by a cache limit.
 */

func hashOf(n byte) string { return strings.Repeat(string("0123456789abcdef"[n%16]), 64) }

// write puts one artwork file of a given size into the cache.
func write(t *testing.T, root, hash string, size Size, bytes int) {
	t.Helper()
	dir := filepath.Join(root, hash[:2], hash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, string(size)+".jpg"),
		make([]byte, bytes), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(root, hash string, size Size) bool {
	_, err := os.Stat(filepath.Join(root, hash[:2], hash, string(size)+".jpg"))
	return err == nil
}

func TestAnOrphanGoesEvenWithNoLimit(t *testing.T) {
	// Waste under any policy: nothing will ever ask for it, and removing it
	// cannot cost a visible poster.
	root := t.TempDir()
	live, dead := hashOf(1), hashOf(2)
	write(t, root, live, SizeOriginal, 100)
	write(t, root, dead, SizeOriginal, 100)

	rep, err := Prune(root, map[string]bool{live: true}, everythingSettled, 0)
	if err != nil {
		t.Fatal(err)
	}
	if exists(root, dead, SizeOriginal) {
		t.Error("an orphan survived")
	}
	if !exists(root, live, SizeOriginal) {
		t.Error("a referenced original was deleted")
	}
	if rep.OrphanFiles != 1 || rep.OrphanBytes != 100 {
		t.Errorf("report = %+v", rep)
	}
}

func TestALiveOriginalIsNeverDeleted(t *testing.T) {
	/*
	 * The safety property, asserted against a cap far below what the live
	 * originals occupy. It is the only copy that cannot be rebuilt without
	 * going back to a provider.
	 */
	root := t.TempDir()
	a, b := hashOf(3), hashOf(4)
	write(t, root, a, SizeOriginal, 1000)
	write(t, root, b, SizeOriginal, 1000)

	rep, err := Prune(root, map[string]bool{a: true, b: true}, everythingSettled, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !exists(root, a, SizeOriginal) || !exists(root, b, SizeOriginal) {
		t.Fatal("a live original was deleted to meet a size limit")
	}
	// And it says so rather than reporting success it did not achieve.
	if rep.OverBy != 1990 {
		t.Errorf("OverBy = %d, want the shortfall reported", rep.OverBy)
	}
}

func TestDerivedSizesGoBeforeOriginals(t *testing.T) {
	// A thumbnail is a decode away from coming back, so it is what a cap
	// spends first.
	root := t.TempDir()
	h := hashOf(5)
	write(t, root, h, SizeOriginal, 500)
	write(t, root, h, SizeThumb, 100)
	write(t, root, h, SizePoster, 100)

	if _, err := Prune(root, map[string]bool{h: true}, everythingSettled, 500); err != nil {
		t.Fatal(err)
	}
	if !exists(root, h, SizeOriginal) {
		t.Error("the original went before the derived sizes")
	}
	if exists(root, h, SizeThumb) && exists(root, h, SizePoster) {
		t.Error("nothing was dropped to meet the limit")
	}
}

func TestNothingIsTouchedWhenTheCacheIsUnderTheLimit(t *testing.T) {
	root := t.TempDir()
	h := hashOf(6)
	write(t, root, h, SizeOriginal, 100)
	write(t, root, h, SizeThumb, 50)

	rep, err := Prune(root, map[string]bool{h: true}, everythingSettled, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	if rep.DerivedFiles != 0 || !exists(root, h, SizeThumb) {
		t.Errorf("dropped something while under the limit: %+v", rep)
	}
}

func TestANilLiveSetIsRefused(t *testing.T) {
	/*
	 * The difference between "this library references no artwork" and "I could
	 * not find out" is the entire cache. A nil map is what a caller that
	 * dropped an error would hand over, so it is refused rather than obeyed.
	 */
	root := t.TempDir()
	h := hashOf(7)
	write(t, root, h, SizeOriginal, 100)

	if _, err := Prune(root, nil, everythingSettled, 0); err == nil {
		t.Error("a nil live set was accepted")
	}
	if !exists(root, h, SizeOriginal) {
		t.Error("a nil live set deleted the cache")
	}
}

func TestSomethingThatIsNotArtworkIsLeftAlone(t *testing.T) {
	// The cache directory is LANcast's, but deleting a file it does not
	// recognise is how a tool earns a reputation.
	root := t.TempDir()
	stray := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(stray, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Prune(root, map[string]bool{}, everythingSettled, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Error("a file that was not artwork was removed")
	}
}

func TestTotalBytesCountsWhatIsThere(t *testing.T) {
	root := t.TempDir()
	h := hashOf(8)
	write(t, root, h, SizeOriginal, 300)
	write(t, root, h, SizeThumb, 200)

	got, err := TotalBytes(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != 500 {
		t.Errorf("TotalBytes = %d, want 500", got)
	}
}

// everythingSettled treats every file as older than the read of the live set,
// which is what the tests above, about what an orphan is, need.
var everythingSettled = time.Now().Add(24 * time.Hour)

// R08-7: a file written after the live set was read is not yet known to be an
// orphan, whatever the set says, and is left alone; one older is removed.
func TestPruneLeavesWhatWasWrittenDuringTheWalk(t *testing.T) {
	root := t.TempDir()
	old := strings.Repeat("a", 64)
	fresh := strings.Repeat("b", 64)
	write(t, root, old, SizeOriginal, 100)
	write(t, root, fresh, SizeOriginal, 100)
	settled := time.Now().Add(-time.Minute)
	oldFile := filepath.Join(root, old[:2], old, string(SizeOriginal)+".jpg")
	if err := os.Chtimes(oldFile, settled.Add(-time.Hour), settled.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	// Neither is referenced: the old one is an orphan, the fresh one's row
	// has simply not been written yet.
	rep, err := Prune(root, map[string]bool{}, settled, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rep.OrphanFiles != 1 {
		t.Errorf("removed %d orphans, want 1", rep.OrphanFiles)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("the old orphan survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, fresh[:2], fresh, string(SizeOriginal)+".jpg")); err != nil {
		t.Errorf("a file written after the live set was read was deleted: %v", err)
	}
}
