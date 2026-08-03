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
	"os"
	"path/filepath"
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

// Open creates or appends to the log in dir.
func Open(dir string) (*File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("log directory: %w", err)
	}
	path := filepath.Join(dir, FileName)
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
