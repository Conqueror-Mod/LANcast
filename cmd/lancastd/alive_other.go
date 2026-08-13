//go:build !windows

package main

import (
	"os"
	"syscall"
)

// processAlive reports whether a pid still names a running process.
//
// Signal 0 is the portable liveness check: it performs the permission and
// existence checks and delivers nothing. A permission error means the process
// exists and belongs to somebody else, which is still "alive" — the answer this
// caller needs.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || err == syscall.EPERM
}
