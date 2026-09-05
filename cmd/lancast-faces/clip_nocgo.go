//go:build !cgo

package main

/*
 * The no-cgo build's half of semantic search.
 *
 * It exists for the same reason native_nocgo.go does: `go build ./...` and
 * `GOOS=linux go vet ./...` have to keep working from a Windows desktop with no
 * C toolchain, and cgo is off by default when cross-compiling. A package that
 * only compiled with a toolchain would turn the cross-platform check this
 * project runs before pushing into a failure nobody could act on.
 *
 * A binary built this way is honest about being useless rather than quietly
 * returning empty vectors — which would read as "nothing in your library
 * matches anything you search for".
 *
 * ClipModelName is defined here too, and deliberately with the same value: it
 * names the coordinate system a stored vector belongs to, the server compares
 * it against what is in the database, and a build that reported a different one
 * would make every existing embedding look stale and re-run a pass over the
 * whole library to no purpose.
 */

const ClipModelName = "openclip-vit-b-32"

func embedImageFile(path, modelsDir string) ([]float32, error) { return nil, errNoClipModel }

func embedQuery(query, modelsDir string) ([]float32, error) { return nil, errNoClipModel }

func clipModels(dir string) error { return errNoClipModel }
