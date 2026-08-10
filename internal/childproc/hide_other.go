//go:build !windows

// Package childproc adjusts how subprocesses are launched.
//
// Only Windows needs anything: it is the platform that gives a console program
// a visible window when the parent has no console of its own.
package childproc

import (
	"os/exec"
	"syscall"
)

// Hide is a no-op off Windows, where starting a child process never creates a
// window.
func Hide(cmd *exec.Cmd) {}

// Detach starts a child in its own process group so it survives its parent.
func Detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}
