package config

import (
	"testing"
	"time"
)

// The watched rule, which is the one setting here that can destroy state a
// person cannot reconstruct: mark something watched wrongly and their place in
// it is gone.
func TestWatchedThreshold(t *testing.T) {
	s := Defaults() // 90%
	cases := []struct {
		name             string
		position, length int64
		want             bool
	}{
		{"most of the way through is not finished", 5000, 10000, false},
		{"exactly at the threshold is finished", 9000, 10000, true},
		{"past the threshold is finished", 9600, 10000, true},
		{"the very start is not", 1, 10000, false},
		// An unprobed file has no duration, and a percentage of an unknown
		// length is not a fact about anything. Guessing here would mark things
		// watched that were never played.
		{"unknown duration is never watched", 9999, 0, false},
		{"nothing played is never watched", 0, 10000, false},
	}
	for _, tc := range cases {
		if got := s.Watched(tc.position, tc.length); got != tc.want {
			t.Errorf("%s: Watched(%d, %d) = %v, want %v",
				tc.name, tc.position, tc.length, got, tc.want)
		}
	}
}

func TestContinueCutoff(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)

	s := Defaults() // 16 weeks
	got := s.ContinueCutoff(now)
	want := now.AddDate(0, 0, -7*16).Unix()
	if got != want {
		t.Errorf("cutoff = %d, want %d", got, want)
	}

	// Zero weeks means nothing ever expires, which has to be a distinct answer
	// from "expires now" — the difference between a shelf that keeps everything
	// and one that is always empty.
	s.ContinueWeeks = 0
	if got := s.ContinueCutoff(now); got != 0 {
		t.Errorf("cutoff with no window = %d, want 0", got)
	}
}

// A hand-edited config must not be able to make a rule that destroys state.
// The file is editable, so this is the floor under it — not a substitute for
// the API rejecting bad input, which it also does.
func TestClampRepairsAHandEditedFile(t *testing.T) {
	s := Settings{WatchedThreshold: 0, ContinueWeeks: -3, ContinueLimit: 0,
		ScanIntervalHours: -1}
	clamp(&s)

	d := Defaults()
	// Zero would mark everything watched the instant it started playing: the
	// setting failing open in the most destructive direction available to it.
	if s.WatchedThreshold != d.WatchedThreshold {
		t.Errorf("threshold = %d, want the default %d", s.WatchedThreshold, d.WatchedThreshold)
	}
	if s.ContinueWeeks != d.ContinueWeeks {
		t.Errorf("weeks = %d, want the default %d", s.ContinueWeeks, d.ContinueWeeks)
	}
	if s.ContinueLimit != d.ContinueLimit {
		t.Errorf("limit = %d, want the default %d", s.ContinueLimit, d.ContinueLimit)
	}
	if s.ScanIntervalHours != 0 {
		t.Errorf("interval = %d, want 0 (off)", s.ScanIntervalHours)
	}

	// Zero is a legitimate answer for these two and must survive clamping:
	// "never expire" and "no periodic scan".
	ok := Settings{WatchedThreshold: 75, ContinueWeeks: 0, ContinueLimit: 5,
		ScanIntervalHours: 0}
	clamp(&ok)
	if ok.ContinueWeeks != 0 || ok.ScanIntervalHours != 0 || ok.WatchedThreshold != 75 {
		t.Errorf("clamp changed valid settings: %+v", ok)
	}
}
