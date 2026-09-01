package main

import (
	"path/filepath"
	"runtime"
	"testing"
)

/*
 * Which ONNX Runtime the worker loads.
 *
 * The reported fault was that face grouping failed with the models correctly
 * downloaded and verified beside it. The worker had bound to
 * `C:\Windows\System32\onnxruntime.dll` — the copy Windows 11 ships for
 * Windows ML, at 1.17 against a build asking for API 29 — because the library
 * path was left empty and the loader was allowed to answer.
 *
 * Pure on purpose: this asserts the decision, not a load, so it runs on any
 * platform with no runtime present. Same split `probe.ParseJSON` keeps from
 * ffmpeg.
 */

// The fault. A models directory must always produce a path, because that is
// where the download puts the runtime.
func TestTheModelsDirectoryNamesTheRuntime(t *testing.T) {
	got := runtimePath("", filepath.Join("C:", "data", "faces"))
	want := filepath.Join("C:", "data", "faces", onnxLibName())
	if got != want {
		t.Errorf("runtimePath = %q, want %q", got, want)
	}
	if got == "" {
		t.Fatal("empty: the loader would choose, and on Windows it chooses the OS copy")
	}
}

// The override still wins, because a development runtime lives wherever it was
// unpacked and is the whole reason the variable exists.
func TestTheEnvironmentOverrideWins(t *testing.T) {
	custom := filepath.Join("D:", "build", "onnxruntime", "lib", onnxLibName())
	if got := runtimePath(custom, filepath.Join("C:", "data", "faces")); got != custom {
		t.Errorf("runtimePath = %q, want the override %q", got, custom)
	}
}

/*
 * Empty only when there is genuinely nothing to name.
 *
 * Not a fallback worth having, but the alternative — inventing a relative path
 * — would be worse: it would resolve against whatever the working directory
 * happens to be, which is the same class of fault as letting the loader decide.
 */
func TestNothingToNameStaysEmpty(t *testing.T) {
	if got := runtimePath("", ""); got != "" {
		t.Errorf("runtimePath = %q, want empty", got)
	}
}

// The filename has to agree with cmd/lancastd (which decides where the download
// goes) and internal/faceinstall (which puts it there). They cannot import each
// other, so the agreement is asserted rather than assumed.
func TestTheLibraryNameMatchesThePlatform(t *testing.T) {
	got := onnxLibName()
	want := map[string]string{
		"windows": "onnxruntime.dll",
		"darwin":  "libonnxruntime.dylib",
	}[runtime.GOOS]
	if want == "" {
		want = "libonnxruntime.so"
	}
	if got != want {
		t.Errorf("onnxLibName = %q, want %q on %s", got, want, runtime.GOOS)
	}
}
