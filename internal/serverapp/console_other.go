//go:build !windows

package serverapp

// attachConsole is a no-op everywhere but Windows: other platforms never
// detach a process from the terminal that launched it, so standard output
// already goes where the caller expects.
func attachConsole() {}
