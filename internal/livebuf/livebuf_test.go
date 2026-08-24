package livebuf

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

/*
 * A hand-driven clock, so pacing is asserted exactly rather than slept through.
 * Sleeping advances it, which is what makes a paced read testable at all: the
 * code under test believes time passed, and the test knows precisely how much.
 */
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock {
	return &clock{t: time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)}
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

/*
 * Deliberately does NOT advance the clock.
 *
 * The pacer polls while it waits, so a sleep that moved virtual time would let
 * it race to any deadline in microseconds and no test could observe it holding
 * anything. Time moves only when a test says so; this just yields.
 */
func (c *clock) sleep(time.Duration) { time.Sleep(time.Millisecond) }

func (c *clock) opts(lead time.Duration) Options {
	return Options{Lead: lead, now: c.now, sleep: c.sleep}
}

/*
 * A source that delivers in bursts separated by silence — the shape measured at
 * our own endpoint, where 98% of a 42-second window was silence and the longest
 * hole was 9,850ms.
 */
type bursty struct {
	mu     sync.Mutex
	chunks [][]byte
	closed bool
	gate   chan struct{}
}

func newBursty() *bursty { return &bursty{gate: make(chan struct{}, 1024)} }

// publish makes one burst available to the reader.
func (b *bursty) publish(p []byte) {
	b.mu.Lock()
	b.chunks = append(b.chunks, append([]byte(nil), p...))
	b.mu.Unlock()
	b.gate <- struct{}{}
}

func (b *bursty) Read(p []byte) (int, error) {
	<-b.gate
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, io.EOF
	}
	if len(b.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b.chunks[0])
	if n < len(b.chunks[0]) {
		b.chunks[0] = b.chunks[0][n:]
		b.gate <- struct{}{}
	} else {
		b.chunks = b.chunks[1:]
	}
	return n, nil
}

func (b *bursty) Close() error {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	close(b.gate)
	return nil
}

// drain reads until EOF, returning everything.
func drain(t *testing.T, r io.Reader) []byte {
	t.Helper()
	out, err := io.ReadAll(r)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("read: %v", err)
	}
	return out
}

func TestPassesEveryByteThrough(t *testing.T) {
	want := bytes.Repeat([]byte("live"), 4096)
	b := New(io.NopCloser(bytes.NewReader(want)), Options{})
	defer b.Close()

	got := drain(t, b)
	if !bytes.Equal(got, want) {
		t.Fatalf("stream changed: got %d bytes, want %d", len(got), len(want))
	}
}

/*
 * The lead is the whole feature: nothing goes out until enough is held to
 * survive a silence. Serving the first burst immediately is what the old
 * pass-through did, and it is why a ten-second hole reached the decoder.
 */
