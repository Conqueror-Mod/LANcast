package meta

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// Limiter is a token bucket. Being a considerate API citizen is not optional
// when the key is free, so every provider goes through one.
type Limiter struct {
	mu       sync.Mutex
	tokens   float64
	capacity float64
	rate     float64 // tokens per second
	last     time.Time

	// now is swappable so tests do not have to sleep.
	now func() time.Time
}

// NewLimiter builds a bucket allowing ratePerSec sustained requests with a
// burst of capacity.
func NewLimiter(ratePerSec float64, capacity int) *Limiter {
	if ratePerSec <= 0 {
		ratePerSec = 5
	}
	if capacity <= 0 {
		capacity = int(ratePerSec)
	}
	return &Limiter{
		tokens:   float64(capacity),
		capacity: float64(capacity),
		rate:     ratePerSec,
		now:      time.Now,
		last:     time.Now(),
	}
}

// Wait blocks until a token is available or ctx is done.
func (l *Limiter) Wait(ctx context.Context) error {
	for {
		wait := l.reserve()
		if wait <= 0 {
			return nil
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// reserve takes a token if one is available, else reports how long to wait.
func (l *Limiter) reserve() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if elapsed := now.Sub(l.last); elapsed > 0 {
		l.tokens = minF(l.capacity, l.tokens+elapsed.Seconds()*l.rate)
		l.last = now
	}
	if l.tokens >= 1 {
		l.tokens--
		return 0
	}
	needed := 1 - l.tokens
	return time.Duration(needed / l.rate * float64(time.Second))
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// Backoff computes the delay before retry number attempt (1-based), with full
// jitter. Jitter matters: without it, a whole library's worth of workers that
// hit a 429 together retry together and hit it again.
func Backoff(attempt int, base, max time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := base
	for i := 1; i < attempt && d < max; i++ {
		d *= 2
	}
	if d > max {
		d = max
	}
	return time.Duration(rand.Int63n(int64(d) + 1))
}
