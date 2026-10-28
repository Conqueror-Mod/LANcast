//go:build windows

package history

import (
	"syscall"
	"unsafe"
)

var getSystemTimes = syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemTimes")

// systemTimes is the machine's idle and total time, in 100 ns ticks. Kernel
// time includes idle time on Windows, so total is kernel plus user.
func systemTimes() (idle, total uint64, ok bool) {
	var i, k, u syscall.Filetime
	r, _, _ := getSystemTimes.Call(
		uintptr(unsafe.Pointer(&i)), uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&u)))
	if r == 0 {
		return 0, 0, false
	}
	ft := func(f syscall.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }
	return ft(i), ft(k) + ft(u), true
}
