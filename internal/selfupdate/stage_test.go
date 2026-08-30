package selfupdate

import (
	"os"
	"path/filepath"
	"strings"
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
	// the running process's own image. The name is stamped rather than fixed —
	// see backupOf and the test below for why — so it is found by pattern.
	if got := read(t, backupOf(t, install, "LANcast-Server.exe")); got != "old server" {
		t.Errorf("the previous server was not preserved, got %q", got)
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

// backupOf finds the file Apply moved aside for name, whatever it stamped it.
func backupOf(t *testing.T, dir, name string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), name+".old") {
			return filepath.Join(dir, e.Name())
		}
	}
	t.Fatalf("no backup of %s in %v", name, entries)
	return ""
}

/*
 * Two updates in a row, with the first backup impossible to remove.
 *
 * This is the reported failure, reduced. The server tray is resident and runs
 * out of the install directory, so after one update it holds the renamed image
 * open for as long as it lives. With a fixed `.old` the next update had to
 * *replace* a mapped file, and Windows refuses:
 *
 *	rename LANcast-Server.exe LANcast-Server.exe.old: Access is denied.
 *
 * Measured on the reporting install: v0.8.31 and v0.8.32 applied cleanly and
 * v0.8.33 failed with exactly that. The first update after the tray starts
 * works and every one after it fails — permanently, and looking like a one-off.
 *
 * A file that cannot be removed is the thing to simulate, and an open handle is
 * not portable; a *directory* where the backup would go is refused by Remove
 * and by Rename on every platform, which is the same obstruction for this
 * purpose.
 */
func TestASecondUpdateSurvivesAnImmovableBackup(t *testing.T) {
	data, install := t.TempDir(), t.TempDir()
	exe := filepath.Join(install, "LANcast-Server.exe")
	if err := os.WriteFile(exe, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Something that cannot be removed or replaced, sitting exactly where a
	// fixed backup name would want to go.
	if err := os.Mkdir(exe+".old", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(exe+".old", "held"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Stage(data, "v2", map[string][]byte{"LANcast-Server.exe": []byte("v2")}, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(data, install); err != nil {
		t.Fatalf("Apply refused to update past an immovable backup: %v", err)
	}
	if got := read(t, exe); got != "v2" {
		t.Errorf("server = %q, want v2 — the update did not take", got)
	}
}

// And the sweep takes both spellings, or the file whose immovability caused the
// stamp would be left behind for ever by the version that fixed it.
func TestCleanupTakesOldAndStampedBackups(t *testing.T) {
	install := t.TempDir()
	for _, n := range []string{"a.exe.old", "b.exe.old.k3j4h", "keep.exe"} {
		if err := os.WriteFile(filepath.Join(install, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := CleanupOld(install); got != 2 {
		t.Errorf("removed %d, want 2", got)
	}
	if _, err := os.Stat(filepath.Join(install, "keep.exe")); err != nil {
		t.Error("cleanup removed something that was not a backup")
	}
}

/*
 * An incomplete staging is discarded rather than retried for ever.
 *
 * This is the state the reporting install was left in: a manifest naming three
 * files with only one of them beside it, because an earlier Apply consumed two
 * and then failed. Nothing can satisfy that manifest, so every restart read it,
 * refused, and changed nothing — the install stayed on its old version while
 * the log repeated one line.
 *
 * Throwing it away costs a re-download and hands the next check back the
 * ability to do its job.
 */
func TestAnUnsatisfiableStagingIsDiscarded(t *testing.T) {
	data, install := t.TempDir(), t.TempDir()
	if err := Stage(data, "v2", map[string][]byte{
		"a.exe": []byte("a"),
		"b.exe": []byte("b"),
	}, 1); err != nil {
		t.Fatal(err)
	}
	// One of them vanishes, exactly as a half-finished Apply leaves it.
	if err := os.Remove(filepath.Join(Dir(data), "b.exe")); err != nil {
		t.Fatal(err)
	}

	if _, err := Apply(data, install); err == nil {
		t.Fatal("Apply accepted a staging it could not satisfy")
	}
	if _, ok := Pending(data); ok {
		t.Error("the unsatisfiable staging survived, so every later start " +
			"repeats the same refusal and the install never updates")
	}
}
