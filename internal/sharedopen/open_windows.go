//go:build windows

package sharedopen

import (
	"os"
	"syscall"
)

// Open opens a file for reading that its owner can still delete or replace.
// os.Open on Windows leaves out FILE_SHARE_DELETE, so a film held open by a
// stalled stream, or a .cb7 on the comic shelf, would make Explorer refuse to
// delete or overwrite it ("the file is in use") for as long as the handle
// lives, and an in-app delete would fail with it.
//
// A directory is refused here (no FILE_FLAG_BACKUP_SEMANTICS), which every
// caller already treats as "not a file".
func Open(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}
