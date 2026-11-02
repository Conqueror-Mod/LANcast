package cheevos

import (
	"os"
	"path/filepath"
	"testing"
)

// The DLL is looked for beside the executable, by full path, and by the
// name the release ships (.goreleaser.yaml).
func TestDLLPathIsBesideTheExecutable(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	got := DLLPath()
	if !filepath.IsAbs(got) || filepath.Dir(got) != filepath.Dir(exe) || filepath.Base(got) != "lancast-cheevos.dll" {
		t.Fatalf("DLLPath() = %q, want lancast-cheevos.dll beside %s", got, exe)
	}
}
