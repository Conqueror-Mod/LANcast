//go:build !windows

package dataperm

// Protect does nothing off Windows. A systemd service's data directory is
// /var/lib/lancast, created by the package with the service account's mode;
// the Windows default this repairs — ProgramData's grant letting every user
// add files — has no counterpart there.
func Protect(string) (Report, error) { return Report{}, nil }

// ShareCert does nothing off Windows, for the same reason.
func ShareCert(string) error { return nil }
