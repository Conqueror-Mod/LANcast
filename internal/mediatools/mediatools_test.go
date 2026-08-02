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
