package service

import (
	"strings"
)

/*
 * The port lives in the service's command line (ADR 0089).
 *
 * `service install` writes `-addr` into the command line the service control
 * manager starts, and that command line stays the one place the port is
 * recorded. A second copy in the settings file would be a second answer to
 * "which port", and the two would disagree the first time somebody edited
 * one by hand.
 */

// withAddr rewrites a service command line's -addr, keeping everything else
// as it was. One that has none gains one at the end.
func withAddr(cmdline, addr string) string {
	args := splitCommandLine(cmdline)
	replaced := false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-addr" && i+1 < len(args):
			args[i+1] = addr
			replaced = true
			i++
		case strings.HasPrefix(args[i], "-addr="):
			args[i] = "-addr=" + addr
			replaced = true
		}
	}
	if !replaced {
		args = append(args, "-addr", addr)
	}
	return joinCommandLine(args)
}

// joinCommandLine is splitCommandLine's inverse for the command lines this
// package writes: a token with a space in it, or an empty one, is quoted.
func joinCommandLine(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t") {
			a = `"` + a + `"`
		}
		out[i] = a
	}
	return strings.Join(out, " ")
}
