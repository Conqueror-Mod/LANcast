package applog

import (
	"bufio"
	"os"
	"path/filepath"
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
	// Cause is the report's first "panic:", "fatal error:" or "Exception"
	// line, which is what a reader needs first. Empty if none was found.
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

// crashCause is the first line of a crash report that names what happened.
func crashCause(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "panic:") || strings.HasPrefix(line, "fatal error:") ||
			strings.HasPrefix(line, "Exception ") {
			return line
		}
	}
	return ""
}
