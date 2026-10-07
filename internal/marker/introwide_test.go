package marker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"testing"

	"lancast/internal/store"
)

/*
 * A shared opening broken by a sound of each episode's own.
 *
 * Futurama S7 E16: the opening is the same as its siblings' except for under a
 * second, about nine seconds in, which split every comparison into two pieces
 * too short to decide (2.1–8.9 and 9.7–16.4). A one-second bridge joins them.
 *
 * The season below is built so the stricter rules cannot decide the episodes
 * that matter: a 3-second cold open of their own, then the opening's first
 * half (6.5 s), 0.8 s unique to the episode, then the second half. The second
 * half is 7 s on every third episode and 6 s on the rest, so those episodes'
 * comparisons split between the halves and none of the stricter rules agrees.
 */
func TestAnOpeningBrokenByAShortSoundOfItsOwn(t *testing.T) {
	r := rand.New(rand.NewSource(23))
	first, second := noise(r, 6.5), noise(r, 7)
	heads := map[string][]float64{}
	ms := int64(1300 * 1000)
	var se store.Season
	const n = 12
	for i := 0; i < n; i++ {
		p := fmt.Sprintf("e%02d.mkv", i+1)
		h := noise(r, 3)
		h = append(h, first...)
		h = append(h, noise(r, 0.8)...)
		tail := second
		if i%3 != 0 {
			tail = second[:len(second)*6/7]
		}
		h = append(h, tail...)
		h = append(h, noise(r, 20)...)
		heads[p] = h
		se.Episodes = append(se.Episodes, store.Item{ID: int64(i + 1), Kind: "episode", Path: p, DurationMS: &ms})
	}
	w := NewWorker(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.headFn = func(_ context.Context, p string, _ int) ([]float64, error) { return heads[p], nil }
	w.tailAudioFn = func(context.Context, string, int) ([]float64, error) { return nil, fmt.Errorf("no tail") }
	w.tailFn = func(context.Context, string, float64) (string, error) { return "", nil }

	st := &seasonStore{}
	if err := w.examineSeason(context.Background(), st, se); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i += 3 {
		id := int64(i + 1)
		var intro *store.Marker
		for _, m := range st.saved[id] {
			if m.Kind == store.MarkerIntro {
				m := m
				intro = &m
			}
		}
		if intro == nil {
			t.Errorf("episode %d: no intro; the opening was never joined across its own sound", id)
			continue
		}
		start, end := float64(intro.StartMS)/1000, float64(*intro.EndMS)/1000
		if start < 2 || start > 4.5 || end < 12.5 {
			t.Errorf("episode %d: intro %.1f–%.1f s, want about 3–16", id, start, end)
		}
	}
}
