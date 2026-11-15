package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestTheOptimizedQueue(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	add := func(title string) int64 {
		id, err := st.UpsertItem(ctx, file(lib.ID, filepath.Join(lib.Path, title+".mkv"), title))
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	a, b := add("Alpha"), add("Bravo")

	if n, err := st.RequestOptimized(ctx, []int64{a, b}); err != nil || n != 2 {
		t.Fatalf("request = %d, %v", n, err)
	}
	// The oldest request runs first; same-second requests by id.
	next, err := st.NextOptimized(ctx)
	if err != nil || next.ItemID != a || next.Title != "Alpha" {
		t.Fatalf("next = %+v, %v", next, err)
	}

	// Ready: asking again changes nothing, and the copy is found.
	if ok, _ := st.MarkOptimized(ctx, a, OptimizedReady, 1000, `{"streams":[]}`, ""); !ok {
		t.Fatal("mark ready did not apply")
	}
	if n, _ := st.RequestOptimized(ctx, []int64{a}); n != 0 {
		t.Error("asking again for a ready copy re-queued it")
	}
	ready, err := st.ReadyOptimized(ctx, a)
	if err != nil || ready.SizeBytes != 1000 || ready.ProbeJSON == "" {
		t.Errorf("ready = %+v, %v", ready, err)
	}
	if total, _ := st.OptimizedBytes(ctx); total != 1000 {
		t.Errorf("bytes = %d", total)
	}

	// Failed: asking again queues it again.
	_, _ = st.MarkOptimized(ctx, b, OptimizedFailed, 0, "", "ffmpeg said no")
	if n, _ := st.RequestOptimized(ctx, []int64{b}); n != 1 {
		t.Error("asking again after a failure did not re-queue")
	}

	// Removed while converting stays removed: the worker's late "ready" is
	// refused rather than resurrecting the row.
	_, _ = st.MarkOptimized(ctx, b, OptimizedConverting, 0, "", "")
	if err := st.RemoveOptimized(ctx, []int64{b}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := st.MarkOptimized(ctx, b, OptimizedReady, 5, "{}", ""); ok {
		t.Error("a removed copy came back when its conversion finished")
	}
	if _, err := st.NextOptimized(ctx); !errors.Is(err, ErrNotFound) {
		t.Errorf("queue after removal err = %v, want empty", err)
	}

	// A restart puts an interrupted conversion back in the queue.
	_, _ = st.RequestOptimized(ctx, []int64{b})
	_, _ = st.MarkOptimized(ctx, b, OptimizedConverting, 0, "", "")
	if err := st.ResumeOptimized(ctx); err != nil {
		t.Fatal(err)
	}
	if next, err := st.NextOptimized(ctx); err != nil || next.ItemID != b {
		t.Errorf("after resume next = %+v, %v", next, err)
	}
}

// RequeueOptimized moves only a ready copy (R03-2). A converting one is the
// worker's, and putting it back in the queue would start the same job twice.
func TestRequeueMovesOnlyAReadyCopy(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	add := func(title string) int64 {
		id, err := st.UpsertItem(ctx, file(lib.ID, filepath.Join(lib.Path, title+".mkv"), title))
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	ready, busy := add("Ready"), add("Busy")
	if _, err := st.RequestOptimized(ctx, []int64{ready, busy}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MarkOptimized(ctx, ready, OptimizedReady, 500, `{"format":{}}`, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MarkOptimized(ctx, busy, OptimizedConverting, 0, "", ""); err != nil {
		t.Fatal(err)
	}

	if moved, err := st.RequeueOptimized(ctx, busy); err != nil || moved {
		t.Errorf("requeue a converting copy = %v, %v; want left alone", moved, err)
	}
	if moved, err := st.RequeueOptimized(ctx, ready); err != nil || !moved {
		t.Fatalf("requeue a ready copy = %v, %v", moved, err)
	}
	if _, err := st.ReadyOptimized(ctx, ready); !errors.Is(err, ErrNotFound) {
		t.Errorf("after requeue the copy still reads as ready: %v", err)
	}
	if n, _ := st.OptimizedBytes(ctx); n != 0 {
		t.Errorf("space used after requeue = %d, want 0", n)
	}
	next, err := st.NextOptimized(ctx)
	if err != nil || next.ItemID != ready {
		t.Errorf("next in the queue = %+v, %v; want the re-queued copy", next, err)
	}
	if moved, _ := st.RequeueOptimized(ctx, ready); moved {
		t.Error("a second requeue moved a row that was already queued")
	}
}
