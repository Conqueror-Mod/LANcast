package diskuse

import "golang.org/x/sys/windows"

// diskSpace is the free and total bytes of the drive holding dir, as the
// account running LANcast sees it.
func diskSpace(dir string) (free, total uint64) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, 0
	}
	var totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, &total, &totalFree); err != nil {
		return 0, 0
	}
	return free, total
}
