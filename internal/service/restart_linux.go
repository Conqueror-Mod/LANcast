//go:build linux

package service

import "time"

// Restart drives systemd's own restart rather than a stop-then-start pair.
//
// systemd already sequences that correctly, including the wait this has to do
// by hand on Windows, so reproducing it here would be a worse copy of something
// the platform does properly.
func (systemdManager) Restart(time.Duration) error { return systemctl("restart", Name) }
