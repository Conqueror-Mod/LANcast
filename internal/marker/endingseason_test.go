package marker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"sync"
	"testing"

	"lancast/internal/store"
)

/*
 * The season pass decides an episode's credits from the ending its siblings
 * share, and writes them with the intro.
 *
 * Synthetic audio: every episode's tail is its own noise followed by the same
 * 50 seconds of shared noise, which fingerprints exactly as a closing theme
 * would — the same samples in every file, and nothing else in common.
 */

type seasonStore struct {
	mu    sync.Mutex
	kinds map[int64][]string
	saved map[int64][]store.Marker
}

func (s *seasonStore) PendingIntroSeasons(context.Context, int, int) ([]store.Season, error) {
	return nil, nil
}
func (s *seasonStore) SaveMarkers(_ context.Context, id int64, kinds []string, m []store.Marker) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.kinds == nil {
		s.kinds, s.saved = map[int64][]string{}, map[int64][]store.Marker{}
	}
	s.kinds[id], s.saved[id] = kinds, m
	return nil
}
func (s *seasonStore) MarkIntrosExamined(context.Context, []int64, int64) error { return nil }

func noise(r *rand.Rand, secs float64) []float64 {
	out := make([]float64, int(secs*SampleRate))
	for i := range out {
		out[i] = r.Float64()*0.6 - 0.3
	}
	return out
}

const seasonDur = 1300.0 // seconds; the shared ending begins 50 s from the end

func seasonWorker(blackFor func(path string) string) (*Worker, store.Season) {
	r := rand.New(rand.NewSource(54))
	shared := noise(r, 50)
	heads := map[string][]float64{}
	tails := map[string][]float64{}
	ms := int64(seasonDur * 1000)
	var se store.Season
	for i := 1; i <= 3; i++ {
		p := fmt.Sprintf("e%d.mkv", i)
		heads[p] = noise(r, 20)
		tails[p] = append(noise(r, 70), shared...)
		se.Episodes = append(se.Episodes, store.Item{ID: int64(i), Kind: "episode", Path: p, DurationMS: &ms})
	}
	w := NewWorker(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.headFn = func(_ context.Context, p string, _ int) ([]float64, error) { return heads[p], nil }
	w.tailAudioFn = func(_ context.Context, p string, _ int) ([]float64, error) { return tails[p], nil }
	w.tailFn = func(_ context.Context, p string, _ float64) (string, error) { return blackFor(p), nil }
	w.shapeFn = func(context.Context, string, float64) Shape { return Shape{} }
	return w, se
}

func creditsOf(t *testing.T, st *seasonStore, id int64) store.Marker {
	t.Helper()
	for _, m := range st.saved[id] {
		if m.Kind == store.MarkerCredits {
			return m
		}
	}
	t.Fatalf("episode %d: no credits marker; saved %+v with kinds %v", id, st.saved[id], st.kinds[id])
	return store.Marker{}
}

// No black run at all — Black Books — and the shared ending decides.
func TestSeasonPassFindsCreditsFromTheSharedEnding(t *testing.T) {
	w, se := seasonWorker(func(string) string { return "" })
	st := &seasonStore{}
	if err := w.examineSeason(context.Background(), st, se); err != nil {
		t.Fatal(err)
	}
	for _, ep := range se.Episodes {
		m := creditsOf(t, st, ep.ID)
		at := float64(m.StartMS) / 1000
		if m.Source != SourceEnding || at < seasonDur-52 || at > seasonDur-48 {
			t.Errorf("episode %d: credits at %.1fs from %q, want ~%.0fs from %q",
				ep.ID, at, m.Source, seasonDur-50, SourceEnding)
		}
	}
}

/*
 * A black run at the ending — the two agree — and the frame-exact black run is
 * written. The scan is told where it began: 87% of the episode, and the
 * blackdetect times are relative to it.
 */
func TestSeasonPassPrefersAnAgreeingBlackRun(t *testing.T) {
	from := seasonDur * EpisodeScanFrom
	w, se := seasonWorker(func(string) string {
		return fmt.Sprintf("black_start:%.2f black_end:%.2f\n", seasonDur-55-from, seasonDur-49-from)
	})
	st := &seasonStore{}
	if err := w.examineSeason(context.Background(), st, se); err != nil {
		t.Fatal(err)
	}
	m := creditsOf(t, st, 1)
	if m.Source != SourceUngated || m.StartMS != int64((seasonDur-55)*1000) {
		t.Errorf("got %+v, want the black run at %.0fs", m, seasonDur-55)
	}
}

// Writing credits is what keeps the per-file pass off the episode: both kinds
// go in one call, and SaveMarkers stamps markers_at when credits is among them.
func TestSeasonPassWritesCreditsWithTheIntro(t *testing.T) {
	w, se := seasonWorker(func(string) string { return "" })
	st := &seasonStore{}
	if err := w.examineSeason(context.Background(), st, se); err != nil {
		t.Fatal(err)
	}
	kinds := st.kinds[1]
	if len(kinds) != 2 || kinds[0] != store.MarkerIntro || kinds[1] != store.MarkerCredits {
		t.Errorf("kinds = %v, want [intro credits]", kinds)
	}
}

// A file whose black scan fails keeps its credits: the call is not
// authoritative about a kind it could not examine.
func TestSeasonPassLeavesCreditsAloneWhenTheScanFails(t *testing.T) {
	w, se := seasonWorker(func(string) string { return "" })
	w.tailFn = func(context.Context, string, float64) (string, error) { return "", fmt.Errorf("unreadable") }
	st := &seasonStore{}
	if err := w.examineSeason(context.Background(), st, se); err != nil {
		t.Fatal(err)
	}
	if kinds := st.kinds[1]; len(kinds) != 1 || kinds[0] != store.MarkerIntro {
		t.Errorf("kinds = %v, want [intro] alone", kinds)
	}
}
