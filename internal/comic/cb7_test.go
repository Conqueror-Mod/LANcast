package comic

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testdata/twelve.cb7 was made with 7-Zip, solid, from twelve 40×60 PNGs
// named p1 … p12, whose red channel is the page number, plus a ComicInfo.xml
// and a macOS shadow copy of p1 under __MACOSX/. Neither of those is a page.
func TestACB7ReadsLikeACBZ(t *testing.T) {
	t.Cleanup(drainShelf)
	b, err := OpenBook("testdata/twelve.cb7")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.Len() != 12 {
		t.Fatalf("%d pages, want 12", b.Len())
	}
	// In page order, not name order: p2 is the second page, not p10. The
	// last page is read first, which in a solid archive means decompressing
	// every page before it; then the first, which means starting again.
	for _, n := range []int{11, 0, 1, 9} {
		body, ctype, err := b.Page(n)
		if err != nil {
			t.Fatalf("page %d: %v", n, err)
		}
		if ctype != "image/png" {
			t.Fatalf("page %d is %q", n, ctype)
		}
		img, err := png.Decode(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("page %d: %v", n, err)
		}
		r, _, _, _ := img.At(0, 0).RGBA()
		if got := int(r >> 8); got != n+1 {
			t.Fatalf("page %d shows p%d", n, got)
		}
	}
	if _, _, err := b.Page(12); err == nil {
		t.Fatal("a page past the end was read")
	}
}

func TestACB7ThatIsNotOneIsRefused(t *testing.T) {
	if _, err := openCB7("cb7.go"); err == nil {
		t.Fatal("a Go source file opened as a 7-Zip archive")
	}
}

// drainShelf closes every archive nobody holds, so a test's files are free
// to be removed when it ends.
func drainShelf() {
	shelf.mu.Lock()
	defer shelf.mu.Unlock()
	for _, a := range shelf.open {
		a.stale = true
		shelf.closeIfIdleLocked(a)
	}
	shelf.open = nil
}

func copyFixture(t *testing.T, dir, name string) string {
	t.Helper()
	t.Cleanup(drainShelf)
	raw, err := os.ReadFile("testdata/twelve.cb7")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func shelved(p string) *cb7Archive {
	shelf.mu.Lock()
	defer shelf.mu.Unlock()
	for _, a := range shelf.open {
		if a.path == p {
			return a
		}
	}
	return nil
}

// Opening a .cb7 again finds the same archive, still positioned, and pages
// the stream passed are held for the request that wants them.
func TestACB7StaysOpenBetweenPages(t *testing.T) {
	p := copyFixture(t, t.TempDir(), "a.cb7")
	b, _ := OpenBook(p)
	first := shelved(p)
	if first == nil {
		t.Fatal("not on the shelf")
	}
	// Page 1 is p2, stored after p10, p11 and p12: those are passed and held.
	if _, _, err := b.Page(1); err != nil {
		t.Fatal(err)
	}
	b.Close()
	// Stored as p1, p10, p11, p12, p2, …: all four ahead of p2 are held.
	if len(first.held) != 4 {
		t.Fatalf("held %d pages, want p1 and p10–p12", len(first.held))
	}

	b, _ = OpenBook(p)
	if shelved(p) != first || first.refs != 1 {
		t.Fatal("a second open did not share the archive")
	}
	body, _, err := b.Page(9) // p10, from the held pages
	if err != nil {
		t.Fatal(err)
	}
	img, _ := png.Decode(bytes.NewReader(body))
	if r, _, _, _ := img.At(0, 0).RGBA(); r>>8 != 10 {
		t.Fatalf("held page shows p%d", r>>8)
	}
	if len(first.held) != 3 {
		t.Fatalf("a served page stayed held: %d", len(first.held))
	}
	b.Close()
	b.Close() // twice is once
	if first.refs != 0 {
		t.Fatalf("refs = %d", first.refs)
	}
}

// A file replaced since it was opened is opened again; the old archive is
// closed once nobody holds it.
func TestAReplacedCB7IsReopened(t *testing.T) {
	p := copyFixture(t, t.TempDir(), "b.cb7")
	b, _ := OpenBook(p)
	old := shelved(p)
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	b2, err := OpenBook(p)
	if err != nil {
		t.Fatal(err)
	}
	if shelved(p) == old {
		t.Fatal("the old archive was reused for a replaced file")
	}
	if old.closed {
		t.Fatal("closed while still held")
	}
	b.Close()
	if !old.closed {
		t.Fatal("the replaced archive stayed open")
	}
	b2.Close()
}

// No more than shelfSize archives stay open, and the one dropped is the least
// recently used that nobody is reading.
func TestTheShelfHoldsTwo(t *testing.T) {
	dir := t.TempDir()
	var opened []*cb7Archive
	for _, name := range []string{"1.cb7", "2.cb7", "3.cb7"} {
		p := copyFixture(t, dir, name)
		b, err := OpenBook(p)
		if err != nil {
			t.Fatal(err)
		}
		opened = append(opened, shelved(p))
		b.Close()
	}
	if !opened[0].closed || opened[1].closed || opened[2].closed {
		t.Fatalf("closed: %v %v %v", opened[0].closed, opened[1].closed, opened[2].closed)
	}
}

// A .cb7 on the shelf does not stop its owner deleting or replacing it, which
// on Windows os.Open would (openShared).
func TestAHeldCB7CanStillBeDeleted(t *testing.T) {
	p := copyFixture(t, t.TempDir(), "held.cb7")
	b, err := OpenBook(p)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := os.Remove(p); err != nil {
		t.Fatalf("deleting a held comic: %v", err)
	}
	// And it is still readable by whoever holds it.
	if _, _, err := b.Page(0); err != nil {
		t.Fatalf("reading after the delete: %v", err)
	}
}
