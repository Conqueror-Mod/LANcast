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
		if _, due := scanDue(base, base.Add(72*time.Hour), 0, nil); due {
			t.Error("a disabled timer fired")
		}
	})

	t.Run("switching it on starts the clock rather than firing", func(t *testing.T) {
		last, due := scanDue(time.Time{}, base, 1, nil)
		if due {
			t.Error("fired the moment it was enabled — that is a scan nobody asked for, " +
				"for an interval that elapsed while the feature was off")
		}
		if !last.Equal(base) {
			t.Errorf("clock = %v, want it started at %v", last, base)
		}
	})

	t.Run("waits the interval, then fires once", func(t *testing.T) {
		last, due := scanDue(base, base.Add(59*time.Minute), 1, nil)
		if due {
			t.Error("fired early")
		}
		last, due = scanDue(last, base.Add(time.Hour), 1, nil)
		if !due {
			t.Fatal("did not fire after the interval elapsed")
		}
		// The clock has to move, or every subsequent tick is also "overdue" and
		// the server scans continuously.
		if _, again := scanDue(last, base.Add(time.Hour).Add(time.Minute), 1, nil); again {
			t.Error("fired again a minute later — the clock did not advance")
		}
	})

	t.Run("switching it off forgets the clock", func(t *testing.T) {
		last, _ := scanDue(base, base.Add(30*time.Minute), 1, nil)
		off, _ := scanDue(last, base.Add(31*time.Minute), 0, nil)
		if !off.IsZero() {
			t.Error("kept a clock while disabled, so re-enabling would fire immediately")
		}
		// Re-enabled: starts again, does not fire on the strength of time that
		// passed while it was off.
		if _, due := scanDue(off, base.Add(48*time.Hour), 1, nil); due {
			t.Error("fired on re-enable")
		}
	})

	t.Run("a long interval is respected", func(t *testing.T) {
		if _, due := scanDue(base, base.Add(100*time.Hour), 168, nil); due {
			t.Error("weekly fired after four days")
		}
		if _, due := scanDue(base, base.Add(169*time.Hour), 168, nil); !due {
			t.Error("weekly never fired")
		}
	})
}

/*
 * The preferred hour.
 *
 * A gate on top of the interval rather than a schedule replacing it, so the
 * cases worth writing down are the ones where the two meet — and the one that
 * would be a silent, permanent failure.
 */

func hourPtr(h int) *int { return &h }

func TestAPreferredHourHoldsADueScanBack(t *testing.T) {
	last := time.Date(2026, 9, 15, 2, 0, 0, 0, time.Local)
	// A full day later, but at ten in the morning rather than at three.
	now := time.Date(2026, 9, 16, 10, 0, 0, 0, time.Local)
	if _, due := scanDue(last, now, 24, hourPtr(3)); due {
		t.Error("scanned at 10am when 3am was asked for")
	}
}

func TestAPreferredHourLetsADueScanThrough(t *testing.T) {
	last := time.Date(2026, 9, 15, 3, 0, 0, 0, time.Local)
	now := time.Date(2026, 9, 16, 3, 30, 0, 0, time.Local)
	if _, due := scanDue(last, now, 24, hourPtr(3)); !due {
		t.Error("held a due scan back during the hour it was asked for")
	}
}

func TestTheGateDoesNotRestartTheInterval(t *testing.T) {
	/*
	 * The failure that would look like the timer being off entirely.
	 *
	 * The ticker runs every minute. If a refused scan advanced `last`, the
	 * interval would restart on every one of those minutes and the scan would
	 * never come due — so the gate would not delay a scan, it would cancel it
	 * for ever.
	 */
	last := time.Date(2026, 9, 15, 2, 0, 0, 0, time.Local)
	held := last
	for i := 0; i < 60; i++ {
		now := time.Date(2026, 9, 16, 10, i, 0, 0, time.Local)
		held, _ = scanDue(held, now, 24, hourPtr(3))
	}
	if !held.Equal(last) {
		t.Fatalf("last moved to %v while the gate was holding; the scan would never come due", held)
	}
	// And when the hour finally arrives it fires.
	if _, due := scanDue(held, time.Date(2026, 9, 17, 3, 0, 0, 0, time.Local), 24, hourPtr(3)); !due {
		t.Error("never fired once the preferred hour arrived")
	}
}

func TestMidnightIsAChoosableHour(t *testing.T) {
	/*
	 * Why the setting is a pointer rather than an int with a sentinel: midnight
	 * is the hour somebody is most likely to pick, and a `0` meaning "unset"
	 * would make it the one hour that cannot be chosen.
	 */
	last := time.Date(2026, 9, 15, 0, 0, 0, 0, time.Local)
	now := time.Date(2026, 9, 16, 0, 5, 0, 0, time.Local)
	if _, due := scanDue(last, now, 24, hourPtr(0)); !due {
		t.Error("midnight was treated as no preference")
	}
}

func TestNoPreferredHourScansWheneverItIsDue(t *testing.T) {
	// The default and the behaviour of every existing server: unchanged.
	last := time.Date(2026, 9, 15, 2, 0, 0, 0, time.Local)
	now := time.Date(2026, 9, 16, 10, 0, 0, 0, time.Local)
	if _, due := scanDue(last, now, 24, nil); !due {
		t.Error("a due scan with no preferred hour did not run")
	}
}

func TestTheIntervalStillGovernsHowOften(t *testing.T) {
	/*
	 * The gate does not make a scan due; it only withholds one that already is.
	 * Without this, "at 3" on a weekly interval would scan every night.
	 */
	last := time.Date(2026, 9, 16, 3, 0, 0, 0, time.Local)
	// The right hour, the next day, but the interval is a week.
	now := time.Date(2026, 9, 17, 3, 0, 0, 0, time.Local)
	if _, due := scanDue(last, now, 24*7, hourPtr(3)); due {
		t.Error("the preferred hour overrode the interval")
	}
}
