//go:build !windows

// Package childproc adjusts how subprocesses are launched.
//
// Only Windows needs anything: it is the platform that gives a console program
// a visible window when the parent has no console of its own.
package childproc

import "os/exec"

// Hide is a no-op off Windows, where starting a child process never creates a
// window.
func Hide(cmd *exec.Cmd) {}
