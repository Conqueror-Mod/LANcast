package applog

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

/*
 * Quiet says a thing once, then counts.
 *
 * The log's worst failure is not silence but repetition: a paired server that
 * was switched off wrote 3,263 identical lines in five days, most of
 * lancastd.log, and pushed out of the file the history somebody would actually
 * have wanted. That was fixed for peers by logging state changes
 * (internal/api/peerhealth.go), but the next noisy path, a tab with a dead
 * cookie getting 401 after 401, or a broken client retrying, has to be noticed
 * and fixed in the same way, one path at a time.
 *
 * Quiet makes the quiet version the easy one. The first line for a key is
 * written. Repeats within the window are dropped to Debug and counted. The
 * first repeat after the window is written again, saying how many were
 * swallowed (`repeats=N`), so a reader learns both that it is still happening
 * and how often, from one line per window instead of one per occurrence.
 *
 * A key is whatever makes two lines "the same": a route and a client, a peer,
 * an item. It is the caller's choice, and the one thing to get right.
 */
type Quiet struct {
	window time.Duration
	now    func() time.Time

	mu   sync.Mutex
	seen map[string]*quietEntry
}

type quietEntry struct {
	written    time.Time // when a line for this key was last written
	suppressed int       // repeats dropped since then
}

// quietMaxKeys bounds memory. Past it, keys whose window has long closed are
// dropped; a key that comes back after that is simply news again.
const quietMaxKeys = 1024

// NewQuiet returns a Quiet that writes each key at most once per window.
func NewQuiet(window time.Duration) *Quiet {
	return &Quiet{window: window, now: time.Now, seen: map[string]*quietEntry{}}
}

// Allow reports whether a line for key should be written now and, if so, how
// many were suppressed since the last one. A false answer has been counted.
func (q *Quiet) Allow(key string) (write bool, suppressed int) {
	now := q.now()
	q.mu.Lock()
	defer q.mu.Unlock()

	e, ok := q.seen[key]
	if ok && now.Sub(e.written) < q.window {
		e.suppressed++
		return false, 0
	}
	if !ok {
		if len(q.seen) >= quietMaxKeys {
			q.prune(now)
		}
		e = &quietEntry{}
		q.seen[key] = e
	}
	suppressed = e.suppressed
	e.written, e.suppressed = now, 0
	return true, suppressed
}

// prune drops keys whose window closed long enough ago that nothing is
// pending for them. q.mu must be held.
func (q *Quiet) prune(now time.Time) {
	for k, e := range q.seen {
		if now.Sub(e.written) >= q.window && e.suppressed == 0 {
			delete(q.seen, k)
		}
	}
}

// Log writes msg at level if key is due, adding repeats=N when earlier ones
// were suppressed, and otherwise writes it at Debug so a person who turned
// Debug on still sees every occurrence.
func (q *Quiet) Log(l *slog.Logger, level slog.Level, key, msg string, args ...any) {
	write, suppressed := q.Allow(key)
	if !write {
		l.Log(context.Background(), slog.LevelDebug, msg, args...)
		return
	}
	if suppressed > 0 {
		args = append(args, "repeats", suppressed)
	}
	l.Log(context.Background(), level, msg, args...)
}
