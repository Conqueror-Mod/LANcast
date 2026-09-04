package store

import (
	"context"
	"testing"
)

/*
 * Photographs of one person (ADR 0052).
 *
 * Face grouping's whole purpose is finding a photograph again — FacePeople.tsx
 * says so in its first paragraph — and until this filter existed a named group
 * led nowhere. These are the three properties that make it answer the question
 * honestly rather than approximately.
 */

// listedIDs runs a filter and returns the item ids it yielded, so a test can
// say what it means rather than index into a slice.
func listedIDs(t *testing.T, s *Store, f ItemFilter) []int64 {
	t.Helper()
	items, _, err := s.ListItems(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]int64, 0, len(items))
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func contains(ids []int64, want int64) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// The basic claim: a group's photographs, and nobody else's.
func TestPhotographsOfOnePerson(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	mine := fx.photo(t, s, "mine.jpg", fx.folder)
	theirs := fx.photo(t, s, "theirs.jpg", fx.folder)

	if err := s.RecordFaces(ctx, mine, []Face{{Score: 0.9, Embedding: person(0, 0, 8)}}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFaces(ctx, theirs, []Face{{Score: 0.9, Embedding: person(1, 0, 8)}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ClusterLibrary(ctx, fx.library); err != nil {
		t.Fatal(err)
	}

	got := listedIDs(t, s, ItemFilter{
		LibraryID:      fx.library,
		Kind:           "photo",
		FaceClusterIDs: []int64{clusterOf(t, s, mine)},
	})
	if !contains(got, mine) {
		t.Errorf("the filter did not return the photograph the person is in: %v", got)
	}
	if contains(got, theirs) {
		t.Errorf("the filter returned somebody else's photograph: %v", got)
	}
}

/*
 * One photograph, two faces of the same person, one tile.
 *
 * A mirror, a photograph of a photograph, or a group shot the detector fires
 * twice on. A join would return the picture once per face and count it once
 * per face with it — so the grid would show a duplicate and the total above it
 * would be wrong, which is the shape of bug nothing reports because nothing
 * fails.
 */
func TestOnePhotographWithTwoFacesOfOnePersonAppearsOnce(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	p := fx.photo(t, s, "mirror.jpg", fx.folder)
	if err := s.RecordFaces(ctx, p, []Face{
		{Score: 0.9, Embedding: person(0, 0, 8)},
		{Score: 0.8, Embedding: person(0, 1, 8)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ClusterLibrary(ctx, fx.library); err != nil {
		t.Fatal(err)
	}

	f := ItemFilter{LibraryID: fx.library, Kind: "photo", FaceClusterIDs: []int64{clusterOf(t, s, p)}}
	got := listedIDs(t, s, f)
	if len(got) != 1 {
		t.Errorf("one photograph with two faces of one person listed %d times: %v", len(got), got)
	}

	_, total, err := s.ListItems(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1 — the count is what the grid draws above itself", total)
	}
}

/*
 * A marked folder is not reachable through a person filter.
 *
 * This is a security property rather than a behaviour, and it is deliberately
 * belted and braced. Marking a folder deletes the faces under it
 * (DeleteFacesUnderSensitive), so in ordinary operation no face row survives
 * there and the filter finds nothing by construction. This test defeats that
 * first line on purpose — inserting the face row directly, which RecordFaces
 * would have refused — so what it proves is the *second*: ExcludeSensitive on
 * the query itself.
 *
 * Worth having both. The face table's guarantee depends on marking and
 * indexing never interleaving badly, and the API's guarantee does not depend on
 * anything. This project already defends CSRF twice for the same reason.
 *
 * What it would mean to get wrong: a person filter would be a second door onto
 * exactly what ADR 0051 covers — you could not open the folder, but you could
 * ask who was in it and be shown.
 */
func TestAMarkedFolderIsNotReachableThroughAPersonFilter(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	// One photograph in the ordinary folder, so a cluster exists to ask about.
	open := fx.photo(t, s, "open.jpg", fx.folder)
	if err := s.RecordFaces(ctx, open, []Face{{Score: 0.9, Embedding: person(0, 0, 8)}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ClusterLibrary(ctx, fx.library); err != nil {
		t.Fatal(err)
	}
	cluster := clusterOf(t, s, open)

	// And one in the private folder, marked, with a face row written straight
	// into the table — the state RecordFaces refuses and marking cleans up.
	hidden := fx.photo(t, s, "hidden.jpg", fx.private)
	if err := s.SetSensitive(ctx, fx.private, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO face (item_id, cluster_id, x, y, w, h, score, embedding, detected_at)
		VALUES (?, ?, 0, 0, 10, 10, 0.9, X'00', 1)`, hidden, cluster); err != nil {
		t.Fatal(err)
	}

	got := listedIDs(t, s, ItemFilter{
		LibraryID:      fx.library,
		Kind:           "photo",
		FaceClusterIDs: []int64{cluster},
	})
	if contains(got, hidden) {
		t.Error("a photograph in a marked folder was returned by a person filter — " +
			"the filter is a second door onto what ADR 0051 covers")
	}
	if !contains(got, open) {
		t.Errorf("the unmarked photograph went missing too: %v", got)
	}
}

/*
 * The tile's number is what its grid contains.
 *
 * The tile says "N photographs" and pressing it opens the filter above. Those
 * two came from different queries and disagreed in three ways: the count was
 * faces rather than photographs, so a group shot counted twice; it included
 * folders nobody may open; and nothing compared them, so the tile could promise
 * forty and open a grid of thirty-eight without anything failing.
 *
 * Asserting them against each other rather than against a literal is the point.
 * A number this test hardcoded would go on agreeing with itself after somebody
 * changed one of the two queries.
 */
func TestAPersonTileCountsWhatTheirGridHolds(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	// Two ordinary photographs, one of them a group shot with the same person
	// detected twice, and one in a folder that gets marked afterwards.
	single := fx.photo(t, s, "single.jpg", fx.folder)
	twice := fx.photo(t, s, "twice.jpg", fx.folder)
	private := fx.photo(t, s, "private.jpg", fx.private)

	for _, p := range []int64{single, private} {
		if err := s.RecordFaces(ctx, p, []Face{{Score: 0.9, Embedding: person(0, 0, 8)}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordFaces(ctx, twice, []Face{
		{Score: 0.9, Embedding: person(0, 1, 8)},
		{Score: 0.8, Embedding: person(0, 2, 8)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ClusterLibrary(ctx, fx.library); err != nil {
		t.Fatal(err)
	}
	cluster := clusterOf(t, s, single)

	if err := s.SetSensitive(ctx, fx.private, true); err != nil {
		t.Fatal(err)
	}

	clusters, err := s.FaceClusters(ctx, fx.library)
	if err != nil {
		t.Fatal(err)
	}
	var tile int
	for _, c := range clusters {
		if c.ID == cluster {
			tile = c.Count
		}
	}

	_, total, err := s.ListItems(ctx, ItemFilter{
		LibraryID: fx.library, Kind: "photo", FaceClusterIDs: []int64{cluster},
	})
	if err != nil {
		t.Fatal(err)
	}

	if tile != total {
		t.Errorf("the tile says %d photographs and the grid holds %d", tile, total)
	}
	if tile != 2 {
		t.Errorf("count = %d, want 2 — two photographs, one of which holds the "+
			"same person twice, and one marked folder that counts for nothing", tile)
	}
}

/*
 * One person, several groups, all their photographs.
 *
 * Naming does not merge groups — a re-cluster seeds a named one as an anchor
 * and never dissolves it, which is the locked-fields rule applied to identity —
 * so a person routinely *is* several groups. collapsePeople.ts already puts
 * them on one tile and renames them together.
 *
 * Measured on a real library it is not an edge case: one person's photographs
 * split 277/73 across two groups, the smaller one almost entirely a single
 * photo shoot. A single-valued filter would have shown 277 of 350 and said
 * nothing about the missing 73 — the failure being that it looks like an
 * answer.
 *
 * This is why the parameter is repeatable and why repeated means OR. AND —
 * photographs with two *different* people in them — is a separate question and
 * would be a separate parameter.
 */
func TestAPersonSpanningTwoGroupsGetsAllTheirPhotographs(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	everyday := fx.photo(t, s, "everyday.jpg", fx.folder)
	shoot := fx.photo(t, s, "shoot.jpg", fx.folder)
	stranger := fx.photo(t, s, "stranger.jpg", fx.folder)

	// Two directions far enough apart to cluster separately — the shape a photo
	// shoot makes — plus a third person who must not be swept in.
	if err := s.RecordFaces(ctx, everyday, []Face{{Score: 0.9, Embedding: person(0, 0, 8)}}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFaces(ctx, shoot, []Face{{Score: 0.9, Embedding: person(1, 0, 8)}}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFaces(ctx, stranger, []Face{{Score: 0.9, Embedding: person(2, 0, 8)}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ClusterLibrary(ctx, fx.library); err != nil {
		t.Fatal(err)
	}

	// Both groups are the same person, the way naming two of them makes them.
	both := []int64{clusterOf(t, s, everyday), clusterOf(t, s, shoot)}
	for _, id := range both {
		if err := s.NameCluster(ctx, id, "Georgia"); err != nil {
			t.Fatal(err)
		}
	}

	got := listedIDs(t, s, ItemFilter{
		LibraryID:      fx.library,
		Kind:           "photo",
		FaceClusterIDs: both,
	})
	if !contains(got, everyday) || !contains(got, shoot) {
		t.Errorf("a person's two groups did not both contribute: %v", got)
	}
	if contains(got, stranger) {
		t.Errorf("somebody else was swept in: %v", got)
	}
	if len(got) != 2 {
		t.Errorf("got %d photographs, want 2: %v", len(got), got)
	}
}
