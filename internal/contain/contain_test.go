package contain

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// symlink makes a link or skips: on Windows creating one needs Developer Mode
// or elevation, and a machine without either cannot run these, which is not
// the code failing. Linux CI always can.
func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
}

func write(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// layout is a library folder and a folder beside it holding a secret.
func layout(t *testing.T) (lib, secret string) {
	t.Helper()
	base := t.TempDir()
	lib = filepath.Join(base, "lib")
	secret = filepath.Join(base, "outside", "secret.mkv")
	write(t, filepath.Join(lib, "Films", "real.mkv"))
	write(t, secret)
	return lib, secret
}

// The finding itself: a file inside by name, outside on disk.
func TestWithinRefusesALinkOutOfTheRoot(t *testing.T) {
	lib, secret := layout(t)
	link := filepath.Join(lib, "Films", "film.mkv")
	symlink(t, secret, link)

	if _, err := Within(lib, link); !errors.Is(err, ErrOutside) {
		t.Fatalf("Within = %v, want ErrOutside — a link out of the library passed", err)
	}
}

// A link through a *folder* is the same escape one level up.
func TestWithinRefusesALinkedFolderOutOfTheRoot(t *testing.T) {
	lib, secret := layout(t)
	symlink(t, filepath.Dir(secret), filepath.Join(lib, "Linked"))

	if _, err := Within(lib, filepath.Join(lib, "Linked", "secret.mkv")); !errors.Is(err, ErrOutside) {
		t.Fatalf("Within = %v, want ErrOutside", err)
	}
}

// Positive control: a link to another file in the library is fine, and the
// path handed back is the one asked about, not where it points.
func TestWithinAcceptsALinkInsideTheRoot(t *testing.T) {
	lib, _ := layout(t)
	link := filepath.Join(lib, "Collection", "same film.mkv")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	symlink(t, filepath.Join(lib, "Films", "real.mkv"), link)

	got, err := Within(lib, link)
	if err != nil {
		t.Fatalf("Within = %v, want nil — a link inside the library was refused", err)
	}
	if got != link {
		t.Errorf("Within returned %q, want the path asked about %q", got, link)
	}
}

// A library location that is itself a link (or a mapped path) resolves the
// same way on both sides and still contains its own files.
func TestWithinAcceptsARootThatIsALink(t *testing.T) {
	lib, _ := layout(t)
	alias := filepath.Join(t.TempDir(), "alias")
	symlink(t, lib, alias)

	if _, err := Within(alias, filepath.Join(alias, "Films", "real.mkv")); err != nil {
		t.Fatalf("Within = %v, want nil — a linked root lost its own files", err)
	}
}

// A file that does not exist yet is judged by the folder it will be made in:
// the optimized copy about to be written, a drive that went to sleep.
func TestWithinAcceptsAMissingFile(t *testing.T) {
	lib, _ := layout(t)
	if _, err := Within(lib, filepath.Join(lib, "Films", "not yet", "copy.mkv")); err != nil {
		t.Fatalf("Within = %v, want nil", err)
	}
	if _, err := Within(lib, filepath.Join(lib, "..", "elsewhere.mkv")); !errors.Is(err, ErrOutside) {
		t.Fatalf("Within = %v, want ErrOutside for a lexical escape", err)
	}
}

// Writing through a dangling link creates a file wherever it points.
func TestWithinRefusesADanglingLink(t *testing.T) {
	lib, _ := layout(t)
	link := filepath.Join(lib, "Films", "gone.mkv")
	symlink(t, filepath.Join(t.TempDir(), "nothing-here"), link)

	if _, err := Within(lib, link); !errors.Is(err, ErrDanglingLink) {
		t.Fatalf("Within = %v, want ErrDanglingLink", err)
	}
}

// Removing a link removes the link, so a stray link out of the library is
// still deletable — but not through a linked folder that leaves it.
func TestEntryWithinJudgesTheFolderNotTheLink(t *testing.T) {
	lib, secret := layout(t)
	link := filepath.Join(lib, "Films", "film.mkv")
	symlink(t, secret, link)

	if _, err := EntryWithin(lib, link); err != nil {
		t.Fatalf("EntryWithin = %v, want nil — the link itself is inside", err)
	}

	symlink(t, filepath.Dir(secret), filepath.Join(lib, "Linked"))
	if _, err := EntryWithin(lib, filepath.Join(lib, "Linked", "secret.mkv")); !errors.Is(err, ErrOutside) {
		t.Fatalf("EntryWithin = %v, want ErrOutside — the unlink would happen outside", err)
	}
	if _, err := EntryWithin(lib, lib); !errors.Is(err, ErrOutside) {
		t.Fatalf("EntryWithin(root, root) = %v, want ErrOutside", err)
	}
}

// A loop of links ends in an error, not in a stack overflow.
func TestWithinRefusesALinkLoop(t *testing.T) {
	lib, _ := layout(t)
	a, b := filepath.Join(lib, "a.mkv"), filepath.Join(lib, "b.mkv")
	symlink(t, b, a)
	symlink(t, a, b)

	if _, err := Within(lib, a); err == nil {
		t.Fatal("Within accepted a loop of links")
	}
}
