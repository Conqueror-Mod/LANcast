package store

import (
	"context"
	"testing"
)

/*
 * Semantic photo search, at the storage layer (ADR 0060).
 *
 * No model is involved here and none is needed. The vectors are hand-written
 * directions in a small space, exactly as face_test.go stands in for an
 * embedder — ADR 0052 chose ONNX so the model can be swapped by replacing a
 * file, and a suite that only passed for one model's output would quietly make
 * that untrue.
 *
 * What these prove is the part that is ours rather than the model's: which
 * photographs are candidates, which are refused, what happens to a vector from
 * a different model, and that the ranking is a total order.
 */

// unit returns a normalised vector pointing along one axis, so cosine scores
// are the obvious ones and a test can say what it means.
func unit(axis, dims int) []float32 {
	v := make([]float32, dims)
	v[axis] = 1
	return v
}

const testModel = "openclip-vit-b-32"

func TestSearchRanksTheNearestPhotographFirst(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	near := fx.photo(t, s, "near.jpg", fx.folder)
	far := fx.photo(t, s, "far.jpg", fx.folder)
	if err := s.SavePhotoEmbedding(ctx, near, testModel, unit(0, 8)); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePhotoEmbedding(ctx, far, testModel, unit(1, 8)); err != nil {
		t.Fatal(err)
	}

	hits, err := s.SearchPhotosByVector(ctx, fx.library, testModel, unit(0, 8), 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}
	if hits[0].Item.ID != near {
		t.Errorf("the nearest photograph is not first: %v", hits)
	}
	if hits[0].Score <= hits[1].Score {
		t.Errorf("scores are not ordered: %v then %v", hits[0].Score, hits[1].Score)
	}
}

/*
 * A marked folder is unreachable, and deliberately by two independent routes.
 *
 * Saving refuses one outright, so ordinarily no vector exists to find. This
 * writes the row anyway — the state the refusal is there to prevent — so what
 * it proves is the second line: the search's own exclusion, which does not
 * depend on the first having worked.
 *
 * The stake is higher than it is for faces. A face embedding says *who*; this
 * says *what the picture is of*, so a covered folder reachable by searching its
 * contents would be a cover that lifts for anyone who guesses.
 */
func TestSearchNeverReturnsAMarkedFolder(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	open := fx.photo(t, s, "open.jpg", fx.folder)
	hidden := fx.photo(t, s, "hidden.jpg", fx.private)
	if err := s.SavePhotoEmbedding(ctx, open, testModel, unit(0, 8)); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePhotoEmbedding(ctx, hidden, testModel, unit(0, 8)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSensitive(ctx, fx.private, true); err != nil {
		t.Fatal(err)
	}
	// Defeat the deletion too, so the query is what is under test.
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO photo_embedding (item_id, model, dims, embedding, embedded_at)
		VALUES (?, ?, 8, ?, 1)
		ON CONFLICT(item_id) DO UPDATE SET embedding = excluded.embedding`,
		hidden, testModel, encodeEmbedding(unit(0, 8))); err != nil {
		t.Fatal(err)
	}

	hits, err := s.SearchPhotosByVector(ctx, fx.library, testModel, unit(0, 8), 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.Item.ID == hidden {
			t.Error("a photograph in a marked folder was returned by a semantic search — " +
				"a cover that lifts for anyone who guesses its contents")
		}
	}
	if len(hits) != 1 || hits[0].Item.ID != open {
		t.Errorf("the uncovered photograph should still be found; got %d hits", len(hits))
	}
}

// And the first line: saving one is refused outright, so ordinarily there is
// nothing for the query above to exclude.
func TestSavingAnEmbeddingForAMarkedPhotographIsRefused(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	p := fx.photo(t, s, "hidden.jpg", fx.private)
	if err := s.SetSensitive(ctx, fx.private, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePhotoEmbedding(ctx, p, testModel, unit(0, 8)); err == nil {
		t.Error("an embedding was stored for a photograph a mark covers")
	}
}

// Marking a folder afterwards deletes what was already computed — the vector
// would otherwise sit in the database and in every backup taken from then on.
func TestMarkingDeletesPhotoEmbeddings(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	p := fx.photo(t, s, "later.jpg", fx.private)
	if err := s.SavePhotoEmbedding(ctx, p, testModel, unit(0, 8)); err != nil {
		t.Fatal(err)
	}
	// Marking alone, with nothing called by hand — the deletion has to be part
	// of what marking *is*, or the property only holds where somebody
	// remembered to invoke it.
	if err := s.SetSensitive(ctx, fx.private, true); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM photo_embedding WHERE item_id = ?`, p).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("the embedding survived its folder being marked")
	}
}

