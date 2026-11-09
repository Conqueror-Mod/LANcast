//go:build !windows

package diskuse

import "golang.org/x/sys/unix"

// diskSpace is the free and total bytes of the filesystem holding dir, as an
// unprivileged process may use it.
func diskSpace(dir string) (free, total uint64) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, 0
	}
	bs := uint64(st.Bsize) //nolint:unconvert // int64 on linux, uint32 on darwin
	return st.Bavail * bs, st.Blocks * bs
}
