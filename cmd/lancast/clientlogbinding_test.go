package main

import (
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/applog"
)

/*
 * The boundary, not the tailer: internal/applog has its own suite for what the
 * end of a file is. What matters here is that this reads the *client's* file
 * and nothing else, and that the ordinary case of never having written one is
 * not reported as a fault.
 */

func clientLog(t *testing.T, dir string) map[string]any {
	t.Helper()
	fn, ok := clientLogBindings(dir)["lancastClientLog"].(func() map[string]any)
	if !ok {
		t.Fatal("lancastClientLog is missing")
	}
	return fn()
}

func TestClientLogReadsTheClientsOwnFile(t *testing.T) {
	/*
	 * The tray and the server keep their own files in directories that can be
	 * the same one. Reading the wrong file here would be quiet and wrong in the
	 * worst way: a plausible log that answers a question about a different
	 * process.
	 */
	dir := t.TempDir()
	write(t, filepath.Join(dir, applog.ClientFileName), "window opened\n")
	write(t, filepath.Join(dir, applog.FileName), "the server said something else\n")

	got := clientLog(t, dir)
	lines, _ := got["lines"].([]string)
	if len(lines) != 1 || lines[0] != "window opened" {
		t.Fatalf("lines = %v, want the client's own log", lines)
	}
	if got["path"] != filepath.Join(dir, applog.ClientFileName) {
		t.Errorf("path = %v, want the client log", got["path"])
	}
}

func TestClientLogWithNothingWrittenYetIsNotAnError(t *testing.T) {
	// A window that has only ever run from a terminal may never have opened
	// one, and a section that says "could not read the log" for that is
	// reporting a fault where there is none.
	got := clientLog(t, t.TempDir())
	if got["error"] != nil {
		t.Errorf("error = %v, want none", got["error"])
	}
	lines, ok := got["lines"].([]string)
	if !ok || len(lines) != 0 {
		t.Errorf("lines = %v, want an empty list rather than null", got["lines"])
	}
}

func TestClientLogTakesNoArguments(t *testing.T) {
	/*
	 * The page names nothing — no filename, no directory, no line count — so
	 * there is nothing for it to name wrongly. The same rule the games
	 * bindings keep, asserted here because a later "let the page pick how many
	 * lines" is exactly how it would be given a path.
	 */
	if _, ok := clientLogBindings(t.TempDir())["lancastClientLog"].(func() map[string]any); !ok {
		t.Error("lancastClientLog takes arguments; it must not")
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
