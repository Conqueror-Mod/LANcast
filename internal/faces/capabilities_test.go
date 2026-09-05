package faces

import (
	"context"
	"sync"
	"testing"
	"time"
)

/*
 * Asking what the worker can do, without asking it six times.
 *
 * THIS SHIPPED, AND IT IS THE WORST KIND OF REGRESSION
 *
 * A probe used to load two small face models. Since semantic search it also
 * loads 600MB of CLIP, so it costs 2.6s and about 700MB — and Capabilities
 * released its lock before probing, so every caller got its own subprocess.
 * Opening pages that ask spawned several at once, the machine stopped
 * answering, and the client reported `Failed to fetch` on whatever was in
 * flight. Nothing in the server log said a word, because nothing had failed:
 * the server was simply too busy to answer, once a minute, for ever.
 *
 * The tests below count probes rather than timing anything, because a timing
 * assertion would be flaky on a busy machine and the fault was never really
 * about time — it was about a cost being multiplied by its callers.
 */

func TestConcurrentCallersShareOneProbe(t *testing.T) {
	tool, counter := fakeWorker(t, "serve")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c := tool.Capabilities(context.Background()); !c.Ready {
				t.Error("a caller got a not-ready answer from a ready worker")
			}
		}()
	}
	wg.Wait()

	if n := starts(t, counter); n != 1 {
		t.Errorf("eight callers started %d probes, want 1 — each one is 2.6s "+
			"and 700MB, and that multiplier is what made the app unusable", n)
	}
}

/*
 * A warm cache never probes at all, and a stale one answers without waiting.
 *
 * Readiness only changes when models are installed or removed, and Forget is
 * called on exactly that event — so between those events the previous answer is
 * not merely probably right, it is right. Blocking a page load to re-confirm it
 * buys nothing.
 */
func TestAWarmCacheDoesNotProbe(t *testing.T) {
	tool, counter := fakeWorker(t, "serve")

	tool.Capabilities(context.Background())
	for i := 0; i < 20; i++ {
		tool.Capabilities(context.Background())
	}

	if n := starts(t, counter); n != 1 {
		t.Errorf("twenty-one calls started %d probes, want 1", n)
	}
}

/*
 * A stale answer is served immediately while a fresh one is fetched behind it.
 *
 * Asserted by the answer arriving at all with the cache deliberately aged: the
 * refresh is asynchronous, so a caller that had to wait for it would be
 * indistinguishable from one that did not, except in the thing that actually
 * hurt — a page load stalling for 2.6s once a minute.
 */
func TestAStaleAnswerIsServedWhileItRefreshes(t *testing.T) {
	tool, _ := fakeWorker(t, "serve")

	tool.Capabilities(context.Background())

	tool.mu.Lock()
	tool.at = time.Now().Add(-capsFresh - time.Minute)
	tool.mu.Unlock()

	done := make(chan Capabilities, 1)
	go func() { done <- tool.Capabilities(context.Background()) }()
	select {
	case c := <-done:
		if !c.Ready {
			t.Error("the stale answer was not the one previously cached")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a stale cache blocked the caller; it should answer now and " +
			"refresh behind")
	}
}

/*
 * A probe that was already running when the models changed does not get to
 * write its answer down.
 *
 * An install finishing calls Forget, and a probe started before it finished saw
 * a half-downloaded directory. Caching that would show "not installed" over a
 * completed install for ten minutes — precisely what somebody watching the
 * progress bar would be left reading, and the failure Forget exists to prevent.
 */
func TestForgetDiscardsAProbeThatWasAlreadyRunning(t *testing.T) {
	tool, _ := fakeWorker(t, "serve")

	tool.mu.Lock()
	tool.startProbeLocked()
	wait := tool.inflight
	tool.mu.Unlock()

	tool.Forget()
	<-wait

	tool.mu.Lock()
	cached := tool.cached
	tool.mu.Unlock()
	if cached != nil {
		t.Error("a probe from before the models changed was cached anyway")
	}
}
