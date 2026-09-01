package faces

import (
	"testing"
	"time"
)

/*
 * Finishing an install has to invalidate what the tool remembers.
 *
 * Capabilities are cached for a minute because each probe spawns a process, and
 * for a minute that is right — nothing about an installed worker changes on its
 * own. An install is the single moment it does.
 *
 * Reported as: the 113MB download completed and the screen went on saying the
 * runtime could not be loaded. That was a true answer to a question asked while
 * the file was still arriving, held for up to a minute — and held in front of
 * the one person guaranteed to be looking, because they had just watched the
 * progress bar finish.
 *
 * The cache is exercised directly rather than through a probe: probing runs the
 * worker binary, which is absent on a build machine, and what needs asserting
 * is the remembering, not the running.
 */

func TestForgetDropsTheCachedAnswer(t *testing.T) {
	tool := &Tool{}
	stale := Capabilities{Ready: false, Reason: "runtime still downloading"}
	tool.cached, tool.at = &stale, time.Now()

	tool.Forget()

	if tool.cached != nil {
		t.Errorf("still holding %+v", *tool.cached)
	}
	if !tool.at.IsZero() {
		t.Error("the cache timestamp survived, so a stale answer could still be considered fresh")
	}
}

// Forgetting nothing is not an error: an install may finish on a server where
// nobody has asked about the worker yet.
func TestForgetOnAnUnaskedToolIsHarmless(t *testing.T) {
	tool := &Tool{}
	tool.Forget()
	if tool.cached != nil {
		t.Error("invented a cache entry")
	}
}

// And it must be safe to call while other callers are asking, because the
// install goroutine and an HTTP request reach it at the same time.
func TestForgetIsSafeAlongsideReaders(t *testing.T) {
	tool := &Tool{}
	done := make(chan struct{})

	go func() {
		for i := 0; i < 500; i++ {
			tool.Forget()
		}
		close(done)
	}()
	for i := 0; i < 500; i++ {
		tool.mu.Lock()
		_ = tool.cached
		tool.mu.Unlock()
	}
	<-done
}
