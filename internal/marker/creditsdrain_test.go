package marker

import (
	"context"
	"io"
	"log/slog"
	"sort"
	"sync"
	"testing"

	"lancast/internal/store"
)

/*
 * The credits pass finishes the queue, not one batch of it.
 *
 * The intro pass had the same shape and stalled at five seasons (v0.9.17). This
 * one matters now for the same reason: revision 46 puts every episode an intro
 * pass wrongly retired back on this queue — 994 of them on a real library — and
 * a pass of twenty-five that only starts at startup or after a scan would take
 * forty restarts to work through them.
 */

type creditsQueue struct {
	mu      sync.Mutex
	pending map[int64]store.Item
	queries int
}

func newCreditsQueue(n int) *creditsQueue {
	q := &creditsQueue{pending: map[int64]store.Item{}}
	ms := int64(5_400_000)
	for i := 1; i <= n; i++ {
		q.pending[int64(i)] = store.Item{ID: int64(i), Path: "f.mkv", DurationMS: &ms}
	}
	return q
}

func (q *creditsQueue) PendingMarkers(_ context.Context, limit int) ([]store.Item, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.queries++
	ids := make([]int64, 0, len(q.pending))
	for id := range q.pending {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var out []store.Item
	for _, id := range ids {
		if len(out) == limit {
			break
		}
		out = append(out, q.pending[id])
	}
	return out, nil
}

func (q *creditsQueue) PendingMarkersCount(context.Context) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending), nil
}

func (q *creditsQueue) SaveMarkers(context.Context, int64, []string, []store.Marker) error {
	return nil
}

func (q *creditsQueue) done(id int64) {
	q.mu.Lock()
	delete(q.pending, id)
	q.mu.Unlock()
}

func (q *creditsQueue) left() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

func drainCreditsWorker(q *creditsQueue) *Worker {
	return NewWorker(q, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestTheCreditsPassFinishesEveryPendingFile(t *testing.T) {
	q := newCreditsQueue(200)
	w := drainCreditsWorker(q)
	var mu sync.Mutex
	examined := 0
	w.examineFn = func(_ context.Context, it store.Item) {
		mu.Lock()
		examined++
		mu.Unlock()
		q.done(it.ID)
	}

	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if examined != 200 || q.left() != 0 {
		t.Errorf("examined %d files, %d left pending; want all 200 in one pass "+
			"(the batch of %d is a query size, not a stopping point)",
			examined, q.left(), w.BatchSize)
	}
}

// A file that will not leave the queue — a save that keeps failing — must end
// the pass rather than be fetched for ever. It is tried again next pass.
func TestAFileThatNeverLeavesTheQueueDoesNotKeepThePassRunning(t *testing.T) {
	q := newCreditsQueue(60)
	w := drainCreditsWorker(q)
	w.examineFn = func(_ context.Context, it store.Item) {
		if it.ID == 7 {
			return // never leaves the queue
		}
		q.done(it.ID)
	}

	done := make(chan error, 1)
	go func() { done <- w.Run(context.Background()) }()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if q.left() != 1 {
		t.Errorf("%d files left pending, want only the stuck one", q.left())
	}
	if q.queries > 6 {
		t.Errorf("queried %d times for 60 files in batches of %d; the stuck file is being refetched",
			q.queries, w.BatchSize)
	}
}

// Turning detection off stops the pass at the next file, loop or not. That is
// what the Enabled comment promises, and a loop must not weaken it.
func TestTurningCreditsDetectionOffStopsThePass(t *testing.T) {
	q := newCreditsQueue(200)
	w := drainCreditsWorker(q)
	on := true
	w.Enabled = func() bool { return on }
	var mu sync.Mutex
	examined := 0
	w.examineFn = func(_ context.Context, it store.Item) {
		mu.Lock()
		examined++
		if examined == 10 {
			on = false
		}
		mu.Unlock()
		q.done(it.ID)
	}

	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if examined != 10 {
		t.Errorf("examined %d files after detection was switched off at 10", examined)
	}
}
