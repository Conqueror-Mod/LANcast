package main

import (
	"testing"
	"time"
)

/*
 * The periodic scan's clock.
 *
 * Extracted from the loop so it can be tested in microseconds rather than in
 * hours — the same reason the transcode decision is a pure function over probe
 * output. What is being checked is not "does a scan run" (Scanner.Start is
 * tested where it lives) but the three things this rule gets wrong if written
 * carelessly: firing the moment it is switched on, firing every tick once due,
 * and remembering a stale clock across being switched off.
 */
func TestScanDue(t *testing.T) {
	base := time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)

	t.Run("off means never", func(t *testing.T) {
		if _, due := scanDue(base, base.Add(72*time.Hour), 0); due {
			t.Error("a disabled timer fired")
		}
	})

	t.Run("switching it on starts the clock rather than firing", func(t *testing.T) {
		last, due := scanDue(time.Time{}, base, 1)
		if due {
			t.Error("fired the moment it was enabled — that is a scan nobody asked for, " +
				"for an interval that elapsed while the feature was off")
		}
		if !last.Equal(base) {
			t.Errorf("clock = %v, want it started at %v", last, base)
		}
	})

	t.Run("waits the interval, then fires once", func(t *testing.T) {
		last, due := scanDue(base, base.Add(59*time.Minute), 1)
		if due {
			t.Error("fired early")
		}
		last, due = scanDue(last, base.Add(time.Hour), 1)
		if !due {
			t.Fatal("did not fire after the interval elapsed")
		}
		// The clock has to move, or every subsequent tick is also "overdue" and
		// the server scans continuously.
		if _, again := scanDue(last, base.Add(time.Hour).Add(time.Minute), 1); again {
			t.Error("fired again a minute later — the clock did not advance")
		}
	})

	t.Run("switching it off forgets the clock", func(t *testing.T) {
		last, _ := scanDue(base, base.Add(30*time.Minute), 1)
		off, _ := scanDue(last, base.Add(31*time.Minute), 0)
		if !off.IsZero() {
			t.Error("kept a clock while disabled, so re-enabling would fire immediately")
		}
		// Re-enabled: starts again, does not fire on the strength of time that
		// passed while it was off.
		if _, due := scanDue(off, base.Add(48*time.Hour), 1); due {
			t.Error("fired on re-enable")
		}
	})

	t.Run("a long interval is respected", func(t *testing.T) {
		if _, due := scanDue(base, base.Add(100*time.Hour), 168); due {
			t.Error("weekly fired after four days")
		}
		if _, due := scanDue(base, base.Add(169*time.Hour), 168); !due {
			t.Error("weekly never fired")
		}
	})
}
