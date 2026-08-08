package applog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A server that has only ever run in a terminal may never have opened a log.
// That is a supported configuration, so it reads as an empty log rather than an
// error the UI has to render.
func TestTailMissingLogIsNotAnError(t *testing.T) {
	lines, complete, err := Tail(t.TempDir(), 100)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("lines = %v, want none", lines)
	}
	if !complete {
		t.Error("complete = false for an absent log; nothing was withheld")
	}
}

func TestTailReturnsLastLinesOldestFirst(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one\ntwo\nthree\nfour\n")

	lines, complete, err := Tail(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	if complete {
		t.Error("complete = true while lines were dropped")
	}
	if got := strings.Join(lines, "|"); got != "three|four" {
		t.Errorf("lines = %q, want %q", got, "three|four")
	}
}

// Asking for more than the log holds returns all of it, and says so — a reader
// told "this is partial" when it is not goes looking for something that is not
// missing.
func TestTailShortLogIsComplete(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one\ntwo\n")

	lines, complete, err := Tail(dir, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !complete {
		t.Error("complete = false for a log returned whole")
	}
	if len(lines) != 2 {
		t.Errorf("lines = %v, want 2", lines)
	}
}

// The tail is read through a fixed window from the end, so a log larger than
// the window starts mid-line. A half-record is worse than one fewer record.
func TestTailDropsPartialFirstLine(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; b.Len() < tailWindow+8192; i++ {
		b.WriteString("line padded out to make the file exceed the read window\n")
	}
	write(t, dir, b.String())

	lines, complete, err := Tail(dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if complete {
		t.Error("complete = true for a log read through a window")
	}
	for i, l := range lines {
		if l != "line padded out to make the file exceed the read window" {
			t.Fatalf("line %d is truncated: %q", i, l)
		}
	}
}

// Blank lines are not records. Dropping them here keeps the count the UI shows
// equal to the number of things that actually happened.
func TestTailSkipsBlankLines(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one\n\n\ntwo\n\n")

	lines, _, err := Tail(dir, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(lines, "|"); got != "one|two" {
		t.Errorf("lines = %q, want %q", got, "one|two")
	}
}

func TestTailZeroLines(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one\ntwo\n")
	lines, _, err := Tail(dir, 0)
	if err != nil || len(lines) != 0 {
		t.Fatalf("Tail(0) = %v, %v", lines, err)
	}
}

func write(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
