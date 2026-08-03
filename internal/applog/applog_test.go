package applog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritesToTheDataDir(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	if _, err := w.Write([]byte("hello\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello\n" {
		t.Errorf("log contents = %q", body)
	}
}

// A restart must not discard what the previous run said — that is usually the
// run whose ending is being investigated.
func TestReopenAppends(t *testing.T) {
	dir := t.TempDir()

	first, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	first.Write([]byte("first run\n"))
	first.Close()

	second, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	second.Write([]byte("second run\n"))
	second.Close()

	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "first run") || !strings.Contains(string(body), "second run") {
		t.Errorf("expected both runs in the log, got %q", body)
	}
}

// A server left running for months must not fill the disk to say so.
func TestRotatesAndKeepsOneGeneration(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	line := strings.Repeat("x", 64<<10) + "\n"
	for written := 0; written < 3*maxBytes; written += len(line) {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	cur, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("current log missing after rotation: %v", err)
	}
	if cur.Size() > maxBytes {
		t.Errorf("current log is %d bytes, above the %d cap", cur.Size(), maxBytes)
	}

	prev, err := os.Stat(filepath.Join(dir, FileName+".1"))
	if err != nil {
		t.Fatalf("previous generation missing: %v", err)
	}
	if prev.Size() > maxBytes {
		t.Errorf("previous log is %d bytes, above the %d cap", prev.Size(), maxBytes)
	}

	// Exactly two: one rolled generation, never an unbounded pile.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("log directory holds %v, want exactly the log and one rolled copy", names)
	}
}

// The most recent lines are the ones that explain a stop, so they must survive
// the rotation that happens while they are being written.
func TestNewestLinesSurviveRotation(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	filler := strings.Repeat("y", 64<<10) + "\n"
	for written := 0; written < maxBytes+(128<<10); written += len(filler) {
		w.Write([]byte(filler))
	}
	w.Write([]byte("the last thing it said\n"))

	body, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "the last thing it said") {
		t.Error("the most recent line is not in the current log")
	}
}

// Logging must never take the server down: a directory that cannot be created
// is reported to the caller, not panicked over, and writes after a failed
// rotation are dropped rather than returning errors up the stack.
func TestWriteAfterCloseDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w.Close()

	n, err := w.Write([]byte("after close\n"))
	if err != nil {
		t.Errorf("Write after Close returned %v; it must be a silent no-op", err)
	}
	if n != len("after close\n") {
		t.Errorf("Write after Close reported %d bytes, want the full length", n)
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close returned %v, want nil", err)
	}
}
