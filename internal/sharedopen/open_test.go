package sharedopen

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// A file held open mid-read can still be deleted by its owner, and whoever
// holds it reads on to the end. On Windows os.Open fails this at the Remove.
func TestAFileHeldMidReadCanBeDeleted(t *testing.T) {
	p := filepath.Join(t.TempDir(), "film.mkv")
	if err := os.WriteFile(p, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	head := make([]byte, 4)
	if _, err := io.ReadFull(f, head); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatalf("deleting a file held mid-read: %v", err)
	}
	rest, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("reading after the delete: %v", err)
	}
	if string(head)+string(rest) != "0123456789" {
		t.Fatalf("read %q%q", head, rest)
	}
}

// The same, for an overwrite in place. (A rename over a held file is still
// refused on Windows: MoveFileEx does not take POSIX semantics, where a
// delete does.)
func TestAFileHeldMidReadCanBeOverwritten(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "film.mkv")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := os.WriteFile(p, []byte("new"), 0o644); err != nil {
		t.Fatalf("overwriting a held file: %v", err)
	}
}

func TestADirectoryIsNotOpenedAsAFileOrIsReportedAsOne(t *testing.T) {
	f, err := Open(t.TempDir())
	if err != nil {
		return // Windows: refused outright
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.IsDir() {
		t.Fatalf("a directory opened and did not say so: %v", err)
	}
}

func TestAMissingFileIsAnError(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "absent")); !os.IsNotExist(err) {
		t.Fatalf("want not-exist, got %v", err)
	}
}
