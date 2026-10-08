package saves

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func read(t *testing.T, f Files, slot string, prev bool) string {
	t.Helper()
	fh, err := f.Open("u_1", 7, slot, prev)
	if err != nil {
		t.Fatalf("open %s prev=%v: %v", slot, prev, err)
	}
	defer fh.Close()
	b, _ := io.ReadAll(fh)
	return string(b)
}

// The rule: a save is never overwritten without the previous copy kept.
func TestWriteKeepsThePreviousCopy(t *testing.T) {
	f := Files{Dir: t.TempDir()}
	if _, err := f.Write("u_1", 7, "sram", strings.NewReader("first")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Open("u_1", 7, "sram", true); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a first save has a previous copy: %v", err)
	}
	w, err := f.Write("u_1", 7, "sram", strings.NewReader("second"))
	if err != nil {
		t.Fatal(err)
	}
	if w.Size != 6 || len(w.SHA256) != 64 {
		t.Errorf("written = %+v", w)
	}
	if got := read(t, f, "sram", false); got != "second" {
		t.Errorf("current = %q", got)
	}
	if got := read(t, f, "sram", true); got != "first" {
		t.Errorf("previous = %q", got)
	}
}

// A save too large to be real is refused, and leaves both copies as they were.
func TestTooLargeLeavesSavesAlone(t *testing.T) {
	f := Files{Dir: t.TempDir()}
	f.Write("u_1", 7, "state-1", strings.NewReader("old"))
	f.Write("u_1", 7, "state-1", strings.NewReader("current"))
	_, err := f.Write("u_1", 7, "state-1", io.LimitReader(zeroes{}, MaxBytes+10))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
	if read(t, f, "state-1", false) != "current" || read(t, f, "state-1", true) != "old" {
		t.Error("a refused write disturbed the saves")
	}
	entries, _ := os.ReadDir(filepath.Dir(f.path("u_1", 7, "state-1", false)))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Errorf("a temporary file was left: %s", e.Name())
		}
	}
}

type zeroes struct{}

func (zeroes) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// Slots are a closed set: a slot name is a file name, and an open one would
// be a path somebody else chose.
func TestSlots(t *testing.T) {
	for _, s := range []string{"sram", "auto", "state-0", "state-9"} {
		if !ValidSlot(s) {
			t.Errorf("%s refused", s)
		}
	}
	for _, s := range []string{"", "state-10", "../sram", "SRAM", "state-1/../../x", "sram.prev"} {
		if ValidSlot(s) {
			t.Errorf("%q accepted", s)
		}
	}
	f := Files{Dir: t.TempDir()}
	if _, err := f.Write("u_1", 7, "../evil", bytes.NewReader(nil)); err == nil {
		t.Error("an invalid slot was written")
	}
}

// People's saves are apart, and removing one person removes only theirs.
func TestUsersAreApart(t *testing.T) {
	f := Files{Dir: t.TempDir()}
	f.Write("u_1", 7, "sram", strings.NewReader("mine"))
	f.Write("u_2", 7, "sram", strings.NewReader("yours"))
	if err := f.RemoveUser("u_2"); err != nil {
		t.Fatal(err)
	}
	if read(t, f, "sram", false) != "mine" {
		t.Error("removing one user touched another's save")
	}
	if _, err := f.Open("u_2", 7, "sram", false); !errors.Is(err, os.ErrNotExist) {
		t.Error("a removed user's save survived")
	}
	// A user id is never a path, whatever it contains.
	if _, err := f.Write("../../etc", 7, "sram", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	if rel, _ := filepath.Rel(f.Dir, f.userDir("../../etc")); strings.HasPrefix(rel, "..") {
		t.Errorf("a user id escaped the save directory: %s", rel)
	}
}
