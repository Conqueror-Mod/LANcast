//go:build windows

package childproc

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW: run a console program without giving it a
// console. Not exported by the syscall package.
const createNoWindow = 0x08000000

// Hide stops a child process from opening a console window.
//
// LANcast's Windows executables are linked -H=windowsgui and have no console of
// their own (ADR 0022). Starting a console program — ffmpeg, ffprobe — from a
// parent with no console makes Windows allocate a *visible* one for the child,
// which appears as a command window flashing on screen.
//
// That is not cosmetic at the rate it happens: hardware-encoder detection runs
// `ffmpeg -encoders` and then a test encode per candidate encoder, so opening
// the app flashed three or four windows before it had shown anything. Every
// transcode and subtitle extraction would do it again during playback.
func Hide(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
