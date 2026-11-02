package applog

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"time"
)

/*
 * Crash capture: where a Go fatal error goes when nobody is holding stderr.
 *
 * A panic nobody recovers, or an access violation inside a Windows callback,
 * ends a Go program by printing the reason and every goroutine's stack to
 * stderr. The window is linked -H=windowsgui and the server runs under the
 * service manager, and neither has a stderr anyone reads, so that message,
 * the only one that says why, was thrown away.
 *
 * That cost a diagnosis on 2026-11-01. The desktop window died 32 minutes into
 * a film: no line in its log, no Application Error in Windows' event log (Go
 * handles the exception itself, so Windows never sees a crash), and the TV
 * left at the film's refresh rate because nothing got to restore it. Every
 * trace said "it stopped" and none said why.
 *
 * debug.SetCrashOutput (Go 1.23) sends the same text to a file as well.
 */

// Crash file names, one per process, each beside its process's own log.
const (
	ServerCrashFileName = "lancastd-crash.log"
	ClientCrashFileName = "lancast-client-crash.log"
)

// EarlierCrash is a crash from a previous run, found at startup.
type EarlierCrash struct {
	// Path is where its report was moved, so the next crash cannot overwrite it.
	Path string
	// At is when the report was last written: when the process died.
	At time.Time
	// Cause is what a reader needs first: the report's "panic:", "fatal
	// error:" or "Exception" line, and where the crash happened (crashCause).
	// Empty if the report says neither.
	Cause string
}

// CaptureCrashes sends this process's fatal errors to name in dir. A report
// left by an earlier run is moved to name.1 first and returned, so the caller
// can say in its log that the last run crashed and where to read why.
func CaptureCrashes(dir, name string) (*EarlierCrash, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name)

	var earlier *EarlierCrash
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		prev := path + ".1"
		_ = os.Remove(prev)
		if err := os.Rename(path, prev); err == nil {
			earlier = &EarlierCrash{Path: prev, At: st.ModTime(), Cause: crashCause(prev)}
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return earlier, err
	}
	// SetCrashOutput keeps its own duplicate of the handle.
	defer f.Close()
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		return earlier, err
	}
	return earlier, nil
}

// crashCause says in one line what a crash report is about: its "panic:",
// "fatal error:" or "Exception" line when it has one, and where it happened,
// from the stack of the goroutine that crashed.
//
// The header is not always there. The first crash this captured (Go's limit
// on Windows callbacks, 2026-11-01) reached the file as stacks alone: the
// "fatal error:" line went to stderr and nowhere else, so the cause came out
// empty and the log said only that the last run crashed. The stack still said
// it plainly: syscall.compileCallback, called from clientwindow.Monitors.
func crashCause(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return causeOf(lines)
}

// causeOf is crashCause on a report's lines.
func causeOf(lines []string) string {
	header := ""
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "panic:") || strings.HasPrefix(l, "fatal error:") ||
			strings.HasPrefix(l, "Exception ") {
			header = l
			break
		}
	}
	where := crashSite(lines)
	switch {
	case header != "" && where != "":
		return header + " (" + where + ")"
	case header != "":
		return header
	case where != "":
		return "crashed " + where
	}
	return ""
}

// crashSite names where the first goroutine in a report, the one that
// crashed, was: the first frame outside Go's runtime, which says what failed,
// and the first in LANcast's own code, which says where. "" if neither is
// found.
func crashSite(lines []string) string {
	i := 0
	for i < len(lines) && !strings.HasPrefix(lines[i], "goroutine ") {
		i++
	}
	var failed, ours string
	for i++; i < len(lines); i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "goroutine ") {
			break // the end of the crashing goroutine
		}
		if strings.HasPrefix(l, "\t") || strings.HasPrefix(l, "created by ") {
			continue
		}
		fn := frameFunc(l)
		at := ""
		if i+1 < len(lines) && strings.HasPrefix(lines[i+1], "\t") {
			at = frameFile(lines[i+1])
		}
		if failed == "" && !strings.HasPrefix(fn, "runtime.") {
			failed = fn
		}
		if ours == "" && (strings.HasPrefix(fn, "lancast/") || strings.HasPrefix(fn, "main.")) {
			ours = fn
			if at != "" {
				ours += " at " + at
			}
		}
		if failed != "" && ours != "" {
			break
		}
	}
	switch {
	case ours == "":
		if failed == "" {
			return ""
		}
		return "in " + failed
	case failed == "" || strings.HasPrefix(ours, failed+" "):
		return "in " + ours
	}
	return "in " + failed + ", called from " + ours
}

// frameFunc is the function on a stack frame's first line, without its
// arguments: "syscall.NewCallback(...)" is "syscall.NewCallback".
func frameFunc(l string) string {
	if i := strings.LastIndex(l, "("); i > 0 {
		return l[:i]
	}
	return l
}

// frameFile is a stack frame's file and line, without the directory or the
// offsets: "\tlancast/internal/x/a.go:50 +0x6e fp=…" is "a.go:50". A build
// without -trimpath prints the full path, spaces and all ("D:/My Projects/…"),
// so it is cut at the line number, not at the first space.
func frameFile(l string) string {
	m := frameFileRe.FindStringSubmatch(l)
	if m == nil {
		return ""
	}
	return path.Base(filepath.ToSlash(m[1]))
}

var frameFileRe = regexp.MustCompile(`^\s*(.+?\.go:\d+)`)
