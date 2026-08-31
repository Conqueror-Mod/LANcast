// Package applog gives a background server somewhere to write.
//
// A Windows service has no console and no inherited stderr: anything the
// process logs is discarded by the operating system. That is not a cosmetic
// gap. When the service died on a real machine, the only record anywhere was
// Windows' own "terminated unexpectedly", and telling a crash apart from an
// external kill took reading event-log IDs — because LANcast itself had said
// nothing at all.
package applog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FileName is the log inside the data directory. It sits beside the database
// so "where are the logs" has the same answer as "where is my data".
const FileName = "lancastd.log"

// maxBytes is when the current log rolls aside. Small on purpose: this exists
// to answer "why did it stop", which is always near the end of the file, and a
// server left running for months must not fill a disk to say so.
const maxBytes = 4 << 20

// File is an append-only log that rolls once when it grows past maxBytes,
// keeping exactly one previous generation.
//
// One generation, not a configurable depth: the question this answers is what
// happened just before a stop, and a rotation policy is a thing to get wrong
// for no benefit here.
type File struct {
	mu   sync.Mutex
	path string
	f    *os.File
	n    int64
}

/*
 * TrayFileName is the tray's own log, deliberately not the server's.
 *
 * The tray runs in the user's session while the service writes lancastd.log
 * from session 0, and both rotate by renaming at a size threshold. Two
 * processes rotating one file is a race that ends with the *server's* log
 * truncated or lost — a worse fault than the silence this exists to fix.
 */
const TrayFileName = "lancast-tray.log"

// Open creates or appends to the server's log in dir.
func Open(dir string) (*File, error) { return OpenNamed(dir, FileName) }

// OpenNamed creates or appends to a named log in dir, for a process that must
// not share the server's file.
func OpenNamed(dir, name string) (*File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("log directory: %w", err)
	}
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return nil, fmt.Errorf("open log %s: %w", path, err)
	}
	var size int64
	if st, err := f.Stat(); err == nil {
		size = st.Size()
	}
	return &File{path: path, f: f, n: size}, nil
}

// Path is where the log is being written, for reporting it at startup.
func (w *File) Path() string { return w.path }

func (w *File) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.f == nil {
		// A previous rotation could not reopen the file. Drop the line rather
		// than fail the write: logging must never take the server down.
		return len(p), nil
	}
	if w.n+int64(len(p)) > maxBytes {
		w.rotate()
		if w.f == nil {
			return len(p), nil
		}
	}
	n, err := w.f.Write(p)
	w.n += int64(n)
	return n, err
}

// rotate moves the current log aside and starts a new one. Every step is
// best-effort: a log that cannot roll is not a reason to stop serving media.
func (w *File) rotate() {
	_ = w.f.Close()
	w.f = nil

	prev := w.path + ".1"
	_ = os.Remove(prev)
	if err := os.Rename(w.path, prev); err != nil {
		// Could not move it aside — reopen and keep appending rather than lose
		// the destination entirely.
		if f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640); err == nil {
			w.f = f
			if st, err := f.Stat(); err == nil {
				w.n = st.Size()
			}
		}
		return
	}
	if f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640); err == nil {
		w.f = f
		w.n = 0
	}
}

func (w *File) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// Tee returns a writer that sends every line to the log file and also to also,
// where a failure on either one cannot stop the other.
//
// This exists because io.MultiWriter is the obvious thing and is wrong here.
// It writes sequentially and returns on the first error, so pairing the log
// file with os.Stderr means that under a Windows service — where there is no
// stderr and every write to it fails — the file receives nothing at all. The
// log shipped empty in exactly the situation it was built for, and the local
// test missed it by running in a terminal where stderr worked.
//
// Failures are swallowed rather than reported: the caller is a logger, nothing
// upstream will act on the error, and a log that cannot be written must never
// take the server down with it.
func Tee(primary, also io.Writer) io.Writer {
	return tee{primary: primary, also: also}
}

type tee struct{ primary, also io.Writer }

func (t tee) Write(p []byte) (int, error) {
	if t.primary != nil {
		_, _ = t.primary.Write(p)
	}
	if t.also != nil {
		_, _ = t.also.Write(p)
	}
	return len(p), nil
}

// tailWindow is the most of the log Tail will read. The log is capped at 4 MB
// and the question it answers is always near the end, so reading the whole file
// to return the last few hundred lines is work with no reader.
const tailWindow = 512 << 10

// Tail returns the last n lines of the log in dir, oldest first, and reports
// whether the returned lines start at the beginning of the file. A false there
// is what lets a reader be told the view is partial rather than assume it is
// the whole log — the same reason the grid says "120 of 1,226".
//
// A missing log is not an error: a server that has only ever run in a terminal
// may never have opened one, and that is a supported configuration.
func Tail(dir string, n int) (lines []string, complete bool, err error) {
	if n <= 0 {
		return nil, true, nil
	}
	f, err := os.Open(filepath.Join(dir, FileName))
	if os.IsNotExist(err) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	size := st.Size()
	start := int64(0)
	whole := true
	if size > tailWindow {
		start = size - tailWindow
		whole = false
	}
	buf := make([]byte, size-start)
	if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
		return nil, false, err
	}

	split := strings.Split(strings.ReplaceAll(string(buf), "\r\n", "\n"), "\n")
	// A window that starts mid-file almost certainly starts mid-line; a
	// half-record is worse than one fewer record.
	if !whole && len(split) > 0 {
		split = split[1:]
	}
	out := make([]string, 0, len(split))
	for _, s := range split {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	if len(out) > n {
		return out[len(out)-n:], false, nil
	}
	return out, whole, nil
}
