package applog

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// clock is a time a test moves by hand.
type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }
func quietAt(c *clock, w time.Duration) *Quiet {
	q := NewQuiet(w)
	q.now = c.now
	return q
}

/*
 * The case it exists for: the peer that was switched off. One failure every
 * 63 seconds for an hour is 57 occurrences. With a ten-minute window that is
 * six lines, each after the first saying how many it stood for.
 */
func TestQuietSaysItOnceThenCounts(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)}
	q := quietAt(c, 10*time.Minute)

	written, total := 0, 0
	var lastRepeats []int
	for i := 0; i < 57; i++ {
		if ok, n := q.Allow("peer F7H2"); ok {
			written++
			lastRepeats = append(lastRepeats, n)
		}
		total++
		c.add(63 * time.Second)
	}
	if written != 6 {
		t.Fatalf("an hour of a failure every 63s wrote %d lines with a 10-minute window, want 6", written)
	}
	if lastRepeats[0] != 0 {
		t.Errorf("the first line claimed %d earlier repeats; it is the first", lastRepeats[0])
	}
	// Every occurrence is accounted for exactly: written, counted in a later
	// written line, or still pending for the next one.
	counted := 0
	for _, n := range lastRepeats {
		counted += n
	}
	q.mu.Lock()
	pending := q.seen["peer F7H2"].suppressed
	q.mu.Unlock()
	if written+counted+pending != total {
		t.Errorf("occurrences unaccounted for: %d written + %d counted + %d pending != %d",
			written, counted, pending, total)
	}
}

func TestQuietKeysAreIndependent(t *testing.T) {
	c := &clock{t: time.Now()}
	q := quietAt(c, time.Minute)
	if ok, _ := q.Allow("a"); !ok {
		t.Fatal("first a was suppressed")
	}
	if ok, _ := q.Allow("b"); !ok {
		t.Fatal("b was suppressed because a had just been written")
	}
	if ok, _ := q.Allow("a"); ok {
		t.Error("a repeated inside its window was written")
	}
}

// After the window, the next occurrence is written with the count it stands
// for, and the count starts again.
func TestQuietReportsWhatItSwallowed(t *testing.T) {
	c := &clock{t: time.Now()}
	q := quietAt(c, time.Minute)
	q.Allow("k")
	for range 5 {
		q.Allow("k")
	}
	c.add(2 * time.Minute)
	if ok, n := q.Allow("k"); !ok || n != 5 {
		t.Fatalf("after the window: write=%v repeats=%d, want true and 5", ok, n)
	}
	c.add(2 * time.Minute)
	if ok, n := q.Allow("k"); !ok || n != 0 {
		t.Fatalf("a quiet window then one more: write=%v repeats=%d, want true and 0", ok, n)
	}
}

// Memory is bounded: a flood of distinct keys does not grow the map for ever.
func TestQuietForgetsClosedKeys(t *testing.T) {
	c := &clock{t: time.Now()}
	q := quietAt(c, time.Minute)
	for i := 0; i < quietMaxKeys; i++ {
		q.Allow(fmt.Sprint("old", i))
	}
	c.add(2 * time.Minute)
	for i := 0; i < 10; i++ {
		q.Allow(fmt.Sprint("new", i))
	}
	q.mu.Lock()
	n := len(q.seen)
	q.mu.Unlock()
	if n > 20 {
		t.Errorf("%d keys held after the old ones' windows closed, want only the new ones", n)
	}
}

// Log writes due lines at their level, repeats at Debug, and adds repeats=N.
func TestQuietLog(t *testing.T) {
	c := &clock{t: time.Now()}
	q := quietAt(c, time.Minute)
	var buf bytes.Buffer
	l := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	for range 4 {
		q.Log(l, slog.LevelWarn, "401 /api/x 10.0.0.5", "request refused", "route", "/api/x")
	}
	if got := strings.Count(buf.String(), "request refused"); got != 1 {
		t.Fatalf("4 repeats inside the window wrote %d Info+ lines, want 1:\n%s", got, buf.String())
	}
	c.add(2 * time.Minute)
	q.Log(l, slog.LevelWarn, "401 /api/x 10.0.0.5", "request refused", "route", "/api/x")
	if !strings.Contains(buf.String(), "repeats=3") {
		t.Errorf("the next line did not say how many it stood for:\n%s", buf.String())
	}
	if strings.Count(buf.String(), "level=WARN") != 2 {
		t.Errorf("want two WARN lines in total:\n%s", buf.String())
	}
}
