//go:build windows

package api

import (
	"os"
	"syscall"
)

// isHidden reports whether a directory carries the Windows hidden or system
// attribute.
//
// A dotfile check alone is not enough here: $RECYCLE.BIN, System Volume
// Information, and Config.Msi have ordinary names and would otherwise be
// offered as media library candidates.
func isHidden(_ string, info os.FileInfo) bool {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return false
	}
	const mask = syscall.FILE_ATTRIBUTE_HIDDEN | syscall.FILE_ATTRIBUTE_SYSTEM
	return data.FileAttributes&mask != 0
}
