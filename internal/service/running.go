package service

import (
	"fmt"
	"strings"
)

// Running describes a LANcast server that already holds the machine, so a second
// one that refuses to start can say *which* one rather than only that it exists.
//
// This is the diagnostic gap the single-instance guard left: "another LANcast
// server is already running" is true, unactionable, and identical whether the
// offender is the installed service, a stray desktop launch, or a build being
// tested from a terminal. Each of those wants a different response, and the
// answer took Get-CimInstance to find.
type Running struct {
	// Service reports that the holder is the installed OS service, which is the
	// common case on a machine where LANcast was installed rather than run from
	// a build — and the one that survives a reboot without anybody launching it.
	Service bool
	// PID is the holding process, 0 when it could not be determined.
	PID uint32
	// DataDir and Addr are what the holder was started with, when they can be
	// read. The data dir matters most: two servers on one database is the bug
	// this guard exists to prevent, and knowing which directory the winner holds
	// is how an operator tells "the service has my library" from "the service
	// has an empty one".
	DataDir string
	Addr    string
}

// Describe renders the holder as a sentence for a log line or a message box.
// Empty when nothing could be established, so a caller can fall back to the
// generic message rather than print a blank.
func (r Running) Describe() string {
	if !r.Service && r.PID == 0 {
		return ""
	}

	var b strings.Builder
	if r.Service {
		fmt.Fprintf(&b, "the installed %q service", Name)
	} else {
		b.WriteString("another LANcast process")
	}
	if r.PID != 0 {
		fmt.Fprintf(&b, " (pid %d)", r.PID)
	}
	if r.DataDir != "" {
		fmt.Fprintf(&b, ", data %s", r.DataDir)
	}
	if r.Addr != "" {
		fmt.Fprintf(&b, ", listening on %s", r.Addr)
	}
	return b.String()
}

// Hint is what to actually do about it, which differs by holder: a service is
// stopped through the service manager and needs administrator rights, while a
// stray process is just closed.
func (r Running) Hint() string {
	if r.Service {
		return "stop it first: `lancastd service stop` as administrator, " +
			"or Stop-Service " + Name + " in an elevated PowerShell"
	}
	if r.PID != 0 {
		return "close the running LANcast, or end that process, then try again"
	}
	return "close any running LANcast — including one started by the installer — then try again"
}

// dataDirFromArgs reads the -data and -addr values out of a service's recorded
// command line.
//
// Pure, and separate from the Windows API call that produces the string, for
// the reason probe.ParseJSON is separate from running ffprobe (CLAUDE.md): the
// parsing is where the mistakes are, and it should be testable without a
// service installed or a Windows host to install it on.
//
// Accepts both `-data X` and `-data=X`, because a hand-edited service entry may
// use either and this is a diagnostic — being wrong here would put a false
// directory in front of an operator who is already confused.
func dataDirFromArgs(args []string) (dataDir, addr string) {
	get := func(flag string) string {
		for i, a := range args {
			if a == flag && i+1 < len(args) {
				return args[i+1]
			}
			if strings.HasPrefix(a, flag+"=") {
				return strings.TrimPrefix(a, flag+"=")
			}
		}
		return ""
	}
	return get("-data"), get("-addr")
}

// splitCommandLine splits a Windows service binary path into arguments,
// respecting the double quotes that wrap a path containing spaces —
// `"C:\Program Files\LANcast\LANcast-Server.exe" service run -data C:\...`
// is four tokens plus the flags, not seven.
//
// Deliberately not CommandLineToArgvW: this needs no backslash-escape handling
// (a service path is a plain quoted path), and keeping it in Go keeps it
// testable on any platform. If it ever needs the full escape rules, that is the
// signal to call the real API instead of growing this.
func splitCommandLine(s string) []string {
	var args []string
	var cur strings.Builder
	inQuote := false

	flush := func() {
		if cur.Len() > 0 {
			args = append(args, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ' ' && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return args
}
