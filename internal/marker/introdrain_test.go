package marker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"testing"

	"lancast/internal/store"
)

/*
 * The intro pass finishes the queue, not one batch of it.
 *
 * v0.9.16 reset every season for re-examination. The startup pass took five
 * and returned, and with no library scan to start another the other sixty
 * seasons sat pending.
 */

// seasonQueue is an IntroStore holding seasons until they are stamped.
type seasonQueue struct {
	pending map[[2]int64]store.Season
	queries int
}

func newSeasonQueue(n int) *seasonQueue {
	q := &seasonQueue{pending: map[[2]int64]store.Season{}}
	for i := 1; i <= n; i++ {
		se := store.Season{ShowID: int64(i), ShowName: "Show", Season: 1,
			Episodes: []store.Item{{ID: int64(i*100 + 1)}, {ID: int64(i*100 + 2)}}}
		q.pending[[2]int64{se.ShowID, 1}] = se
	}
	return q
}

func (q *seasonQueue) PendingIntroSeasons(_ context.Context, _, limit int) ([]store.Season, error) {
	q.queries++
	keys := make([][2]int64, 0, len(q.pending))
	for k := range q.pending {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i][0] < keys[j][0] })
	var out []store.Season
	for _, k := range keys {
		if len(out) == limit {
			break
		}
		out = append(out, q.pending[k])
	}
	return out, nil
}

func (q *seasonQueue) SaveMarkers(context.Context, int64, []string, []store.Marker) error {
	return nil
}
func (q *seasonQueue) MarkIntrosExamined(context.Context, []int64, int64) error { return nil }
func (q *seasonQueue) PendingMarkers(context.Context, int) ([]store.Item, error) {
	return nil, nil
}
func (q *seasonQueue) PendingMarkersCount(context.Context) (int, error) { return 0, nil }

func drainWorker(q *seasonQueue) *Worker {
	return NewWorker(q, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestTheIntroPassFinishesEveryPendingSeason(t *testing.T) {
	q := newSeasonQueue(65) // the real library's count
	w := drainWorker(q)
	examined := 0
	w.examineSeasonFn = func(_ context.Context, _ IntroStore, se store.Season) error {
		examined++
		delete(q.pending, [2]int64{se.ShowID, int64(se.Season)})
		return nil
	}

	if err := w.RunIntros(context.Background()); err != nil {
		t.Fatal(err)
	}
	if examined != 65 || len(q.pending) != 0 {
		t.Errorf("examined %d seasons, %d left pending; want all 65 in one pass "+
			"(the batch of %d is a query size, not a stopping point)",
			examined, len(q.pending), introSeasonBatch)
	}
}

// A season that fails stays pending. The pass must end rather than retry it
// for ever, and must still get through everything else.
func TestAFailingSeasonDoesNotKeepThePassRunning(t *testing.T) {
	q := newSeasonQueue(12)
	w := drainWorker(q)
	broken := [2]int64{3, 1}
	w.examineSeasonFn = func(_ context.Context, _ IntroStore, se store.Season) error {
		key := [2]int64{se.ShowID, int64(se.Season)}
		if key == broken {
			return errors.New("unreadable")
		}
		delete(q.pending, key)
		return nil
	}

	done := make(chan error, 1)
	go func() { done <- w.RunIntros(context.Background()) }()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(q.pending) != 1 {
		t.Errorf("%d seasons left pending, want only the broken one", len(q.pending))
	}
	if q.queries > 5 {
		t.Errorf("queried %d times for 12 seasons; the failing one is being retried", q.queries)
	}
}

// Turning detection off stops the pass at the next season, loop or not.
func TestTurningDetectionOffStopsThePass(t *testing.T) {
	q := newSeasonQueue(20)
	w := drainWorker(q)
	on := true
	w.Enabled = func() bool { return on }
	examined := 0
	w.examineSeasonFn = func(_ context.Context, _ IntroStore, se store.Season) error {
		examined++
		delete(q.pending, [2]int64{se.ShowID, int64(se.Season)})
		if examined == 7 {
			on = false
		}
		return nil
	}

	if err := w.RunIntros(context.Background()); err != nil {
		t.Fatal(err)
	}
	if examined != 7 {
		t.Errorf("examined %d seasons after detection was switched off at 7", examined)
	}
}
