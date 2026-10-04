package marker

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"lancast/internal/store"
)

/*
 * The worker puts every candidate to the gate, and a pass stopped while the
 * gate is reading frames records nothing.
 */

type savedMarkers struct {
	mu    sync.Mutex
	saves map[int64][]store.Marker
}

func (s *savedMarkers) PendingMarkers(context.Context, int) ([]store.Item, error) { return nil, nil }
func (s *savedMarkers) PendingMarkersCount(context.Context) (int, error)          { return 0, nil }
func (s *savedMarkers) SaveMarkers(_ context.Context, id int64, _ []string, m []store.Marker) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saves == nil {
		s.saves = map[int64][]store.Marker{}
	}
	s.saves[id] = m
	return nil
}

// A 6,000s film with a fade at 91.6% and its credits at 95.2%.
const twoCandidates = "black_start:996.0 black_end:1003.0\nblack_start:1212.0 black_end:1220.0\n"

func gatedWorker(st Store, shape func(ctx context.Context, at float64) Shape) *Worker {
	w := NewWorker(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// The tail scan begins at 75% of 6,000s; blackdetect reports from there.
	w.tailFn = func(context.Context, string, float64) (string, error) { return twoCandidates, nil }
	w.shapeFn = func(ctx context.Context, _ string, at float64) Shape { return shape(ctx, at) }
	return w
}

func film() store.Item {
	ms := int64(6_000_000)
	return store.Item{ID: 7, Path: "film.mkv", DurationMS: &ms}
}

func TestWorkerSkipsAFadeTheGateTurnsAway(t *testing.T) {
	st := &savedMarkers{}
	var asked []float64
	w := gatedWorker(st, func(_ context.Context, at float64) Shape {
		asked = append(asked, at)
		if at >= 5712 {
			return Shape{PBlack: 92, Edge: 10}
		}
		return Shape{PBlack: 50, Edge: 8} // a dim scene
	})
	w.examine(context.Background(), film())

	got := st.saves[7]
	if len(got) != 1 || got[0].StartMS != 5_712_000 {
		t.Fatalf("saved %+v, want one credits marker at 5712s — the fade at 5496s is a scene", got)
	}
	if got[0].Source != Source {
		t.Errorf("Source = %q, want %q so the player can tell a gated marker apart", got[0].Source, Source)
	}
	// Five frames for the rejected fade, then five for the accepted credits.
	if len(asked) != 10 || asked[0] != 5511 || asked[5] != 5727 {
		t.Errorf("frames read at %v, want +15..+90 after 5496 and then after 5712", asked)
	}
}

func TestWorkerStampsAnAbstentionWhenTheGateAcceptsNothing(t *testing.T) {
	st := &savedMarkers{}
	w := gatedWorker(st, func(context.Context, float64) Shape { return Shape{PBlack: 40, Edge: 9} })
	w.examine(context.Background(), film())

	got, stamped := st.saves[7]
	if !stamped {
		t.Fatal("an honest abstention was not stamped; it would be decoded again on every pass")
	}
	if len(got) != 0 {
		t.Errorf("saved %+v, want no marker", got)
	}
}

/*
 * Shutdown arrives while frames are being read. Every frame after it fails,
 * the gate counts failures against, and the result is indistinguishable from
 * an abstention — which must not be stamped, or a restart retires the film.
 */
func TestWorkerRecordsNothingWhenStoppedDuringTheGate(t *testing.T) {
	st := &savedMarkers{}
	ctx, cancel := context.WithCancel(context.Background())
	w := gatedWorker(st, func(ctx context.Context, _ float64) Shape {
		cancel()
		if ctx.Err() != nil {
			return Shape{} // what a killed ffmpeg gives back
		}
		return Shape{PBlack: 92, Edge: 10}
	})
	w.examine(ctx, film())

	if got, stamped := st.saves[7]; stamped {
		t.Errorf("saved %+v for a film whose examination was cut short", got)
	}
}
