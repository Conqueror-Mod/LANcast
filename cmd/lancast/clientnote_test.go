package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func testNoter() (*noter, *bytes.Buffer, func(time.Duration)) {
	var buf bytes.Buffer
	l := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	n := newNoter(func() *slog.Logger { return l })
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	n.now = func() time.Time { return now }
	return n, &buf, func(d time.Duration) { now = now.Add(d) }
}

func lines(buf *bytes.Buffer) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// A note lands as one line with its area and level.
func TestANoteIsWritten(t *testing.T) {
	n, buf, _ := testNoter()
	if !n.note("warn", "playback", "Dreamcatcher (item 6782) would not play directly; converting") {
		t.Fatal("a valid note was refused")
	}
	got := lines(buf)
	if len(got) != 1 || !strings.Contains(got[0], "level=WARN") || !strings.Contains(got[0], "area=playback") {
		t.Fatalf("got %q", got)
	}
}

// The page cannot forge a second log line by putting a newline in a message.
func TestANoteCannotForgeALogLine(t *testing.T) {
	n, buf, _ := testNoter()
	n.note("info", "error", "boom\ntime=2026-10-03T09:00:00Z level=ERROR msg=\"server compromised\"\r\n")
	got := lines(buf)
	if len(got) != 1 {
		t.Fatalf("one note became %d log lines:\n%s", len(got), buf)
	}
}

/*
 * cleanNote itself removes control characters.
 *
 * The test above passes on its own merits only partly: slog's text handler
 * already escapes a newline inside a quoted value, so it would stay one line
 * even if cleanNote did nothing. Cleaning is the layer that does not depend
 * on which handler is configured, so it is checked directly, where removing it
 * fails.
 */
func TestCleanNoteRemovesControlCharacters(t *testing.T) {
	got := cleanNote("boom\nlevel=ERROR\r\x1b[31mred\ttab")
	for _, r := range got {
		if r < 0x20 || r == 0x7f {
			t.Fatalf("cleanNote left control character %U in %q", r, got)
		}
	}
	if !strings.HasPrefix(got, "boom level=ERROR") {
		t.Errorf("cleanNote = %q; the text around a newline should survive as one line", got)
	}
}

// Only the known areas are accepted; anything else is refused, not logged.
func TestUnknownAreasAreRefused(t *testing.T) {
	n, buf, _ := testNoter()
	for _, area := range []string{"", "server", "../../etc", "PLAYBACK "} {
		accepted := n.note("info", area, "x")
		if area == "PLAYBACK " {
			// Case and spacing are forgiven; the word is what matters.
			if !accepted {
				t.Errorf("area %q was refused", area)
			}
			continue
		}
		if accepted {
			t.Errorf("area %q was accepted", area)
		}
	}
	if n := len(lines(buf)); n != 1 {
		t.Errorf("refused notes reached the log: %d lines\n%s", n, buf)
	}
}

// A message is capped, so one note cannot be a megabyte.
func TestANoteIsCapped(t *testing.T) {
	n, buf, _ := testNoter()
	n.note("info", "error", strings.Repeat("x", 10_000))
	if len(buf.String()) > 2*noteMaxRunes {
		t.Fatalf("a 10,000-character note wrote %d bytes", len(buf.String()))
	}
}

// The same note repeated is one line per window; a page in a loop writing
// different notes hits the per-area ceiling.
func TestNotesAreQuietAndCeilinged(t *testing.T) {
	n, buf, tick := testNoter()
	for range 50 {
		n.note("warn", "playback", "the same failure")
	}
	if c := len(lines(buf)); c != 1 {
		t.Fatalf("50 identical notes wrote %d lines, want 1", c)
	}

	buf.Reset()
	tick(11 * time.Minute)
	accepted := 0
	for i := range 100 {
		if n.note("warn", "error", fmt.Sprintf("distinct failure %d", i)) {
			accepted++
		}
	}
	if accepted != notePerMinute {
		t.Fatalf("100 distinct notes in a minute: %d accepted, want the ceiling of %d", accepted, notePerMinute)
	}
	// Another area is not starved by the first.
	if !n.note("warn", "auth", "signed out") {
		t.Error("one area's flood refused a note in another area")
	}
	// And the ceiling lifts after a minute.
	tick(61 * time.Second)
	if !n.note("warn", "error", "after the minute") {
		t.Error("the ceiling did not lift after a minute")
	}
}
