package faces

import (
	"context"
	"errors"
	"testing"
)

/*
 * A second search-indexing pass is refused while one runs (T-9).
 *
 * The handler started the pass in a goroutine with no claim, so a second press
 * answered 202 and ran a second pass over the same library, both handed the
 * same pending photographs. The claim is made before Start returns.
 */
func TestASecondIndexingPassIsRefused(t *testing.T) {
	ix := quietIndexer(newFakeEmbedStore(), &Tool{Dir: t.TempDir(), ModelsDir: t.TempDir()})
	if !ix.claim() {
		t.Fatal("a fresh indexer could not be claimed")
	}

	if err := ix.Start(context.Background(), 1, func(error) {}); !errors.Is(err, ErrAlreadyIndexing) {
		t.Errorf("Start while a pass runs = %v, want ErrAlreadyIndexing", err)
	}
	if err := ix.Run(context.Background(), 1); !errors.Is(err, ErrAlreadyIndexing) {
		t.Errorf("Run while a pass runs = %v, want ErrAlreadyIndexing", err)
	}
}

// And the claim is let go when the pass ends, however it ends.
func TestTheIndexerIsFreeAfterAPass(t *testing.T) {
	ix := quietIndexer(newFakeEmbedStore(), &Tool{Dir: t.TempDir(), ModelsDir: t.TempDir()})

	done := make(chan error, 1)
	if err := ix.Start(context.Background(), 1, func(err error) { done <- err }); err != nil {
		t.Fatalf("Start = %v", err)
	}
	if err := <-done; err == nil {
		t.Fatal("a pass with no worker installed reported success")
	}
	if !ix.claim() {
		t.Error("the indexer stayed claimed after a pass that refused to run")
	}
}
