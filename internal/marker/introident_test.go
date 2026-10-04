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
 * An ident at the start is not an intro, and the intro may be behind it.
 */

func TestIsIdentNeedsBothShortAndAtTheStart(t *testing.T) {
	for _, c := range []struct {
		name       string
		start, end float64
		want       bool
	}{
		{"Lanterns: DC Studios and HBO at 0:00", 0, 8.7, true},
		{"Futurama: titles at 0:00, but 29 s of them", 0, 29.1, false},
		{"Futurama, later seasons: 17 s at 0:00", 0, 17.2, false},
		{"The League: a 4 s title card, a minute in", 41.5, 45.9, false},
		{"Silicon Valley: 12 s, three minutes in", 145.6, 158.0, false},
	} {
		in := Intro{Found: true, StartSec: c.start, EndSec: c.end}
		if got := in.IsIdent(); got != c.want {
			t.Errorf("%s: IsIdent = %v, want %v", c.name, got, c.want)
		}
	}
	if (Intro{}).IsIdent() {
		t.Error("no intro at all is not an ident")
	}
}

/*
 * Every episode opens on the same 10 s of logos, then has its own cold open,
 * then a 9 s title sequence at a different point in each. The logos align at
 * the same offset in every pair and win the vote; the titles are what should
 * be marked.
 */
func identSeason() (*Worker, store.Season, []float64) {
	r := rand.New(rand.NewSource(7))
	ident := noise(r, 10)
	titles := noise(r, 9)
	at := []float64{30, 42, 55}
	heads := map[string][]float64{}
	ms := int64(1300 * 1000)
	var se store.Season
	for i, t := range at {
		p := fmt.Sprintf("e%d.mkv", i+1)
		h := append([]float64{}, ident...)
		h = append(h, noise(r, t-10)...)
		h = append(h, titles...)
		h = append(h, noise(r, 15)...)
		heads[p] = h
		se.Episodes = append(se.Episodes, store.Item{ID: int64(i + 1), Kind: "episode", Path: p, DurationMS: &ms})
	}
	w := NewWorker(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.headFn = func(_ context.Context, p string, _ int) ([]float64, error) { return heads[p], nil }
	w.tailAudioFn = func(context.Context, string, int) ([]float64, error) { return nil, fmt.Errorf("no tail") }
	w.tailFn = func(context.Context, string, float64) (string, error) { return "", nil }
	return w, se, at
}

func TestSeasonPassLooksPastAnIdent(t *testing.T) {
	w, se, at := identSeason()
	st := &seasonStore{}
	if err := w.examineSeason(context.Background(), st, se); err != nil {
		t.Fatal(err)
	}
	for i, ep := range se.Episodes {
		var intro *store.Marker
		for _, m := range st.saved[ep.ID] {
			if m.Kind == store.MarkerIntro {
				m := m
				intro = &m
			}
		}
		if intro == nil {
			t.Errorf("episode %d: no intro; the titles behind the ident were not found", ep.ID)
			continue
		}
		start := float64(intro.StartMS) / 1000
		if start < at[i]-1.5 || start > at[i]+1.5 {
			t.Errorf("episode %d: intro starts at %.1fs, want the titles at %.0fs", ep.ID, start, at[i])
		}
	}
}
