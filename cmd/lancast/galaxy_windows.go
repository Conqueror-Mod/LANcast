package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

/*
 * startGalaxy runs GOG Galaxy with the command line its own Start-menu
 * shortcut uses, character for character: `/path="D:\Games\X"`, quotes inside
 * the argument. Go's default quoting would wrap the whole argument instead
 * (`"/path=D:\Games\X"`); most programs read the two the same, and this one is
 * not ours to test, so it gets exactly what GOG gives it.
 *
 * The pieces are GalaxyArgs' output, validated there: digits for the id and a
 * path without a quote in it, so nothing here can end an argument early. No
 * shell is involved; CmdLine is the line CreateProcess receives.
 */
func startGalaxy(exe string, args []string) error {
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: `"` + exe + `" ` + strings.Join(args, " "),
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
