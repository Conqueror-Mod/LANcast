package main

import (
	"path/filepath"
	"runtime"
)

/*
 * runtimePath decides which ONNX Runtime to load, and is pure so the decision
 * can be tested without a runtime on disk — the same split probing keeps
 * between `probe.ParseJSON` and ffmpeg, for the same reason.
 *
 * An empty answer is the dangerous one, which is why there is a function here
 * at all. It makes onnxruntime_go ask the loader for a bare filename, and on
 * Windows 11 that always succeeds: the OS ships its own Windows ML copy in
 * System32 at 1.17, this build asks for API 29, and the worker fails claiming
 * *our* verified download is unusable. Empty is returned only when there is
 * genuinely nothing to name.
 */
func runtimePath(env, modelsDir string) string {
	if env != "" {
		return env
	}
	if modelsDir == "" {
		return ""
	}
	return filepath.Join(modelsDir, onnxLibName())
}

/*
 * onnxLibName is what the ONNX Runtime shared library is called here.
 *
 * Deliberately not behind the cgo tag. The no-cgo build has no use for it, and
 * a name that exists in only one of two builds is a name that drifts — this one
 * has to agree with `onnxLibName` in cmd/lancastd, which decides where the
 * optional download is placed, and with `faceinstall`, which places it. Three
 * spellings of one filename is a fault nobody finds by reading any single file.
 */
func onnxLibName() string {
	switch runtime.GOOS {
	case "windows":
		return "onnxruntime.dll"
	case "darwin":
		return "libonnxruntime.dylib"
	default:
		return "libonnxruntime.so"
	}
}
