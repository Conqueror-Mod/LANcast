//go:build !windows

package games

// steamRoot finds nothing off Windows.
//
// The desktop client that owns this package is Windows-only, and the games tab
// only ever appears where its bindings do. This file exists so the package
// still builds and its parsers still run under `GOOS=linux go vet ./...` and on
// CI, which builds on Linux — the rules are pure and worth testing everywhere,
// the registry is not.
func steamRoot() (string, bool) { return "", false }
