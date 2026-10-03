package main

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"

	"lancast/internal/applog"
)

/*
 * The page writes to the window's log (docs/logging-plan.md, Phase 2).
 *
 * The desktop window could read its own log (lancastClientLog) but nothing
 * the web app noticed could reach it. A direct play that failed and fell back
 * to a conversion, a codec claim withdrawn after a failure (the ratchet that
 * once re-encoded every HEVC film for weeks, silently), being signed out
 * mid-session, an uncaught error: all of it went to a browser console nobody
 * has open, and was gone by the time anybody asked.
 *
 * lancastClientNote is the way in, and it is narrow on purpose, because the
 * page is the less trusted side of this boundary:
 *   - **a closed set of areas** ‒ an unknown one is refused, so a log line
 *     always says which part of the app it is about;
 *   - **one line, capped** ‒ newlines and other control characters are
 *     replaced, so the page cannot forge a second log line, and the message
 *     is cut at noteMaxRunes;
 *   - **quiet** ‒ the same note repeated is one line per window with a count
 *     (applog.Quiet), and each area has a hard ceiling per minute on top, so a
 *     broken page in a loop cannot fill the file with *different* lines
 *     either.
 */

// noteAreas is every area the page may write about.
var noteAreas = map[string]bool{
	"playback":     true, // failures and fallbacks to conversion
	"capabilities": true, // codec and container claims withdrawn
	"auth":         true, // signed out while the page thought otherwise
	"together":     true, // Watch Together seen from this window
	"error":        true, // uncaught errors and rejected promises
}

const (
	noteMaxRunes     = 500
	notePerMinute    = 30 // per area
	noteQuietWindow  = 10 * time.Minute
	noteRefusedLevel = slog.LevelDebug
)

// noteLevel maps the page's word to a level; anything else is Info.
func noteLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "error":
		return slog.LevelError
	case "warn", "warning":
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// cleanNote makes a page-supplied message one bounded line.
func cleanNote(msg string) string {
	var b strings.Builder
	n := 0
	for _, r := range msg {
		if n >= noteMaxRunes {
			b.WriteString("…")
			break
		}
		if unicode.IsControl(r) {
			r = ' '
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

// noter writes page notes to a logger under the rules above.
type noter struct {
	log   func() *slog.Logger // read per call, so the default logger can change
	quiet *applog.Quiet
	now   func() time.Time

	mu     sync.Mutex
	minute map[string]noteWindow // per area
}

type noteWindow struct {
	start time.Time
	count int
}

func newNoter(log func() *slog.Logger) *noter {
	return &noter{log: log, quiet: applog.NewQuiet(noteQuietWindow), now: time.Now, minute: map[string]noteWindow{}}
}

// note writes one page note and reports whether it was accepted. A refused
// note (unknown area, empty, over the ceiling) is not an error to the page:
// logging must never be the thing that breaks the app.
func (n *noter) note(level, area, message string) bool {
	area = strings.ToLower(strings.TrimSpace(area))
	if !noteAreas[area] {
		return false
	}
	msg := cleanNote(message)
	if msg == "" {
		return false
	}

	now := n.now()
	n.mu.Lock()
	w := n.minute[area]
	if now.Sub(w.start) >= time.Minute {
		w = noteWindow{start: now}
	}
	w.count++
	n.minute[area] = w
	over := w.count > notePerMinute
	n.mu.Unlock()
	if over {
		n.log().Log(context.Background(), noteRefusedLevel, "page note over the per-minute ceiling", "area", area)
		return false
	}

	n.quiet.Log(n.log(), noteLevel(level), "note|"+area+"|"+msg, msg, "area", area, "from", "page")
	return true
}

// clientNoteBindings is the window function behind page notes.
func clientNoteBindings() map[string]any {
	n := newNoter(slog.Default)
	return map[string]any{
		// lancastClientNote(level, area, message) -> accepted. Three strings,
		// nothing else: no file, no logger name, no format.
		"lancastClientNote": func(level, area, message string) bool {
			return n.note(level, area, message)
		},
	}
}
