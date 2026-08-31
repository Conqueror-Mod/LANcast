package faces

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"

	"lancast/internal/store"
)

/*
 * The face pass.
 *
 * The inference is a subprocess and is not exercised here — it needs a C
 * toolchain and a 38MB model. What is exercised is everything around it, which
 * is where this worker can actually go wrong: reading the wire format,
 * attributing results to the right photograph, and deciding what counts as
 * "examined".
 *
 * That last one is subtler than it looks and is the difference between a pass
 * that finishes and one that loops on a bad file for ever.
 */

type fakeStore struct {
	mu        sync.Mutex
	recorded  map[int64][]store.Face
	done      map[int64]bool
	clustered int
	refuse    map[int64]error
	pending   [][]store.Item
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		recorded: map[int64][]store.Face{},
		done:     map[int64]bool{},
		refuse:   map[int64]error{},
	}
}

func (f *fakeStore) PendingFaces(_ context.Context, _ int64, _ int) ([]store.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pending) == 0 {
		return nil, nil
	}
	out := f.pending[0]
	f.pending = f.pending[1:]
	return out, nil
}

func (f *fakeStore) PendingFacesCount(context.Context, int64) (int, error) { return 0, nil }

func (f *fakeStore) RecordFaces(_ context.Context, itemID int64, faces []store.Face) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.refuse[itemID]; err != nil {
		return err
	}
	f.recorded[itemID] = faces
	return nil
}

func (f *fakeStore) MarkFacesDone(_ context.Context, itemID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.done[itemID] = true
	return nil
}

func (f *fakeStore) ClusterLibrary(context.Context, int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clustered++
	return nil
}

