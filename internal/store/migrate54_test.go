package store

import (
	"context"
	"testing"
)

/*
 * Revision 54 — credits markers from the ungated rule are decided again, and a
 * file whose length changes loses its credits stamp.
 */

func pendingMarkerIDs(t *testing.T, st *Store) map[int64]bool {
	t.Helper()
	items, err := st.PendingMarkers(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]bool{}
	for _, it := range items {
		out[it.ID] = true
	}
	return out
}

func credits(source string) []Marker {
	return []Marker{{Kind: MarkerCredits, StartMS: 7_600_000, Source: source, Confidence: 0.9}}
}

func TestRevision54RequeuesOnlyMarkersFromTheUngatedRule(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	size := int64(4_000_000_000)
	old := seedProbed(t, st, lib, "Old.mkv", probedFile{kind: "movie", sizeBytes: size, durationMS: 8_000_000})
	gated := seedProbed(t, st, lib, "Gated.mkv", probedFile{kind: "movie", sizeBytes: size, durationMS: 8_000_000})
	abstained := seedProbed(t, st, lib, "Abstained.mkv", probedFile{kind: "movie", sizeBytes: size, durationMS: 8_000_000})

	for id, m := range map[int64][]Marker{
		old:       credits("blackdetect"),
		gated:     credits("blackdetect-gated"),
		abstained: nil, // examined, nothing found
	} {
		if err := st.SaveMarkers(ctx, id, []string{MarkerCredits}, m); err != nil {
			t.Fatal(err)
		}
	}
	if q := pendingMarkerIDs(t, st); len(q) != 0 {
		t.Fatalf("queue before the migration = %v, want empty", q)
	}

	if _, err := st.db.Exec(`UPDATE meta SET value = '53' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(st.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	q := pendingMarkerIDs(t, st)
	if !q[old] {
		t.Error("a marker from the ungated rule was not put back on the queue")
	}
	if q[gated] {
		t.Error("a marker the gate already decided was queued again")
	}
	// The gate only removes candidates. A file with none abstains again, and
	// decoding it again would buy nothing.
	if q[abstained] {
		t.Error("an abstention was queued again; the gate cannot turn it into an answer")
	}
	// Left in place until the new pass replaces it — the player ignores it.
	if ms, _ := st.MarkersFor(ctx, old); len(ms) != 1 {
		t.Errorf("old marker = %+v, want it kept until the re-run replaces it", ms)
	}
}

func TestANewLengthUnstampsTheCreditsPass(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := seedProbed(t, st, lib, "Alien3.mkv", probedFile{kind: "movie", sizeBytes: 1, durationMS: 6_900_000})
	if err := st.SaveMarkers(ctx, id, []string{MarkerCredits}, credits("blackdetect-gated")); err != nil {
		t.Fatal(err)
	}

	// The same bytes probed again, a frame apart: the marker still stands.
	if err := st.SaveProbe(ctx, id, ProbeResult{DurationMS: 6_900_040}); err != nil {
		t.Fatal(err)
	}
	if pendingMarkerIDs(t, st)[id] {
		t.Fatal("a re-probe that agreed to within a frame threw the credits away")
	}

	// A longer cut: 6,900s became 8,650s. A marker at 7,600s was 110% of the
	// old length and is 87.9% of the new one — a position in a different film.
	if err := st.SaveProbe(ctx, id, ProbeResult{DurationMS: 8_650_000}); err != nil {
		t.Fatal(err)
	}
	if !pendingMarkerIDs(t, st)[id] {
		t.Error("the file's length changed and its credits were not looked for again")
	}
}
