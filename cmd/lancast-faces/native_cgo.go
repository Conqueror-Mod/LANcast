//go:build cgo

package main

/*
 * The cgo path, and the reason there is a C function here at all.
 *
 * This binary exists to carry native inference (ADR 0052), and the thing that
 * has to be proven first is that a cgo binary can be cross-compiled and
 * published by a Linux runner for every platform LANcast ships to. A Go file
 * that merely *could* use cgo proves nothing: without a real C symbol the
 * toolchain is never invoked, the mingw cross-compiler is never exercised, and
 * the first genuine native dependency discovers all of that during a release.
 *
 * So there is one real C call. It reports the compiler the artefact was built
 * with, which is also the one fact a cross-compiled binary cannot fake — a
 * build that silently lost its cgo says so in `capabilities` rather than at the
 * first photograph.
 */

// #include <stdlib.h>
//
// static const char* lancast_native_id(void) {
// #if defined(__clang__)
//   return "clang " __clang_version__;
// #elif defined(__GNUC__)
//   return "gcc " __VERSION__;
// #else
//   return "unknown C toolchain";
// #endif
// }
import "C"

const hasCGO = true

func nativeInfo() string {
	return C.GoString(C.lancast_native_id())
}

/*
 * detectOne is where the model will run.
 *
 * Unreachable today — `detect` refuses before it gets here, because caps()
 * reports not-ready while no model is bundled. It is written as the seam rather
 * than left absent so that adding ONNX Runtime is a change to one function with
 * a settled contract on both sides of it.
 */
func detectOne(path, modelsDir string) ([]Face, error) {
	return nil, errNoModel
}
