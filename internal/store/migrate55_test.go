package store

import (
	"context"
	"testing"
)

/*
 * Revision 55 — an episode keeps the ungated credits marker revision 54 sent
 * back, because episodes are no longer gated and re-examining one would
 * arrive at the marker it already has.
 */
func TestRevision55RestampsEpisodesWithUngatedMarkersOnly(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "TV", "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	size := int64(400_000_000)
	ep := seedProbed(t, st, lib, "S01E01.mkv", probedFile{kind: "episode", sizeBytes: size, durationMS: 1_300_000})
	bare := seedProbed(t, st, lib, "S01E02.mkv", probedFile{kind: "episode", sizeBytes: size, durationMS: 1_300_000})
	film := seedProbed(t, st, lib, "Film.mkv", probedFile{kind: "movie", sizeBytes: size, durationMS: 6_000_000})
	for _, id := range []int64{ep, film} {
		if err := st.SaveMarkers(ctx, id, []string{MarkerCredits}, credits("blackdetect")); err != nil {
			t.Fatal(err)
		}
	}
	// Where revision 54 left them: all waiting.
	if _, err := st.db.Exec(`UPDATE media_item SET markers_at = NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE meta SET value = '54' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(st.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	q := pendingMarkerIDs(t, st)
	if q[ep] {
		t.Error("an episode with an ungated marker is still queued to arrive at the same marker")
	}
	if !q[film] {
		t.Error("a film's ungated marker was restamped; films are still gated")
	}
	// Nothing to keep, so nothing to stamp: it has not been examined.
	if !q[bare] {
		t.Error("an episode with no marker was stamped as examined")
	}
}
