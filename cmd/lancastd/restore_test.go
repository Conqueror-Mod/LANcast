package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"lancast/internal/store"
)

func TestRestoreRequiresAFrom(t *testing.T) {
	err := runRestore(nil)
	if err == nil {
		t.Fatal("restore with no -from succeeded")
	}
	if !strings.Contains(err.Error(), "-from") {
		t.Errorf("error %q does not say what is missing", err)
	}
}

/*
 * The schema gate's message is the whole point of the gate, so it is asserted
 * rather than assumed. Somebody restoring a backup from a newer build needs to
 * be told to update, not told a version number.
 */
func TestDescribeSnapshotFailureExplainsANewerBackup(t *testing.T) {
	err := describeSnapshotFailure("b.db", &store.SnapshotTooNewError{Found: 40, Supported: 39})
	msg := err.Error()
	for _, want := range []string{"newer LANcast", "update LANcast"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
	// The typed error must survive being described, so callers can still test
	// for it rather than matching on a sentence.
	var tooNew *store.SnapshotTooNewError
	if !errors.As(err, &tooNew) {
		t.Error("describing the failure lost the typed error")
	}
}

func TestDescribeSnapshotFailureExplainsAWrongFile(t *testing.T) {
	err := describeSnapshotFailure("holiday.jpg", fmt.Errorf("inspect: %w", store.ErrNotSnapshot))
	if !strings.Contains(err.Error(), "holiday.jpg") {
		t.Errorf("message %q does not name the file", err)
	}
	if !errors.Is(err, store.ErrNotSnapshot) {
		t.Error("describing the failure lost ErrNotSnapshot")
	}
}

// A locked database still gets reset-auth's advice, since the cause and the
// fix are identical.
func TestDescribeSnapshotFailurePassesOtherFailuresThrough(t *testing.T) {
	err := describeSnapshotFailure("lancast.db", errors.New("database is locked"))
	if !strings.Contains(err.Error(), "stop the LANcast server") {
		t.Errorf("message %q does not give the fix", err)
	}
}

// Backups are the one place a person compares two file sizes and decides which
// is the real one, so the number has to be readable.
func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 bytes"},
		{512, "512 bytes"},
		{2048, "2.0 KB"},
		{104 * 1024 * 1024, "104.0 MB"},
		{3 * 1024 * 1024 * 1024, "3.0 GB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.in); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
