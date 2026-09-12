package store

import (
	"context"
	"testing"
)

/*
 * The stamp belongs to the pass that owns it.
 *
 * markers_at is the credits pass's "looked at this file" flag, and SaveMarkers
 * set it whatever the caller was authoritative about. The intro pass writes one
 * marker per episode through the same method, so an episode it examined was
 * recorded as examined for credits too. On a real library: 994 of 994 episodes
 * stamped, 0 with a credits marker, against 1,112 of 1,208 films — which no
 * intro pass touches.
 */

func TestWritingAnIntroDoesNotRetireTheEpisodeFromCreditsDetection(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	_, eps := seedIntroSeason(t, st, "Show", 1, 2)

	end := int64(30_000)
	if err := st.SaveMarkers(ctx, eps[0], []string{MarkerIntro}, []Marker{{
		Kind: MarkerIntro, StartMS: 500, EndMS: &end, Source: "fingerprint", Confidence: 1,
	}}); err != nil {
		t.Fatal(err)
	}

	pending, err := st.PendingMarkers(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range pending {
		if it.ID == eps[0] {
			return
		}
	}
	t.Fatal("an episode with an intro marker was dropped from the credits queue; " +
		"its credits would never be detected")
}

// The credits pass still records that it looked, including when it found
// nothing: that abstention is the whole reason the stamp exists.
func TestTheCreditsPassStillStampsWhatItExamined(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	_, eps := seedIntroSeason(t, st, "Show", 1, 2)

	if err := st.SaveMarkers(ctx, eps[0], []string{MarkerCredits}, nil); err != nil {
		t.Fatal(err)
	}
	pending, err := st.PendingMarkers(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range pending {
		if it.ID == eps[0] {
			t.Fatal("an episode the credits pass examined came back on the queue; " +
				"an abstention would be re-decoded for ever")
		}
	}
}

// An intro write must not clear a stamp the credits pass has already earned,
// either: the two passes run in either order.
func TestAnIntroWriteLeavesAnExistingCreditsStampAlone(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	_, eps := seedIntroSeason(t, st, "Show", 1, 2)

	if err := st.SaveMarkers(ctx, eps[0], []string{MarkerCredits}, nil); err != nil {
		t.Fatal(err)
	}
	end := int64(30_000)
	if err := st.SaveMarkers(ctx, eps[0], []string{MarkerIntro}, []Marker{{
		Kind: MarkerIntro, StartMS: 500, EndMS: &end, Source: "fingerprint",
	}}); err != nil {
		t.Fatal(err)
	}

	pending, _ := st.PendingMarkers(ctx, 50)
	for _, it := range pending {
		if it.ID == eps[0] {
			t.Fatal("writing an intro put an already-examined episode back on the credits queue")
		}
	}
	ms, err := st.MarkersFor(ctx, eps[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].Kind != MarkerIntro {
		t.Errorf("markers = %+v, want the intro that was just written", ms)
	}
}
