//go:build !windows

package api

import "os"

// isHidden has nothing to add on Unix: the dotfile convention already covers
// it, and the caller checks that before asking.
func isHidden(_ string, _ os.FileInfo) bool { return false }
