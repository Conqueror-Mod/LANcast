//go:build !windows

package selfupdate

// inheritFromParent does nothing off Windows: a rename keeps a file's mode
// there too, but the staged files are written with the archive's modes, not
// the data directory's, and no unprivileged app runs them.
func inheritFromParent(string) error { return nil }

func dacl(string) (string, error) { return "", nil }
