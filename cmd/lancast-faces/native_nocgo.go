//go:build !cgo

package main

/*
 * The build without cgo.
 *
 * It exists so `go build ./...` and `go vet ./...` keep working everywhere,
 * including the GOOS=linux vet run from a Windows desktop that this project
 * does before pushing anything platform-specific — cgo is off by default when
 * cross-compiling, and a package that only compiled with a C toolchain would
 * turn that check into a failure nobody could act on.
 *
 * A binary built this way is honest about being useless: capabilities reports
 * not-ready with the reason, and detect refuses.
 */

const hasCGO = false

func nativeInfo() string { return "none (built without cgo)" }

func detectOne(path, modelsDir string) ([]Face, error) {
	return nil, errNoModel
}

func probeModels(dir string) error {
	return errNoModel
}
