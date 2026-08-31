package store

import (
	"context"
	"math"
	"testing"
)

/*
 * Faces, grouping, and the rule that outranks the grouping (ADR 0052).
 *
 * The embeddings here are synthetic — deliberately. Nothing in this file
 * depends on which model produced a vector, and that is the property worth
 * protecting: ADR 0052 chose ONNX so the model can be replaced by swapping a
 * file, and a test suite that only passed for one model's output would quietly
 * make that untrue.
 *
 * So: two clearly-separated directions in vector space stand in for two people.
 */

// person returns a unit vector pointing mostly along one axis, with a small
// perturbation so no two faces of the same person are identical.
func person(axis, n, dims int) []float32 {
	v := make([]float32, dims)
	for i := range v {
		v[i] = 0.01 * float32((i+n)%3)
	}
	v[axis] = 1
	// Normalise, so the fixtures are the shape a real embedder returns.
	var sum float64
	for _, f := range v {
		sum += float64(f) * float64(f)
	}
	norm := float32(math.Sqrt(sum))
	for i := range v {
		v[i] /= norm
	}
	return v
}

type faceFixture struct {
	library int64
	folder  int64
	private int64
}

func makeFaceLibrary(t *testing.T, s *Store) faceFixture {
	t.Helper()
	ctx := context.Background()
	lib, err := s.CreateLibrary(ctx, "Pictures", "picture", `C:\pics`)
	if err != nil {
		t.Fatal(err)
	}
	folder, err := s.EnsureDerivedContainer(ctx, lib.ID, "gallery",
		`C:\pics\Album`, "Album", "album", nil)
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.EnsureDerivedContainer(ctx, lib.ID, "gallery",
		`C:\pics\Private`, "Private", "private", nil)
	if err != nil {
		t.Fatal(err)
	}
	return faceFixture{library: lib.ID, folder: folder, private: private}
}

