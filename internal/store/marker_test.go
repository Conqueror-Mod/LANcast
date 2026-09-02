package store

import (
	"context"
	"path/filepath"
	"testing"
)

// seedMarkerFilm makes a probed film, which is what the marker queue requires.
func seedMarkerFilm(t *testing.T, st *Store, title string, durMS int64, probed bool) int64 {
	t.Helper()
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "Films "+title, "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.UpsertItem(ctx, ScanFile{
		LibraryID: lib.ID, Path: filepath.Join(lib.Path, title+".mkv"),
		Kind: "movie", Title: title, SortTitle: title,
		Container: "mkv", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	q := `UPDATE media_item SET duration_ms = ? WHERE id = ?`
	args := []any{durMS, id}
	if probed {
		q = `UPDATE media_item SET duration_ms = ?, probed_at = 1 WHERE id = ?`
	}
	if _, err := st.db.ExecContext(ctx, q, args...); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPendingMarkersSkipsUnprobedItems(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	ready := seedMarkerFilm(t, st, "Probed", 6_000_000, true)
	seedMarkerFilm(t, st, "Unprobed", 6_000_000, false)

	got, err := st.PendingMarkers(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != ready {
		// Detection needs the file's real length to know where 88% is, and an
		// unprobed duration is the provider's runtime or nothing at all.
		t.Fatalf("got %d items, want only the probed one (%d)", len(got), ready)
	}
}

// The trap faces_at exists to avoid: a film with no detectable credits produces
// no rows, and without a stamp it would be decoded again on every pass for ever.
func TestSaveMarkersStampsEvenWhenNothingWasFound(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	id := seedMarkerFilm(t, st, "NoFade", 6_000_000, true)

	if err := st.SaveMarkers(ctx, id, []string{MarkerCredits}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := st.PendingMarkers(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Error("the item is still pending — an abstention must be remembered")
	}
	m, err := st.MarkersFor(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Errorf("MarkersFor = %+v, want none", m)
	}
}

func TestSaveMarkersRoundTrips(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	id := seedMarkerFilm(t, st, "Film", 6_000_000, true)

	err := st.SaveMarkers(ctx, id, []string{MarkerCredits}, []Marker{{
		Kind: MarkerCredits, StartMS: 5_634_000, Source: "blackdetect", Confidence: 0.9,
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.MarkersFor(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d markers, want 1", len(got))
	}
	if got[0].StartMS != 5_634_000 || got[0].Confidence != 0.9 {
		t.Errorf("marker = %+v, want start 5634000 confidence 0.9", got[0])
	}
	if got[0].EndMS != nil {
		t.Errorf("EndMS = %v, want nil — credits run to the end of the file", got[0].EndMS)
	}
	if got[0].CreatedAt == 0 {
		t.Error("CreatedAt is zero")
	}
}

// A second pass replaces its own kind rather than appending, or re-running the
// detector doubles every marker.
func TestSaveMarkersReplacesItsOwnKind(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	id := seedMarkerFilm(t, st, "Film", 6_000_000, true)

	for _, at := range []int64{5_000_000, 5_634_000} {
		if err := st.SaveMarkers(ctx, id, []string{MarkerCredits}, []Marker{{
			Kind: MarkerCredits, StartMS: at, Source: "blackdetect", Confidence: 0.9,
		}}); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := st.MarkersFor(ctx, id)
	if len(got) != 1 || got[0].StartMS != 5_634_000 {
		t.Errorf("markers = %+v, want only the newest", got)
	}
}

// The credits detector knows nothing about intros and must not delete one by
// writing an empty list. This is what the kinds argument is for.
func TestSaveMarkersLeavesOtherKindsAlone(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	id := seedMarkerFilm(t, st, "Episode", 1_200_000, true)

	end := int64(90_000)
	if err := st.SaveMarkers(ctx, id, []string{MarkerIntro}, []Marker{{
		Kind: MarkerIntro, StartMS: 30_000, EndMS: &end, Source: "fingerprint", Confidence: 0.8,
	}}); err != nil {
		t.Fatal(err)
	}
	// A credits pass that found nothing.
	if err := st.SaveMarkers(ctx, id, []string{MarkerCredits}, nil); err != nil {
		t.Fatal(err)
	}

	got, _ := st.MarkersFor(ctx, id)
	if len(got) != 1 || got[0].Kind != MarkerIntro {
		t.Fatalf("markers = %+v, want the intro to survive a credits pass", got)
	}
	if got[0].EndMS == nil || *got[0].EndMS != 90_000 {
		t.Errorf("EndMS = %v, want 90000 — an intro has a real finish", got[0].EndMS)
	}
}

// The window and the length thresholds are tuned numbers, so a build that
// moves them has to be able to ask every film the new question.
func TestClearMarkersRequeuesEverythingExamined(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	id := seedMarkerFilm(t, st, "Film", 6_000_000, true)
	if err := st.SaveMarkers(ctx, id, []string{MarkerCredits}, nil); err != nil {
		t.Fatal(err)
	}

	n, err := st.ClearMarkers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("cleared %d, want 1", n)
	}
	pending, _ := st.PendingMarkers(ctx, 10)
	if len(pending) != 1 {
		t.Error("the item did not come back onto the queue")
	}
}
