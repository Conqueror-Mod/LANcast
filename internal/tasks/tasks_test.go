package tasks

import (
	"errors"
	"testing"
	"time"
)

func clock(t0 time.Time) (func() time.Time, func(time.Duration)) {
	now := t0
	return func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) }
}

func TestTheBoardReportsWhatAJobLastDid(t *testing.T) {
	b := New()
	now, advance := clock(time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC))
	b.now = now
	ran := 0
	b.Add(Spec{ID: "probe", Name: "Read media files", Trigger: AfterScan, Run: func() { ran++ }})

	got := b.List()[0]
	if got.Running || got.LastStarted != nil || got.LastEnded != nil {
		t.Fatalf("a job that never ran reports %+v", got)
	}

	b.Started("probe")
	if !b.List()[0].Running {
		t.Fatal("a started job is not running")
	}
	advance(90 * time.Second)
	b.Finished("probe", errors.New("ffprobe not found"))
	got = b.List()[0]
	if got.Running || got.LastError != "ffprobe not found" || got.LastEnded.Sub(*got.LastStarted) != 90*time.Second {
		t.Fatalf("after a failed run: %+v", got)
	}

	// The next run clears the error.
	if err := b.Track("probe", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if got = b.List()[0]; got.LastError != "" {
		t.Fatalf("a successful run kept the old error: %+v", got)
	}

	if !b.RunNow("probe") || ran != 1 {
		t.Fatalf("RunNow did not run the job (ran %d)", ran)
	}
	if b.RunNow("nothing") {
		t.Fatal("RunNow claimed to run a job the board does not know")
	}
}

// An interval job says when it is next due, from its period as set now and
// its last run, including one the server kept durably across a restart.
func TestAnIntervalJobSaysWhenItIsNextDue(t *testing.T) {
	b := New()
	t0 := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	now, _ := clock(t0.Add(5 * time.Hour))
	b.now = now
	every := 24 * time.Hour
	b.Add(Spec{
		ID: "prune", Name: "Maintenance", Trigger: Interval,
		Every: func() time.Duration { return every },
		Last:  func() time.Time { return t0 },
	})
	got := b.List()[0]
	if got.EveryHours != 24 || got.LastEnded == nil || !got.LastEnded.Equal(t0) {
		t.Fatalf("durable last run not reported: %+v", got)
	}
	if got.NextAt == nil || !got.NextAt.Equal(t0.Add(24*time.Hour)) {
		t.Fatalf("next due = %v, want %v", got.NextAt, t0.Add(24*time.Hour))
	}

	// The period is read each time: a changed setting changes the answer.
	every = 6 * time.Hour
	if got = b.List()[0]; !got.NextAt.Equal(t0.Add(6 * time.Hour)) {
		t.Fatalf("after the period changed, next due = %v", got.NextAt)
	}
}

func TestADisabledJobHasNoNextRun(t *testing.T) {
	b := New()
	b.Add(Spec{
		ID: "update", Trigger: Interval,
		Every:   func() time.Duration { return 24 * time.Hour },
		Last:    func() time.Time { return time.Now() },
		Enabled: func() bool { return false },
	})
	if got := b.List()[0]; got.Enabled || got.NextAt != nil {
		t.Fatalf("a switched-off job reports %+v", got)
	}
}

func TestReportingToANilBoardIsHarmless(t *testing.T) {
	var b *Board
	b.Started("x")
	b.Finished("x", nil)
}