func (fx faceFixture) photo(t *testing.T, s *Store, name string, parent int64) int64 {
	t.Helper()
	res, err := s.db.Exec(`
		INSERT INTO media_item (library_id, kind, path, title, sort_title,
			parent_id, added_at, updated_at, missing)
		VALUES (?, 'photo', ?, ?, ?, ?, 1, 1, 0)`,
		fx.library, `C:\pics\`+name, name, name, parent)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func clusterOf(t *testing.T, s *Store, itemID int64) int64 {
	t.Helper()
	var id *int64
	if err := s.db.QueryRow(
		`SELECT cluster_id FROM face WHERE item_id = ? LIMIT 1`, itemID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id == nil {
		t.Fatalf("item %d has a face in no cluster", itemID)
	}
	return *id
}

// Two photographs of the same person land in one group; a third person does not
// join them.
func TestClusteringGroupsAPersonAndSeparatesAnother(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	a1 := fx.photo(t, s, "a1.jpg", fx.folder)
	a2 := fx.photo(t, s, "a2.jpg", fx.folder)
	b1 := fx.photo(t, s, "b1.jpg", fx.folder)

	for i, id := range []int64{a1, a2} {
		if err := s.RecordFaces(ctx, id, []Face{{Score: 0.9, Embedding: person(0, i, 8)}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordFaces(ctx, b1, []Face{{Score: 0.9, Embedding: person(4, 0, 8)}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ClusterLibrary(ctx, fx.library); err != nil {
		t.Fatal(err)
	}

	if clusterOf(t, s, a1) != clusterOf(t, s, a2) {
		t.Error("two photographs of one person landed in different groups")
	}
	if clusterOf(t, s, a1) == clusterOf(t, s, b1) {
		t.Error("two different people were grouped as one — the failure that " +
			"attaches somebody's face to somebody else's name")
	}
}

/*
 * The rule that outranks everything else here.
 *
 * A name a person typed is an edit. A re-cluster may move faces and create
 * groups; it may never rename or discard a named one. This has the same
 * standing as the locked-fields test the rest of the project holds — if it
 * fails, LANcast has become the thing it was built to replace.
 */
func TestAReclusterNeverOverwritesAName(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	a1 := fx.photo(t, s, "a1.jpg", fx.folder)
	if err := s.RecordFaces(ctx, a1, []Face{{Score: 0.9, Embedding: person(0, 0, 8)}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ClusterLibrary(ctx, fx.library); err != nil {
		t.Fatal(err)
	}
	id := clusterOf(t, s, a1)
	if err := s.NameCluster(ctx, id, "Georgia"); err != nil {
		t.Fatal(err)
	}

	// More photographs arrive, of that person and of somebody else, and the
	// library is clustered again.
	a2 := fx.photo(t, s, "a2.jpg", fx.folder)
	b1 := fx.photo(t, s, "b1.jpg", fx.folder)
	if err := s.RecordFaces(ctx, a2, []Face{{Score: 0.9, Embedding: person(0, 1, 8)}}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFaces(ctx, b1, []Face{{Score: 0.9, Embedding: person(4, 0, 8)}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := s.ClusterLibrary(ctx, fx.library); err != nil {
			t.Fatal(err)
		}
	}

	clusters, err := s.FaceClusters(ctx, fx.library)
	if err != nil {
		t.Fatal(err)
	}
	var named *FaceCluster
	for i := range clusters {
		if clusters[i].Name != nil && *clusters[i].Name == "Georgia" {
			named = &clusters[i]
		}
	}
	if named == nil {
		t.Fatal("the named group did not survive re-clustering")
	}
	if named.ID != id {
		t.Errorf("the name moved to a different cluster (%d, was %d)", named.ID, id)
	}
	if !named.NameLocked {
		t.Error("the name is not locked")
	}
	// And the new photograph of that person joined them rather than starting a
	// rival group, which is what makes naming worth doing.
	if clusterOf(t, s, a2) != id {
		t.Error("a new photograph of a named person started a new group instead")
	}
}

// Naming can be undone — somebody who typed the wrong name must be able to
// clear it, and the group goes back to being ordinary.
func TestANameCanBeCleared(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	a1 := fx.photo(t, s, "a1.jpg", fx.folder)
	_ = s.RecordFaces(ctx, a1, []Face{{Score: 0.9, Embedding: person(0, 0, 8)}})
	_ = s.ClusterLibrary(ctx, fx.library)
	id := clusterOf(t, s, a1)

	if err := s.NameCluster(ctx, id, "Wrong"); err != nil {
		t.Fatal(err)
	}
	if err := s.NameCluster(ctx, id, ""); err != nil {
		t.Fatal(err)
	}
	clusters, _ := s.FaceClusters(ctx, fx.library)
	for _, c := range clusters {
		if c.ID == id {
			if c.Name != nil || c.NameLocked {
				t.Errorf("cleared name still reads %v locked=%v", c.Name, c.NameLocked)
			}
		}
	}
}

/*
 * A marked folder is never indexed, and refusing is the store's job.
 *
 * The worker is not supposed to be handed one — but "not supposed to" is a
 * habit, and this makes it a property. A people view is built from face crops,
 * so indexing a marked folder would put its contents on a screen that is not
 * the folder.
 */
func TestFacesAreRefusedForAMarkedFolder(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	p := fx.photo(t, s, "p.jpg", fx.private)
	if err := s.SetSensitive(ctx, fx.private, true); err != nil {
		t.Fatal(err)
	}
	err := s.RecordFaces(ctx, p, []Face{{Score: 0.9, Embedding: person(0, 0, 8)}})
	if err == nil {
		t.Error("faces were recorded for a photograph in a marked folder")
	}
}

/*
 * And marking a folder afterwards deletes what was already indexed.
 *
 * Deleted rather than hidden: an embedding is derived from the photograph and
 * is not less private than it, so leaving it in place would leave it in every
 * backup taken from then on.
 */
func TestMarkingAFolderDeletesItsFaces(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	keep := fx.photo(t, s, "keep.jpg", fx.folder)
	gone := fx.photo(t, s, "gone.jpg", fx.private)
	_ = s.RecordFaces(ctx, keep, []Face{{Score: 0.9, Embedding: person(0, 0, 8)}})
	_ = s.RecordFaces(ctx, gone, []Face{{Score: 0.9, Embedding: person(4, 0, 8)}})

	// Marking is all it takes — SetSensitive deletes them, so this does not
	// depend on a caller remembering to.
	if err := s.SetSensitive(ctx, fx.private, true); err != nil {
		t.Fatal(err)
	}

	var left int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM face WHERE item_id = ?`, gone).Scan(&left)
	if left != 0 {
		t.Error("a marked folder's faces survived")
	}
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM face WHERE item_id = ?`, keep).Scan(&left)
	if left != 1 {
		t.Error("an unmarked photograph lost its faces")
	}
}

// Re-running detection over a photograph replaces its faces rather than
// doubling them. The worker is re-runnable by design.
func TestReDetectingReplacesRatherThanAppends(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	p := fx.photo(t, s, "p.jpg", fx.folder)
	two := []Face{
		{Score: 0.9, Embedding: person(0, 0, 8)},
		{Score: 0.8, Embedding: person(4, 0, 8)},
	}
	for i := 0; i < 3; i++ {
		if err := s.RecordFaces(ctx, p, two); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM face WHERE item_id = ?`, p).Scan(&n)
	if n != 2 {
		t.Errorf("after three detection passes the photograph has %d faces, want 2", n)
	}
}

// The embedding survives the round trip through the database exactly. A lossy
// blob would degrade every similarity comparison in a way nothing would report.
func TestAnEmbeddingRoundTripsExactly(t *testing.T) {
	in := []float32{0, 1, -1, 0.5, -0.25, 3.4028235e+38, 1e-45}
	out := decodeEmbedding(encodeEmbedding(in))
	if len(out) != len(in) {
		t.Fatalf("length %d, want %d", len(out), len(in))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("element %d = %v, want %v", i, out[i], in[i])
		}
	}
}

// Cosine is the comparison the threshold is expressed in, so its edges matter.
func TestCosine(t *testing.T) {
	a := []float32{1, 0, 0}
	if got := cosine(a, a); math.Abs(got-1) > 1e-6 {
		t.Errorf("a face against itself = %v, want 1", got)
	}
	if got := cosine(a, []float32{0, 1, 0}); math.Abs(got) > 1e-6 {
		t.Errorf("orthogonal = %v, want 0", got)
	}
	// Length is not identity: the same direction at twice the magnitude is the
	// same person, which is why this is cosine rather than a distance.
	if got := cosine(a, []float32{2, 0, 0}); math.Abs(got-1) > 1e-6 {
		t.Errorf("same direction, different magnitude = %v, want 1", got)
	}
	// Mismatched or empty vectors compare as unrelated rather than panicking —
	// a model change mid-library must not take the clusterer down.
	if got := cosine(a, []float32{1, 0}); got != 0 {
		t.Errorf("mismatched dimensions = %v, want 0", got)
	}
	if got := cosine(nil, nil); got != 0 {
		t.Errorf("empty = %v, want 0", got)
	}
}