func quietWorker(st Store) *Worker {
	return NewWorker(st, &Tool{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

/*
 * The wire format, and a regression for a bug that compiled.
 *
 * The result struct was first written as `X, Y, W, H int` with one struct tag,
 * which Go applies to *all four* fields — so three of them would silently never
 * populate and every face would be recorded at zero size, in the right place,
 * with a valid embedding. Nothing would error and the boxes would all be empty.
 */
func TestEveryCoordinateSurvivesTheWire(t *testing.T) {
	line := `{"path":"C:\\pics\\a.jpg","faces":[{"x":11,"y":22,"w":33,"h":44,` +
		`"score":0.87,"embedding":[0.5,-0.25]}]}`

	var r result
	if err := json.Unmarshal([]byte(line), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Faces) != 1 {
		t.Fatalf("decoded %d faces", len(r.Faces))
	}
	f := r.Faces[0]
	for _, c := range []struct {
		name      string
		got, want int
	}{{"x", f.X, 11}, {"y", f.Y, 22}, {"w", f.W, 33}, {"h", f.H, 44}} {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d — a shared struct tag drops three of four",
				c.name, c.got, c.want)
		}
	}
	if f.Score != 0.87 || len(f.Embedding) != 2 {
		t.Errorf("score %v embedding %d", f.Score, len(f.Embedding))
	}
}

// What was detected reaches the store with the item it belongs to.
func TestFacesAreRecordedAgainstTheirPhotograph(t *testing.T) {
	st := newFakeStore()
	w := quietWorker(st)

	var r result
	_ = json.Unmarshal([]byte(`{"path":"p","faces":[{"x":1,"y":2,"w":3,"h":4,`+
		`"score":0.9,"embedding":[1,0]}]}`), &r)
	w.record(context.Background(), store.Item{ID: 42, Path: "p"}, r)

	got := st.recorded[42]
	if len(got) != 1 {
		t.Fatalf("recorded %d faces against item 42", len(got))
	}
	if got[0].W != 3 || got[0].Score != 0.9 || len(got[0].Embedding) != 2 {
		t.Errorf("recorded %+v", got[0])
	}
	if !st.done[42] {
		t.Error("the photograph was not marked examined")
	}
}

/*
 * A photograph with nobody in it is still examined.
 *
 * Without the marker, a library of landscapes would be re-read on every pass
 * for ever — there would be no way to tell "no faces here" from "not looked at
 * yet".
 */
func TestAPhotographWithNoFacesIsStillMarkedExamined(t *testing.T) {
	st := newFakeStore()
	w := quietWorker(st)

	w.record(context.Background(), store.Item{ID: 7}, result{Path: "p"})

	if !st.done[7] {
		t.Error("a photograph with no faces was left pending for ever")
	}
	if len(st.recorded[7]) != 0 {
		t.Error("faces were recorded for a photograph that had none")
	}
}

/*
 * And so is one that could not be read.
 *
 * A truncated JPEG will not become readable by being tried again immediately.
 * Leaving it unmarked puts it at the front of the queue on every run, so the
 * pass spends its life failing on one file and never reaches the library.
 */
func TestAnUnreadablePhotographIsMarkedRatherThanRetriedForEver(t *testing.T) {
	st := newFakeStore()
	w := quietWorker(st)

	w.record(context.Background(), store.Item{ID: 9}, result{Path: "p", Error: "decode: bad JPEG"})

	if !st.done[9] {
		t.Error("an unreadable photograph was left pending, which loops the pass")
	}
	if w.Stats().Failed != 1 {
		t.Errorf("failed count = %d, want 1", w.Stats().Failed)
	}
}

/*
 * A refusal is not a completion.
 *
 * The likely refusal is a folder marked sensitive while the pass was running.
 * That is not an error and must *not* mark the photograph examined: if the mark
 * is lifted later, it has to be looked at again. Marking it would mean a folder
 * that was briefly private is never indexed, with nothing to say why.
 */
func TestARefusedRecordingDoesNotMarkThePhotographExamined(t *testing.T) {
	st := newFakeStore()
	st.refuse[5] = errRefused{}
	w := quietWorker(st)

	var r result
	_ = json.Unmarshal([]byte(`{"path":"p","faces":[{"x":1,"y":1,"w":1,"h":1,`+
		`"score":0.9,"embedding":[1]}]}`), &r)
	w.record(context.Background(), store.Item{ID: 5, Path: "p"}, r)

	if st.done[5] {
		t.Error("a refused photograph was marked examined, so lifting the mark " +
			"would never cause it to be looked at again")
	}
}

type errRefused struct{}

func (errRefused) Error() string { return "item is in a folder marked sensitive" }

/*
 * Two passes over one library would duplicate every embedding in it.
 *
 * The examined marker is written *after* a photograph is handled, so two
 * concurrent passes are handed the same pending set.
 */
func TestASecondPassIsRefusedWhileOneIsRunning(t *testing.T) {
	st := newFakeStore()
	w := quietWorker(st)

	w.mu.Lock()
	w.running = true
	w.mu.Unlock()

	if err := w.Run(context.Background(), 1); err == nil {
		t.Error("a second face pass started while one was running")
	}
}

/*
 * An unavailable worker stops the pass rather than completing it.
 *
 * Running to the end with no tool would mark the whole library examined and
 * never look at it again — a library reported as having no faces in it because
 * an optional download was missing.
 */
func TestAnUnavailableToolStopsThePass(t *testing.T) {
	st := newFakeStore()
	st.pending = [][]store.Item{{{ID: 1, Path: "p"}}}
	// A Tool pointed at a directory with no binary in it.
	w := NewWorker(st, &Tool{Dir: t.TempDir()},
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	err := w.Run(context.Background(), 1)
	if err == nil {
		t.Fatal("the pass reported success with no worker installed")
	}
	if len(st.done) != 0 {
		t.Error("photographs were marked examined without being examined")
	}
	if st.clustered != 0 {
		t.Error("the library was re-grouped after a pass that did nothing")
	}
}

// The tool reports why it is unavailable rather than only that it is. "No faces
// found" and "nothing looked" are different sentences, and a UI that cannot
// tell them apart teaches people to distrust the feature.
func TestAMissingToolExplainsItself(t *testing.T) {
	tool := &Tool{Dir: t.TempDir()}
	c := tool.Capabilities(context.Background())
	if c.Ready {
		t.Error("reported ready with no binary present")
	}
	if c.Reason == "" {
		t.Error("reported unavailable without saying why")
	}
}