func TestHoldsTheLeadBeforeServing(t *testing.T) {
	c := newClock()
	src := newBursty()
	b := New(src, c.opts(6*time.Second))
	defer b.Close()

	src.publish(bytes.Repeat([]byte("x"), 1000))
	// Give the filler a moment of real time to take it.
	waitFor(t, func() bool { return b.Buffered() > 0 })

	done := make(chan int, 1)
	go func() {
		p := make([]byte, 512)
		n, _ := b.Read(p)
		done <- n
	}()

	select {
	case n := <-done:
		t.Fatalf("served %d bytes before the lead was met", n)
	case <-time.After(150 * time.Millisecond):
		// Correct: still holding.
	}

	// Once the lead's worth of wall clock has passed, it serves.
	c.advance(6 * time.Second)
	select {
	case n := <-done:
		if n == 0 {
			t.Fatal("served nothing after the lead was met")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("still holding after the lead was met")
	}
}

/*
 * The point of the whole package, as a test: a silence longer than anything the
 * client could ride out must not reach the client.
 *
 * A burst arrives, then nothing for ten seconds — the measured worst case. With
 * a lead in hand the reader keeps being served throughout, which is exactly
 * what a pass-through cannot do.
 */
func TestServesThroughATenSecondSilence(t *testing.T) {
	c := newClock()
	src := newBursty()
	b := New(src, c.opts(4*time.Second))
	defer b.Close()

	// A burst arrives, then the source goes quiet. Sized well past what the
	// reads below consume, because a queue drained to exactly empty would block
	// on the last read and the test would hang rather than fail.
	src.publish(bytes.Repeat([]byte("a"), 200<<10))
	waitFor(t, func() bool { return b.Buffered() == 200<<10 })
	c.advance(4 * time.Second) // past warmup, and the lead is met

	served := 0
	p := make([]byte, 4096)
	// Ten seconds of silence, read second by second. Every one of them must
	// produce bytes.
	for i := 0; i < 10; i++ {
		n, err := b.Read(p)
		if err != nil {
			t.Fatalf("read %d during silence: %v", i, err)
		}
		if n == 0 {
			t.Fatalf("read %d served nothing during the silence", i)
		}
		served += n
		c.advance(time.Second)
	}
	if served == 0 {
		t.Fatal("nothing was served across the silence")
	}
}

/*
 * Pacing must not become hoarding. A burst that overshoots the lead is surplus
 * latency, and latency nobody asked for is the cost this package is spending —
 * so it is given back rather than kept.
 */
func TestGivesBackSurplusRatherThanHoardingIt(t *testing.T) {
	c := newClock()
	src := newBursty()
	b := New(src, c.opts(2*time.Second))
	defer b.Close()

	src.publish(bytes.Repeat([]byte("a"), 10<<10))
	waitFor(t, func() bool { return b.Buffered() == 10<<10 })
	c.advance(4 * time.Second) // rate ~2.5KB/s, lead ~5KB, so 10KB is surplus

	p := make([]byte, 64<<10)
	n, err := b.Read(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Above the high-water mark it drains without pacing, so a single read
	// takes everything it can rather than a rate-limited sliver.
	if n < 4<<10 {
		t.Errorf("served %d bytes of a 10KB surplus; it is being hoarded", n)
	}
}

// A source that ends must end here too, with everything it produced.
func TestEndOfStreamDrainsThenEOF(t *testing.T) {
	want := bytes.Repeat([]byte("z"), 8192)
	b := New(io.NopCloser(bytes.NewReader(want)), Options{Lead: time.Millisecond})
	defer b.Close()

	got := drain(t, b)
	if len(got) != len(want) {
		t.Fatalf("lost data at EOF: got %d, want %d", len(got), len(want))
	}
}

// A dead channel produces nothing and must not hang the caller for the lead.
func TestSourceErrorIsNotHeldForTheLead(t *testing.T) {
	want := errors.New("upstream refused")
	b := New(io.NopCloser(errReader{want}), Options{Lead: time.Hour})
	defer b.Close()

	p := make([]byte, 16)
	_, err := b.Read(p)
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

// Closing must free a reader that is waiting, or a viewer who leaves mid-hold
// keeps a goroutine and an ffmpeg alive behind them.
func TestCloseWakesAWaitingReader(t *testing.T) {
	c := newClock()
	src := newBursty()
	b := New(src, c.opts(time.Hour))

	done := make(chan error, 1)
	go func() {
		p := make([]byte, 16)
		_, err := b.Read(p)
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	b.Close()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("read returned success after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not wake the waiting reader")
	}
}

// The buffer is a ceiling, not a suggestion: a client slower than the channel
// must not make this process grow without bound.
func TestBufferIsCapped(t *testing.T) {
	src := newBursty()
	b := New(src, Options{Lead: time.Hour, Max: 8 << 10})
	defer b.Close()

	for i := 0; i < 8; i++ {
		src.publish(bytes.Repeat([]byte("a"), 4<<10))
	}
	// Nothing is read, so the filler must block at the cap rather than absorb
	// all of it.
	time.Sleep(200 * time.Millisecond)
	if got := b.Buffered(); got > 12<<10 {
		t.Errorf("buffered %d bytes with an 8KB cap", got)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
