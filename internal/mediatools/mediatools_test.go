package mediatools

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeToolDir creates a directory containing a stand-in ffprobe executable.
func fakeToolDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, exeName("ffprobe")), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestEnsurePutsToolsOnPath(t *testing.T) {
	orig := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", orig) })

	dir := fakeToolDir(t)
	if !Ensure(dir) {
		t.Fatal("Ensure did not accept a directory containing ffprobe")
	}
	if got := os.Getenv("PATH"); len(got) <= len(orig) || got[:len(dir)] != dir {
		t.Errorf("PATH does not start with %q", dir)
	}
}

// A configured-but-wrong directory must not be prepended: it would shadow a PATH
// that already works, turning a good setup into a broken one.
func TestEnsureIgnoresDirectoriesWithoutTools(t *testing.T) {
	orig := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", orig) })

	if Ensure(t.TempDir()) {
		t.Error("Ensure accepted a directory with no ffprobe in it")
	}
	if Ensure("") {
		t.Error("Ensure accepted an empty directory")
	}
	if os.Getenv("PATH") != orig {
		t.Error("PATH was modified despite the directory being rejected")
	}
}

// Resolve prefers what was configured — the whole point of recording it, since a
// service cannot search the user's PATH.
func TestResolvePrefersConfiguredDir(t *testing.T) {
	orig := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", orig) })

	dir := fakeToolDir(t)
	got, ok := Resolve(dir)
	if !ok || got != dir {
		t.Errorf("Resolve(%q) = (%q, %v), want the configured dir", dir, got, ok)
	}
}

func TestDescribeIsHonest(t *testing.T) {
	if got := Describe("", false); got != "not found" {
		t.Errorf("Describe(not ok) = %q", got)
	}
	if got := Describe("", true); got != "on PATH" {
		t.Errorf("Describe(no dir) = %q", got)
	}
	if got := Describe("/opt/ffmpeg/bin", true); got != "/opt/ffmpeg/bin" {
		t.Errorf("Describe(dir) = %q", got)
	}
}

/*
 * The lookup that removes the reason this package exists.
 *
 * A directory found relative to os.Executable() needs no PATH, so a service
 * running as LocalSystem resolves it exactly as a desktop process does — which
 * is the whole failure the package header describes.
 *
 * Written beside the test binary rather than mocked, because the thing worth
 * asserting is that os.Executable() is consulted at all. Skipped rather than
 * failed if that directory is not writable: a read-only build dir is a property
 * of the machine, not a bug in the lookup.
 */
func TestDetectPrefersToolsBesideTheExecutable(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skip("no executable path on this platform")
	}
	beside := filepath.Join(filepath.Dir(exe), exeName("ffprobe"))
	if err := os.WriteFile(beside, []byte("x"), 0o755); err != nil {
		t.Skip("cannot write beside the test binary:", err)
	}
	t.Cleanup(func() { os.Remove(beside) })

	got := Detect()
	if got != filepath.Dir(exe) {
		// If this fails with a real ffmpeg directory, ours-first has regressed:
		// a copy we installed on purpose lost to whatever was on PATH.
		t.Errorf("Detect() = %q, want the executable's own directory %q", got, filepath.Dir(exe))
	}
}

// The managed install drops the tools in a `tools` subdirectory rather than
// scattering them beside the binaries the installer wrote, so that path has to
// be searched too.
func TestSelfDirsIncludesAToolsSubdirectory(t *testing.T) {
	dirs := selfDirs()
	if len(dirs) != 2 {
		t.Fatalf("selfDirs() = %v, want the executable dir and its tools child", dirs)
	}
	if filepath.Base(dirs[1]) != "tools" {
		t.Errorf("second candidate = %q, want a tools directory", dirs[1])
	}
	if filepath.Dir(dirs[1]) != dirs[0] {
		t.Errorf("tools dir %q is not inside %q", dirs[1], dirs[0])
	}
}
