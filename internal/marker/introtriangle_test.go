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
 * Two openings in one season, and the third side of a triangle.
 *
 * Futurama S8: three episodes carry a 29-second opening and the rest a shorter
 * one. Each of the three is compared with four episodes two apart, meets one of
 * the others at most, and gets no majority.
 */

func sib(peer int, start, end, peerStart float64) SiblingMatch {
	return SiblingMatch{Peer: peer, Candidate: Candidate{StartSec: start, EndSec: end}, PeerStartSec: peerStart}
}

func TestATriangleOfLongRunsIsAnIntro(t *testing.T) {
	sibs := []SiblingMatch{sib(5, 0, 29.1, 0), sib(6, 0.2, 29.0, 0), sib(3, 7, 14.7, 7), sib(9, 0, 0, 0)}
	in := IntroFromTriangle(sibs, true, func(p, q int) (float64, float64, float64) {
		if p == 5 && q == 6 {
			return 0, 0, 29
		}
		return 0, 0, 0
	})
	if !in.Found || in.StartSec != 0.2 || in.EndSec != 29.0 {
		t.Errorf("got %+v, want 0.2–29.0: the stretch both sides cover", in)
	}
}

// Two siblings sharing the stretch with this episode and not with each other
// are two files that happen to agree, which is what the majority rule exists
// to refuse.
func TestTwoSidesAreNotATriangle(t *testing.T) {
	sibs := []SiblingMatch{sib(5, 0, 29, 0), sib(6, 0, 29, 0)}
	short := func(p, q int) (float64, float64, float64) { return 0, 0, 6 }
	if in := IntroFromTriangle(sibs, true, short); in.Found {
		t.Errorf("a short third side was accepted: %+v", in)
	}
	elsewhere := func(p, q int) (float64, float64, float64) { return 300, 300, 29 }
	if in := IntroFromTriangle(sibs, true, elsewhere); in.Found {
		t.Errorf("a third side somewhere else in the siblings was accepted: %+v", in)
	}
}

// Sides that begin in different places in this episode do not describe one
// stretch.
func TestSidesMustBeginTogether(t *testing.T) {
	sibs := []SiblingMatch{sib(5, 0, 29, 0), sib(6, 8, 29, 8)}
	if in := IntroFromTriangle(sibs, true, func(p, q int) (float64, float64, float64) { return 0, 8, 29 }); in.Found {
		t.Errorf("sides starting 8 s apart were accepted: %+v", in)
	}
}

// A network ident three episodes share is still an ident.
func TestATriangleOnAnIdentIsRefused(t *testing.T) {
	sibs := []SiblingMatch{sib(5, 0, 13, 0), sib(6, 0, 13, 0)}
	if in := IntroFromTriangle(sibs, true, func(p, q int) (float64, float64, float64) { return 0, 0, 13 }); in.Found {
		t.Errorf("a 13-second run at 0:00 was accepted: %+v", in)
	}
}

/*
 * The season pass, on a season shaped like Futurama S8: thirteen episodes,
 * a long opening on the second, sixth and seventh and a short one on the
 * rest, each opening at 0:00 followed by the episode's own audio.
 */
func TestASeasonWithTwoOpeningsFindsBoth(t *testing.T) {
	r := rand.New(rand.NewSource(17))
	long, short := noise(r, 29), noise(r, 17)
	isLong := map[int]bool{1: true, 5: true, 6: true}
	heads := map[string][]float64{}
	ms := int64(1300 * 1000)
	var se store.Season
	for i := 0; i < 13; i++ {
		p := fmt.Sprintf("e%02d.mkv", i+1)
		open := short
		if isLong[i] {
			open = long
		}
		h := append([]float64{}, open...)
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
	for i, ep := range se.Episodes {
		var intro *store.Marker
		for _, m := range st.saved[ep.ID] {
			if m.Kind == store.MarkerIntro {
				m := m
				intro = &m
			}
		}
		want := 17.0
		if isLong[i] {
			want = 29
		}
		if intro == nil {
			t.Errorf("episode %d: no intro", ep.ID)
			continue
		}
		end := float64(*intro.EndMS) / 1000
		if intro.StartMS > 1500 || end < want-1.5 || end > want+1.5 {
			t.Errorf("episode %d: intro %.1f–%.1f s, want 0–%.0f", ep.ID, float64(intro.StartMS)/1000, end, want)
		}
	}
}

// A triangle at 0:00 is refused in a season whose own intros start later: the
// League S4 opens several files on the same 17 seconds of FX promos.
func TestATriangleAtTheStartNeedsASeasonThatStartsThere(t *testing.T) {
	sibs := []SiblingMatch{sib(5, 0, 17, 0), sib(6, 0, 17, 0)}
	mutual := func(p, q int) (float64, float64, float64) { return 0, 0, 17 }
	if in := IntroFromTriangle(sibs, false, mutual); in.Found {
		t.Errorf("promos at 0:00 accepted in a season whose titles start later: %+v", in)
	}
	if in := IntroFromTriangle(sibs, true, mutual); !in.Found {
		t.Error("a 17-second opening at 0:00 refused in a season whose titles open every episode")
	}
	// Later in the file the season's habit does not matter.
	later := []SiblingMatch{sib(5, 60, 80, 70), sib(6, 60, 80, 90)}
	if in := IntroFromTriangle(later, false, func(p, q int) (float64, float64, float64) { return 70, 90, 20 }); !in.Found {
		t.Error("a triangle a minute in was refused")
	}
}

func TestOpensAtStartIsWhereMostIntrosBegin(t *testing.T) {
	at := func(s ...float64) []Intro {
		var out []Intro
		for _, v := range s {
			out = append(out, Intro{Found: true, StartSec: v, EndSec: v + 17})
		}
		return append(out, Intro{})
	}
	if !opensAtStart(at(0, 0, 0.1, 80)) {
		t.Error("a season opening on its titles was not recognised")
	}
	if opensAtStart(at(88, 62, 200, 0)) {
		t.Error("a season of cards a minute in read as opening at 0:00")
	}
	if opensAtStart([]Intro{{}, {}}) {
		t.Error("a season with nothing decided read as opening at 0:00")
	}
}