/*
 * A vector from another model is skipped, not compared.
 *
 * Two models are two coordinate systems, and a cosine between them is a number
 * with no meaning that still sorts. Ranking a library against a mixture is
 * worse than ranking it against half of one, because nothing about the result
 * looks wrong.
 */
func TestAVectorFromAnotherModelIsNotCompared(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	ours := fx.photo(t, s, "ours.jpg", fx.folder)
	theirs := fx.photo(t, s, "theirs.jpg", fx.folder)
	if err := s.SavePhotoEmbedding(ctx, ours, testModel, unit(0, 8)); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePhotoEmbedding(ctx, theirs, "some-other-model", unit(0, 8)); err != nil {
		t.Fatal(err)
	}

	hits, err := s.SearchPhotosByVector(ctx, fx.library, testModel, unit(0, 8), 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Item.ID != ours {
		t.Errorf("a vector from another model was ranked alongside ours: %d hits", len(hits))
	}
}

// A width that does not match is skipped rather than crashing or scoring. It is
// what a truncated write or a half-swapped model looks like.
func TestAWrongWidthVectorIsSkipped(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	p := fx.photo(t, s, "short.jpg", fx.folder)
	if err := s.SavePhotoEmbedding(ctx, p, testModel, unit(0, 4)); err != nil {
		t.Fatal(err)
	}

	hits, err := s.SearchPhotosByVector(ctx, fx.library, testModel, unit(0, 8), 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("an 4-wide vector was compared against an 8-wide query: %v", hits)
	}
}

/*
 * The pass asks for what this model has not seen.
 *
 * Keyed on the model rather than on presence: a vector from another model is
 * not a stale vector, it is a vector in a different space, and the swap ADR
 * 0052 made cheap is only cheap if something notices.
 */
func TestPendingIsKeyedOnTheModel(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	done := fx.photo(t, s, "done.jpg", fx.folder)
	stale := fx.photo(t, s, "stale.jpg", fx.folder)
	never := fx.photo(t, s, "never.jpg", fx.folder)
	covered := fx.photo(t, s, "covered.jpg", fx.private)

	if err := s.SavePhotoEmbedding(ctx, done, testModel, unit(0, 8)); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePhotoEmbedding(ctx, stale, "some-other-model", unit(0, 8)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSensitive(ctx, fx.private, true); err != nil {
		t.Fatal(err)
	}

	pending, err := s.PhotosPendingEmbedding(ctx, fx.library, testModel, 100)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]bool{}
	for _, it := range pending {
		got[it.ID] = true
	}
	if got[done] {
		t.Error("a photograph this model has already seen is still pending")
	}
	if !got[stale] {
		t.Error("a photograph embedded by a different model is not pending; the swap would never complete")
	}
	if !got[never] {
		t.Error("a photograph nothing has seen is not pending")
	}
	if got[covered] {
		t.Error("a covered photograph is pending; the pass would ask the sidecar to open it")
	}
}

// Ties break on id, so two identical searches cannot disagree about the order.
func TestRankingIsATotalOrder(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	var ids []int64
	for _, name := range []string{"a.jpg", "b.jpg", "c.jpg"} {
		id := fx.photo(t, s, name, fx.folder)
		// Identical vectors, so every score ties.
		if err := s.SavePhotoEmbedding(ctx, id, testModel, unit(0, 8)); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	first, err := s.SearchPhotosByVector(ctx, fx.library, testModel, unit(0, 8), 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.SearchPhotosByVector(ctx, fx.library, testModel, unit(0, 8), 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := range first {
		if first[i].Item.ID != second[i].Item.ID {
			t.Fatalf("two identical searches returned different orders: %v vs %v", first, second)
		}
	}
	if first[0].Item.ID != ids[0] {
		t.Errorf("a tie did not break on id: %v", first)
	}
}
