//go:build windows

package comic

import (
	"os"
	"syscall"
)

// openShared opens a file for reading that its owner can still delete or
// replace. os.Open on Windows leaves out FILE_SHARE_DELETE, so a .cb7 held
// open on the shelf for five minutes would make Explorer refuse to delete or
// overwrite it ("the file is in use"), which a reader holding a file
// open must not do to its owner.
func openShared(path string) (*os.File, error) {
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
