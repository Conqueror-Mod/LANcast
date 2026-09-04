package faces

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"lancast/internal/store"
)

/*
 * The embedding pass, without the model.
 *
 * The inference is a subprocess needing a C toolchain and a few hundred
 * megabytes, and it is not exercised here. What is exercised is everything
 * around it, which is where this worker can go wrong: refusing to run when it
 * cannot, refusing to *store* under a model name it was not given, attributing
 * vectors to the right photograph, and counting what happened.
 *
 * The second of those is the one worth a test on its own. A vector filed under
 * the wrong model name is not a broken row — it is a row that ranks
 * confidently against a query from a different coordinate system, and nothing
 * anywhere reports it.
 */

type fakeEmbedStore struct {
	mu       sync.Mutex
	saved    map[int64][]float32
	models   map[int64]string
	refuse   map[int64]error
	pending  [][]store.Item
	countHit int
}

func newFakeEmbedStore() *fakeEmbedStore {
	return &fakeEmbedStore{
		saved:  map[int64][]float32{},
		models: map[int64]string{},
		refuse: map[int64]error{},
	}
}

func (f *fakeEmbedStore) PhotosPendingEmbedding(_ context.Context, _ int64, _ string, _ int) ([]store.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pending) == 0 {
		return nil, nil
	}
	out := f.pending[0]
	f.pending = f.pending[1:]
	return out, nil
}

func (f *fakeEmbedStore) SavePhotoEmbedding(_ context.Context, id int64, model string, v []float32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refuse[id]; err != nil {
		return err
	}
	f.saved[id] = v
	f.models[id] = model
	return nil
}

func (f *fakeEmbedStore) PhotosPendingEmbeddingCount(_ context.Context, _ int64, _ string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.countHit++
	return 0, nil
}

func quietIndexer(st EmbedStore, tool *Tool) *Indexer {
	return NewIndexer(st, tool, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

/*
 * A pass refuses to start when the worker cannot embed, and says why.
 *
 * The alternative is a run that produces nothing and reports success, after
 * which the library looks indexed and nobody asks again — the same failure
 * ADR 0052 built the reason field to avoid, one feature along.
 */
func TestIndexerRefusesWhenSemanticSearchIsUnavailable(t *testing.T) {
	ix := quietIndexer(newFakeEmbedStore(), &Tool{Dir: t.TempDir(), ModelsDir: t.TempDir()})

	err := ix.Run(context.Background(), 1)
	if err == nil {
		t.Fatal("a pass started with no worker installed")
	}
	if ix.Stats().Running {
		t.Error("the pass is still marked running after refusing to start")
	}
}

/*
 * And it refuses to store anything under a model name it was not given.
 *
 * This is the failure with no symptom. A vector filed under a guessed name
 * ranks against queries from whatever space that name later means, which sorts
 * and is wrong and reports nothing — so the pass stops rather than inventing
 * one.
 */
func TestIndexerRefusesToGuessTheModelName(t *testing.T) {
	st := newFakeEmbedStore()
	st.pending = [][]store.Item{{{ID: 1, Path: "a.jpg"}}}

	ix := quietIndexer(st, &Tool{})
	// A tool reporting ready but naming no model is the shape this guards: a
	// worker older than the field, or a truncated capabilities line.
	ix.tool.cached = &Capabilities{Ready: true, SemanticReady: true, SemanticModel: ""}
	ix.tool.at = time.Now()

	err := ix.Run(context.Background(), 1)
	if err == nil {
		t.Fatal("the pass ran without knowing which model space to file vectors under")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.saved) != 0 {
		t.Error("a vector was stored under a guessed model name")
	}
}

/*
 * A refused write is counted, not escalated.
 *
 * The store declines an embedding for a photograph a mark covers, and a folder
 * can be marked *while* a pass runs — between the query that selected the
 * photograph and the write. That is the rule working, so it is a failure count
 * rather than an aborted pass.
 */
func TestARefusedWriteIsCountedRatherThanFatal(t *testing.T) {
	st := newFakeEmbedStore()
	st.refuse[2] = errors.New("photograph is covered by a mark")

	ix := quietIndexer(st, &Tool{})
	ix.record(context.Background(), store.Item{ID: 2, Path: "b.jpg"},
		embedLine{Path: "b.jpg", Vector: []float32{1, 0}}, "m")

	if got := ix.Stats().Failed; got != 1 {
		t.Errorf("failed count = %d, want 1", got)
	}
	if got := ix.Stats().Embedded; got != 0 {
		t.Errorf("a refused write was counted as embedded: %d", got)
	}
}

// A photograph the worker could not read is counted and left pending, so a
// drive that was asleep is retried rather than written off.
func TestAnUnreadablePhotographIsCountedAndLeftPending(t *testing.T) {
	st := newFakeEmbedStore()
	ix := quietIndexer(st, &Tool{})

	ix.record(context.Background(), store.Item{ID: 3, Path: "c.jpg"},
		embedLine{Path: "c.jpg", Error: "decode: unexpected EOF"}, "m")

	if got := ix.Stats().Failed; got != 1 {
		t.Errorf("failed count = %d, want 1", got)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.saved) != 0 {
		t.Error("something was stored for a photograph that could not be read")
	}
}

// An empty vector is a failure even when the worker reported no error — an
// all-zero embedding stores fine and compares as nothing against everything.
func TestAnEmptyVectorIsAFailure(t *testing.T) {
	st := newFakeEmbedStore()
	ix := quietIndexer(st, &Tool{})

	ix.record(context.Background(), store.Item{ID: 4, Path: "d.jpg"},
		embedLine{Path: "d.jpg"}, "m")

	if got := ix.Stats().Failed; got != 1 {
		t.Errorf("failed count = %d, want 1", got)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, stored := st.saved[4]; stored {
		t.Error("an empty vector was stored")
	}
}

// A stored vector carries the model it came from, which is what makes a swap
// something the next pass notices.
func TestAStoredVectorCarriesItsModel(t *testing.T) {
	st := newFakeEmbedStore()
	ix := quietIndexer(st, &Tool{})

	ix.record(context.Background(), store.Item{ID: 5, Path: "e.jpg"},
		embedLine{Path: "e.jpg", Vector: []float32{0, 1}}, "openclip-vit-b-32")

	st.mu.Lock()
	defer st.mu.Unlock()
	if st.models[5] != "openclip-vit-b-32" {
		t.Errorf("stored under model %q, want the one the worker named", st.models[5])
	}
	if got := ix.Stats().Embedded; got != 1 {
		t.Errorf("embedded count = %d, want 1", got)
	}
}
