//go:build !windows

package sharedopen

import "os"

// Open is os.Open: elsewhere an open file never stops its owner deleting or
// replacing it.
func Open(path string) (*os.File, error) { return os.Open(path) }
