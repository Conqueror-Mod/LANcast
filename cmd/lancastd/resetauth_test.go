package main

import (
	"os"
	"path/filepath"
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

func TestSameDir(t *testing.T) {
	same := [][2]string{
		{`C:\ProgramData\LANcast`, `C:\ProgramData\LANcast`},
		{`C:\ProgramData\LANcast`, `C:\programdata\lancast`},
		{`C:\ProgramData\LANcast\`, `C:\ProgramData\LANcast`},
		{`C:\ProgramData\.\LANcast`, `C:\ProgramData\LANcast`},
	}
	for _, p := range same {
		if !sameDir(p[0], p[1]) {
			t.Errorf("sameDir(%q, %q) = false, want true", p[0], p[1])
		}
	}
	if sameDir(`C:\ProgramData\LANcast`, `C:\Users\Chris\AppData\Roaming\LANcast`) {
		t.Error("sameDir treated two different data directories as one")
	}
}
