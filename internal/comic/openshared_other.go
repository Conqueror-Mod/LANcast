//go:build !windows

package comic

import "os"

// openShared is os.Open: elsewhere an open file never stops its owner deleting
// or replacing it.
func openShared(path string) (*os.File, error) { return os.Open(path) }
