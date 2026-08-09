package selfupdate

import (
	"os"
	"path/filepath"
	"testing"
)

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestStageThenApplyReplacesTheInstall(t *testing.T) {
	data, install := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(install, "LANcast-Server.exe"), []byte("old server"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Stage(data, "v0.7.0", map[string][]byte{
		"LANcast-Server.exe": []byte("new server"),
		"LANcast-Client.exe": []byte("new client"),
	}, 1000); err != nil {
		t.Fatal(err)
	}

	m, ok := Pending(data)
	if !ok || m.Version != "v0.7.0" {
		t.Fatalf("Pending = %+v, %v", m, ok)
	}

	if _, err := Apply(data, install); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if got := read(t, filepath.Join(install, "LANcast-Server.exe")); got != "new server" {
		t.Errorf("server = %q, want the new one", got)
	}
	if got := read(t, filepath.Join(install, "LANcast-Client.exe")); got != "new client" {
		t.Errorf("client = %q, want the new one", got)
	}
	// The replaced file is moved aside rather than deleted, because it may be
	// the running process's own image.
	if got := read(t, filepath.Join(install, "LANcast-Server.exe.old")); got != "old server" {
		t.Errorf("the previous server was not preserved as .old, got %q", got)
	}
	if _, ok := Pending(data); ok {
		t.Error("staging survived a successful apply")
	}
}

// The failure that must never happen: an install left without its executable.
// A move that fails partway puts everything back.
func TestApplyRollsBackOnFailure(t *testing.T) {
	data, install := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(install, "a.exe"), []byte("old a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(install, "b.exe"), []byte("old b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Stage(data, "v0.7.0", map[string][]byte{
		"a.exe": []byte("new a"),
		"b.exe": []byte("new b"),
	}, 1000); err != nil {
		t.Fatal(err)
	}

	// Delete one staged file behind Apply's back, so the pre-flight check fires
	// before anything has been moved.
	if err := os.Remove(filepath.Join(Dir(data), "b.exe")); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(data, install); err == nil {
		t.Fatal("Apply succeeded with a staged file missing")
	}

	if got := read(t, filepath.Join(install, "a.exe")); got != "old a" {
		t.Errorf("a.exe = %q; the install was modified despite the failure", got)
	}
	if got := read(t, filepath.Join(install, "b.exe")); got != "old b" {
		t.Errorf("b.exe = %q; the install was modified despite the failure", got)
	}
}

// An interrupted download leaves files with no manifest, which must read as
// nothing to do rather than as a half-update to install.
func TestIncompleteStagingIsNotPending(t *testing.T) {
	data := t.TempDir()
	if err := os.MkdirAll(Dir(data), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(Dir(data), "LANcast-Server.exe"), []byte("half"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := Pending(data); ok {
		t.Error("a staging directory with no manifest was reported as pending")
	}
}

// A staged name that escapes the staging directory would be moved anywhere the
// service can write, which is everywhere.
func TestStageRefusesPathsThatEscape(t *testing.T) {
	data := t.TempDir()
	for _, bad := range []string{
		"../evil.exe",
		`..\evil.exe`,
		"sub/evil.exe",
		"",
		".",
	} {
		if err := Stage(data, "v1.0.0", map[string][]byte{bad: []byte("x")}, 1); err == nil {
			t.Errorf("Stage accepted the path %q", bad)
		}
	}
}

// Staging twice keeps only the newer one. A stale file from an abandoned update
// must not ride along with the next.
func TestStagingReplacesAnEarlierOne(t *testing.T) {
	data := t.TempDir()
	if err := Stage(data, "v0.7.0", map[string][]byte{"a.exe": []byte("a")}, 1); err != nil {
		t.Fatal(err)
	}
	if err := Stage(data, "v0.8.0", map[string][]byte{"b.exe": []byte("b")}, 2); err != nil {
		t.Fatal(err)
	}
	m, _ := Pending(data)
	if m.Version != "v0.8.0" || len(m.Files) != 1 || m.Files[0] != "b.exe" {
		t.Errorf("manifest = %+v, want only the newer staging", m)
	}
	if _, err := os.Stat(filepath.Join(Dir(data), "a.exe")); err == nil {
		t.Error("a file from the abandoned staging survived")
	}
}

func TestCleanupOldRemovesPreviousImages(t *testing.T) {
	install := t.TempDir()
	for _, n := range []string{"LANcast-Server.exe.old", "LANcast-Client.exe.old"} {
		if err := os.WriteFile(filepath.Join(install, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(install, "LANcast-Server.exe"), []byte("live"), 0o755); err != nil {
		t.Fatal(err)
	}

	if n := CleanupOld(install); n != 2 {
		t.Errorf("removed %d, want 2", n)
	}
	if _, err := os.Stat(filepath.Join(install, "LANcast-Server.exe")); err != nil {
		t.Error("CleanupOld removed a live executable")
	}
}

// Applying with nothing staged is not an error worth propagating; it is the
// normal case on every start.
func TestApplyWithNothingStaged(t *testing.T) {
	if _, err := Apply(t.TempDir(), t.TempDir()); !os.IsNotExist(err) {
		t.Errorf("Apply with nothing staged = %v, want ErrNotExist", err)
	}
}
