//go:build !windows && !linux

package history

// systemTimes cannot be read here; the dashboard says so rather than guess.
func systemTimes() (idle, total uint64, ok bool) { return 0, 0, false }
