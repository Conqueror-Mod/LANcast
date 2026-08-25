package scan

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

/*
 * Sampled fingerprints (ADR 0042).
 *
 * The property that matters is not "the hash is stable" but "two files that
 * differ in a place the sampler looks are told apart, and the report never
 * claims more than it checked".
 */

func write(t *testing.T, dir, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

// A big file so the sampling path runs rather than the read-it-all path.
func big(seed byte) []byte {
	b := bytes.Repeat([]byte{seed}, 4*sampleWindow)
	return b
}

func TestIdenticalBytesFingerprintAlike(t *testing.T) {
	d := t.TempDir()
	a := write(t, d, "a.mkv", big(7))
	b := write(t, d, "b.mkv", big(7))

	fa, err := FingerprintFile(a)
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	fb, err := FingerprintFile(b)
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	if fa.Hash != fb.Hash {
		t.Errorf("identical files hashed differently:\n  %s\n  %s", fa.Hash, fb.Hash)
	}
	if fa.Size != fb.Size {
		t.Errorf("sizes differ: %d vs %d", fa.Size, fb.Size)
	}
}

/*
 * Different lengths must never collide, even when every sampled window would
 * match. This is the realistic near-miss: two files of constant bytes.
 */
func TestDifferentSizesNeverCollide(t *testing.T) {
	d := t.TempDir()
	a := write(t, d, "a.mkv", bytes.Repeat([]byte{1}, 4*sampleWindow))
	b := write(t, d, "b.mkv", bytes.Repeat([]byte{1}, 4*sampleWindow+1))

	fa, _ := FingerprintFile(a)
	fb, _ := FingerprintFile(b)
	if fa.Hash == fb.Hash {
		t.Error("files of different lengths produced one hash")
	}
}

// Each of the three windows has to actually be looked at, or the sampler is
// two windows and a comment.
func TestEachSampledWindowIsRead(t *testing.T) {
	d := t.TempDir()
	base := big(3)
	fa, _ := FingerprintFile(write(t, d, "base.mkv", base))

	for name, at := range map[string]int{
		"head":   0,
		"middle": len(base) / 2,
		"tail":   len(base) - 1,
	} {
		altered := append([]byte(nil), base...)
		altered[at] ^= 0xFF
		f, err := FingerprintFile(write(t, d, name+".mkv", altered))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if f.Hash == fa.Hash {
			t.Errorf("a change at the %s was not sampled", name)
		}
	}
}

/*
 * A file smaller than three windows is hashed whole, which is both correct and
 * cheaper. It must still tell two such files apart.
 */
func TestSmallFilesAreHashedWhole(t *testing.T) {
	d := t.TempDir()
	fa, err := FingerprintFile(write(t, d, "a.mkv", []byte("hello")))
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	fb, _ := FingerprintFile(write(t, d, "b.mkv", []byte("hellp")))
	if fa.Hash == fb.Hash {
		t.Error("two different small files produced one hash")
	}
	if fa.Size != 5 {
		t.Errorf("size = %d, want 5", fa.Size)
	}
}

// An unreadable file is an absence of evidence, not a mismatch. The caller has
// to be able to tell those apart, so this is an error rather than a hash.
func TestAMissingFileIsAnError(t *testing.T) {
	if _, err := FingerprintFile(filepath.Join(t.TempDir(), "nope.mkv")); err == nil {
		t.Error("a missing file fingerprinted without error")
	}
}

/*
 * The edition marker reaches the database (ADR 0042).
 *
 * This test exists because the column, the parser field and the API were all
 * built and wired to each other while nothing carried the value from the parse
 * into the row — the schema had an `edition` column that would have stayed
 * empty for ever, and every unit test on either side still passed. An
 * end-to-end assertion is the only kind that could have caught it.
 */
func TestEditionMarkerIsScannedIntoTheRow(t *testing.T) {
	sc, st := newScanner(t)
	dir := t.TempDir()
	writeFile(t, dir, "Blade Runner (Director's Cut) (1982).mkv", 16)
	writeFile(t, dir, "Blade Runner (1982).mkv", 16)

	lib, err := st.CreateLibrary(context.Background(), "Films", "movie", dir)
	if err != nil {
		t.Fatalf("library: %v", err)
	}
	scanAndWait(t, sc, *lib)

	items, _, err := st.ListItems(context.Background(), store.ItemFilter{
		LibraryID: lib.ID, Limit: 50,
	})
	if err != nil {
		t.Fatalf("items: %v", err)
	}

	var withMarker, without int
	for _, it := range items {
		switch {
		case it.Edition != nil && *it.Edition == "Director's Cut":
			withMarker++
		case it.Edition == nil:
			without++
		default:
			t.Errorf("unexpected edition %q on %q", *it.Edition, it.Title)
		}
		// Both rows are the same work: stripping is what makes the edition
		// match, and keeping the marker must not have changed that.
		if it.Title != "Blade Runner" {
			t.Errorf("title = %q, want %q", it.Title, "Blade Runner")
		}
	}
	if withMarker != 1 || without != 1 {
		t.Errorf("got %d marked and %d unmarked rows, want 1 and 1", withMarker, without)
	}
}
