//go:build !windows

package games

// Epic and Battle.net are found on Windows only, for the reason locate_other.go
// gives for Steam: the desktop client that owns this package is Windows-only.
// These exist so the package builds and its parsers run under
// `GOOS=linux go vet ./...` and on CI, which builds on Linux.
//
// The parsing is deliberately on the other side of this line — ParseEpicManifest
// and BlizzardGames take bytes and structs, so every rule about what counts as
// an installed game is tested on every platform.
func epicManifestDir() (string, bool) { return "", false }

func installedPrograms() []InstalledProgram { return nil }
