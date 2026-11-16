package subtitle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// slowProduce stands in for ffmpeg reading a big container: it finishes when
// released, and gives up if its context ends first, as the process would.
func slowProduce(release <-chan struct{}, calls *atomic.Int32) func(context.Context) ([]byte, error) {
	return func(ctx context.Context) ([]byte, error) {
		calls.Add(1)
		select {
		case <-release:
			return []byte("WEBVTT\n\n00:01.000 --> 00:02.000\nhello\n"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

/*
 * An extraction outlives the request that started it (R09-11).
 *
 * Extracting a track from a big remux reads the whole file, which takes
 * minutes. Tied to the request, it died with the request (or at a flat 60 s),
 * nothing was cached, and every later attempt started again from nothing, so
 * the film never had subtitles. Now the request can give up and the
 * extraction still lands in the cache, without running a second time.
 */
func TestAnExtractionOutlivesTheRequestThatStartedIt(t *testing.T) {
	e := NewExtractor(t.TempDir())
	src := filepath.Join(t.TempDir(), "remux.mkv")
	if err := os.WriteFile(src, []byte("container"), 0o644); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	var calls atomic.Int32
	produce := slowProduce(release, &calls)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := e.cached(ctx, src, "embedded-2", produce); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the impatient request = %v, want its own deadline", err)
	}

	// A second request while it runs waits on the same extraction.
	var wg sync.WaitGroup
	wg.Add(1)
	var second []byte
	var secondErr error
	go func() {
		defer wg.Done()
		second, secondErr = e.cached(context.Background(), src, "embedded-2", produce)
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if secondErr != nil || len(second) == 0 {
		t.Fatalf("the waiting request = %q, %v", second, secondErr)
	}

	// And from now on it is served from the cache.
	body, err := e.cached(context.Background(), src, "embedded-2", func(context.Context) ([]byte, error) {
		t.Error("extracted again although the first one finished")
		return nil, errors.New("no")
	})
	if err != nil || len(body) == 0 {
		t.Fatalf("cached = %q, %v", body, err)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("ffmpeg ran %d times, want once", n)
	}
}

// A big file is given time to be read whole; a small one keeps the base
// limit that guards against a hung track.
func TestABigFileIsGivenTimeToBeRead(t *testing.T) {
	base := 60 * time.Second
	if got := extractTimeout(base, 300<<20); got != base {
		t.Errorf("a 300 MB file = %v, want the base %v", got, base)
	}
	got := extractTimeout(base, 50<<30)
	if got < 50*time.Minute {
		t.Errorf("a 50 GB remux = %v; at 150 MB/s it needs over five minutes, and slower disks far more", got)
	}
}
