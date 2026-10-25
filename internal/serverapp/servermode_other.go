//go:build !windows

package serverapp

import "errors"

// runServerMode is Windows-only: elsewhere the service is a unit the operator
// wrote, in a folder they chose, and LANcast does not move it (ADR 0092).
func runServerMode([]string) error {
	return errors.New("server-mode: switching how the server runs is only offered on Windows")
}
