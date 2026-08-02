package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withDB makes a directory holding a lancast.db of the given size.
func withDB(t *testing.T, size int) string {
	t.Helper()
	dir := t.TempDir()
	body := make([]byte, size)
	if err := os.WriteFile(filepath.Join(dir, "lancast.db"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The trap: a service keeps its data somewhere else, so a human running this
// resolves to the per-user default, finds no accounts, and reads that as the
// command being broken. Naming the other database is what turns that into an
// answer.
func TestExistingDatabasesFindsTheOtherOne(t *testing.T) {
	inUse := withDB(t, 4096)
	other := withDB(t, 4096)

	got := existingDatabases(inUse, []string{other})
	if len(got) != 1 || !sameDir(got[0], other) {
		t.Fatalf("existingDatabases = %v, want [%s]", got, other)
	}
}

// The directory being reset is not an alternative to itself.
func TestExistingDatabasesExcludesTheOneInUse(t *testing.T) {
	inUse := withDB(t, 4096)

	if got := existingDatabases(inUse, []string{inUse}); len(got) != 0 {
		t.Errorf("existingDatabases = %v, want empty — that is the database being reset", got)
	}
	// Same directory spelled differently is still the same directory.
	noisy := filepath.Join(inUse, ".", "")
	if got := existingDatabases(inUse, []string{noisy}); len(got) != 0 {
		t.Errorf("existingDatabases = %v for an uncleaned path to the same dir, want empty", got)
	}
}

func TestExistingDatabasesSkipsDirsWithoutOne(t *testing.T) {
	inUse := withDB(t, 4096)
	empty := t.TempDir() // no lancast.db at all
	stub := withDB(t, 0) // a zero-byte file is not a database
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	if got := existingDatabases(inUse, []string{empty, stub, missing, ""}); len(got) != 0 {
		t.Errorf("existingDatabases = %v, want empty", got)
	}
}

// Two candidates resolving to the same place must be reported once.
func TestExistingDatabasesDeduplicates(t *testing.T) {
	inUse := t.TempDir()
	other := withDB(t, 4096)

	got := existingDatabases(inUse, []string{other, other, filepath.Join(other, ".")})
	if len(got) != 1 {
		t.Errorf("existingDatabases = %v, want one entry", got)
	}
}

// Paths are built with filepath so this exercises the host's real separator
// semantics. Hardcoding Windows spellings passed on Windows and failed on
// Linux, where a backslash is an ordinary character and Clean leaves the "."
// element in place — the test asserted the platform rather than the behaviour.
func TestSameDir(t *testing.T) {
	base := filepath.Join("C:", "ProgramData", "LANcast")
	sep := string(filepath.Separator)

	same := [][2]string{
		{base, base},
		// Case-insensitive: Windows paths differing only in case are one
		// directory, and naming it as an alternative would be noise.
		{base, strings.ToLower(base)},
		{base + sep, base},
		{filepath.Join("C:", "ProgramData", ".", "LANcast"), base},
	}
	for _, p := range same {
		if !sameDir(p[0], p[1]) {
			t.Errorf("sameDir(%q, %q) = false, want true", p[0], p[1])
		}
	}

	other := filepath.Join("C:", "Users", "Chris", "AppData", "Roaming", "LANcast")
	if sameDir(base, other) {
		t.Error("sameDir treated two different data directories as one")
	}
}
