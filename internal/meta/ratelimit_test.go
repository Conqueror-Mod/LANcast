package meta

import (
	"context"
	"testing"
	"time"
)

func TestLimiterAllowsBurstThenThrottles(t *testing.T) {
	l := NewLimiter(10, 3)
	// A fixed clock so the test does not depend on wall time.
	now := time.Now()
	l.now = func() time.Time { return now }
	l.last = now

	for i := 0; i < 3; i++ {
		if d := l.reserve(); d != 0 {
			t.Fatalf("burst token %d was delayed by %v", i, d)
		}
	}
	if d := l.reserve(); d <= 0 {
		t.Error("the fourth request was not throttled")
	}
}

func TestLimiterRefills(t *testing.T) {
	l := NewLimiter(10, 1)
	now := time.Now()
	l.now = func() time.Time { return now }
	l.last = now

	if d := l.reserve(); d != 0 {
		t.Fatalf("first token delayed by %v", d)
	}
	if d := l.reserve(); d <= 0 {
		t.Fatal("second token was not throttled")
	}

	// 10/s means a token every 100ms.
	now = now.Add(150 * time.Millisecond)
	if d := l.reserve(); d != 0 {
		t.Errorf("token not refilled after 150ms: delayed %v", d)
	}
}

func TestLimiterWaitRespectsContext(t *testing.T) {
	l := NewLimiter(0.001, 1) // effectively one token, then a very long wait
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("first Wait: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := l.Wait(ctx); err == nil {
		t.Error("Wait ignored a cancelled context")
	}
}

func TestLimiterWaitSucceeds(t *testing.T) {
	l := NewLimiter(1000, 10)
	for i := 0; i < 10; i++ {
		if err := l.Wait(context.Background()); err != nil {
			t.Fatalf("Wait #%d: %v", i, err)
		}
	}
}

func TestLimiterDefaults(t *testing.T) {
	l := NewLimiter(0, 0)
	if l.rate <= 0 || l.capacity <= 0 {
		t.Errorf("zero arguments produced rate=%v capacity=%v", l.rate, l.capacity)
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	base, max := 100*time.Millisecond, time.Second

	// Full jitter means any single value is in [0, d], so compare ceilings
	// across many samples rather than individual draws.
	maxSeen := func(attempt, n int) time.Duration {
		var m time.Duration
		for i := 0; i < n; i++ {
			if d := Backoff(attempt, base, max); d > m {
				m = d
			}
		}
		return m
	}

	first, third := maxSeen(1, 200), maxSeen(3, 200)
	if third <= first {
		t.Errorf("backoff did not grow: attempt 1 max %v, attempt 3 max %v", first, third)
	}
	for i := 0; i < 500; i++ {
		if d := Backoff(10, base, max); d > max {
			t.Fatalf("backoff %v exceeded the cap %v", d, max)
		}
	}
	if d := Backoff(0, base, max); d < 0 {
		t.Error("attempt 0 produced a negative delay")
	}
}

// Without jitter, a library's worth of workers that hit a 429 together retry
// together and hit it again.
func TestBackoffIsJittered(t *testing.T) {
	seen := map[time.Duration]bool{}
	for i := 0; i < 50; i++ {
		seen[Backoff(5, 100*time.Millisecond, time.Second)] = true
	}
	if len(seen) < 10 {
		t.Errorf("only %d distinct delays in 50 draws — jitter is not working", len(seen))
	}
}
