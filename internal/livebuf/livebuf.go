/*
 * Package livebuf smooths a live stream that arrives in bursts.
 *
 * # Why this exists
 *
 * A live channel is relayed straight from the provider: ffmpeg pulls a segment
 * as fast as the far end serves it, and LANcast writes those bytes onward the
 * moment they appear. So the provider's publishing rhythm reaches the browser
 * verbatim, and measured at our own endpoint that rhythm is brutal:
 *
 *	chunks 367   5.73 MB in 42.5s
 *	gap median      0 ms
 *	gap p90         5 ms
 *	gap p99      5,326 ms
 *	gap max      9,850 ms
 *	silences over 1s: 7, totalling 41.7s of the 42s window
 *
 * Ninety-eight percent of the window is silence between bursts. Every client
 * cushion in `web/src/lib/preroll.ts` exists to survive those holes, and every
 * one of its constants is a guess about a stranger's segment interval — a guess
 * that was wrong by half until it was measured, because the number lives on the
 * other side of somebody else's CDN and can change without warning.
 *
 * This moves the problem to the side that can actually see it. The server reads
 * ahead into a small buffer and hands it out at the rate the stream actually
 * runs at, so a ten-second upstream silence is absorbed here instead of
 * reaching the decoder.
 *
 * # What it costs, stated plainly
 *
 * Latency. The lead below is added to how far behind live every viewer is, and
 * that is the whole trade: a jitter buffer buys continuity with delay, and for
 * live television that is worth it. Nobody watching a channel can tell whether
 * they are eight or eighteen seconds behind the broadcast; everybody can tell
 * when it stops.
 *
 * # What it deliberately does not do
 *
 * It does not parse the stream. Pacing is by *bytes at the observed rate*, not
 * by decoding timestamps — keeping this a byte pipe means it cannot corrupt a
 * container, cannot disagree with the muxer about what a second is, and works
 * unchanged if the output format ever changes. The cost is that the rate is
 * learned rather than known, which the estimator below is built around.
 */
package livebuf

import (
	"errors"
	"io"
	"sync"
	"time"
)

const (
	// DefaultLead is how much media to accumulate before the first byte is
	// released.
	//
	// Past the 9,850ms worst silence measured above once the client's own small
	// cushion is added to it: a lead shorter than the hole is a lead that does
	// not cover the thing it exists for, and that mistake has already been made
	// once on the client side.
	//
	// It is the dominant part of how long a channel takes to start, so it is
	// not free — but it is spent once, and the alternative was spending it
	// again at every silence for as long as somebody watched.
	DefaultLead = 8 * time.Second

	// DefaultMax caps the buffer so a stalled reader cannot grow it without
	// bound. At a typical 1-2 Mbps channel this is roughly a minute of media,
	// which is far more slack than the jitter needs and still small enough that
	// a handful of viewers cannot exhaust memory.
	DefaultMax = 16 << 20

	// warmup is how long to watch the stream before trusting the rate estimate.
	// Below this the average is dominated by the first burst, which arrives at
	// network speed and describes nothing.
	warmup = 2 * time.Second

	// tick bounds how long a paced read will sleep before looking again, so a
	// closing stream is noticed promptly rather than after a full allowance.
	tick = 50 * time.Millisecond
)

// Options configure a Buffer. The zero value is usable and means the defaults.
type Options struct {
	// Lead is how much media to hold before serving the first byte.
	Lead time.Duration
	// Max is the byte ceiling for the buffer.
	Max int
	// now and sleep exist so the pacing can be tested without waiting for it.
	now   func() time.Time
	sleep func(time.Duration)
}

func (o Options) withDefaults() Options {
	if o.Lead <= 0 {
		o.Lead = DefaultLead
	}
	if o.Max <= 0 {
		o.Max = DefaultMax
	}
	if o.now == nil {
		o.now = time.Now
	}
	if o.sleep == nil {
		o.sleep = time.Sleep
	}
	return o
}

/*
 * Buffer reads ahead from a live source and serves it at the stream's own rate.
 *
 * It is an io.ReadCloser so the caller's copy loop does not change: the handler
 * still reads and writes, and the smoothing is invisible to it.
 */
type Buffer struct {
	src  io.ReadCloser
	opts Options

	mu     sync.Mutex
	cond   *sync.Cond
	queue  []byte
	closed bool
	srcErr error

	// Rate estimation. total counts every byte the source has produced, which
	// is what makes the average a property of the stream rather than of what
	// the client has managed to collect.
	start time.Time
	total int64

	released bool      // has the lead been met and serving begun
	lastRead time.Time // when the pacer last handed bytes out
	credit   float64   // fractional bytes carried between reads
}

// New starts reading from src in the background and returns the smoothed
// stream. Closing the Buffer closes src.
func New(src io.ReadCloser, opts Options) *Buffer {
	b := &Buffer{src: src, opts: opts.withDefaults()}
	b.cond = sync.NewCond(&b.mu)
	go b.fill()
	return b
}

// fill drains the source as fast as it will produce, which is the entire point:
// the bursts are absorbed here so they are not passed on.
func (b *Buffer) fill() {
	buf := make([]byte, 64<<10)
	for {
		n, err := b.src.Read(buf)
		if n > 0 {
			b.mu.Lock()
			if b.start.IsZero() {
				b.start = b.opts.now()
			}
			// Backpressure rather than unbounded growth. A full buffer means
			// the client is slower than the channel, and blocking here lets
			// TCP say so instead of this process absorbing the difference.
			for len(b.queue) >= b.opts.Max && !b.closed {
				b.cond.Wait()
			}
			if b.closed {
				b.mu.Unlock()
				return
			}
			b.queue = append(b.queue, buf[:n]...)
			b.total += int64(n)
			b.cond.Broadcast()
			b.mu.Unlock()
		}
		if err != nil {
			b.mu.Lock()
			b.srcErr = err
			b.cond.Broadcast()
			b.mu.Unlock()
			return
		}
	}
}

/*
 * rateLocked is the stream's observed byte rate.
 *
 * A plain average over the whole stream rather than a moving window, and that
 * is deliberate: a live channel has one true bitrate, and a window narrow
 * enough to react to a burst would report the burst — tens of megabits — as the
 * rate, drain the buffer at that speed, and undo the smoothing exactly when it
 * is needed. Slow to react is the correct behaviour when the thing being
 * measured does not change.
 *
 * Returns 0 before there is enough history to say anything, which the caller
 * treats as "do not pace yet".
 */
func (b *Buffer) rateLocked() float64 {
	if b.start.IsZero() {
		return 0
	}
	elapsed := b.opts.now().Sub(b.start)
	if elapsed < warmup {
		return 0
	}
	return float64(b.total) / elapsed.Seconds()
}

// leadMetLocked reports whether enough media has accumulated to start serving.
//
// Before a rate is known the lead is judged by time rather than bytes: waiting
// for `Lead` of wall clock collects `Lead` of media, because a live source
// produces in real time by definition.
func (b *Buffer) leadMetLocked() bool {
	if b.released {
		return true
	}
	if b.srcErr != nil {
		return true // nothing more is coming; serve what there is
	}
	if b.start.IsZero() {
		return false
	}
	/*
	 * Either measure is enough, and that is not belt-and-braces.
	 *
	 * The byte form compares the queue against `rate * lead`, where the rate was
	 * itself derived by dividing those same bytes by that same interval — so on
	 * the exact boundary the product can land one floating-point step *above*
	 * the queue it came from and the condition never becomes true. On a source
	 * that has gone quiet, more bytes are not coming to break the tie, and the
	 * stream never starts.
	 *
	 * Waiting `lead` of wall clock on a live source collects `lead` of media by
	 * definition, so the time form is a complete answer on its own.
	 */
	if b.opts.now().Sub(b.start) >= b.opts.Lead {
		return true
	}
	if rate := b.rateLocked(); rate > 0 {
		return float64(len(b.queue)) >= rate*b.opts.Lead.Seconds()
	}
	return false
}

/*
 * Read hands out bytes at the stream's own rate.
 *
 * The allowance is what the rate would have produced since the last read, plus
 * anything carried over. Two escapes keep it from being a straitjacket:
 *
 *   - Above the high-water mark the buffer drains without pacing. Holding more
 *     than the lead is latency nobody asked for, so a burst that overshoots is
 *     given back rather than kept.
 *   - Before the rate is known, and once the source has ended, bytes flow
 *     freely. Pacing on a guess is worse than not pacing.
 */
func (b *Buffer) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for {
		if b.closed {
			return 0, io.ErrClosedPipe
		}

		if !b.leadMetLocked() {
			/*
			 * Polled, not waited on. Before a rate is known the lead is a
			 * condition on the *clock*, and time passing is not an event
			 * anything can broadcast — a cond.Wait here sleeps until more data
			 * arrives, which on a source that has just gone quiet for ten
			 * seconds is precisely never.
			 */
			b.mu.Unlock()
			b.opts.sleep(tick)
			b.mu.Lock()
			continue
		}
		if !b.released {
			b.released = true
			/*
			 * Back-date the pacing clock by one tick so the first read after
			 * the lead is met actually carries bytes.
			 *
			 * Starting it at `now` means zero elapsed, zero credit, and a read
			 * that returns nothing and sleeps — a stutter at the exact moment
			 * the stream is meant to begin, after having deliberately waited
			 * several seconds to have plenty in hand.
			 */
			b.lastRead = b.opts.now().Add(-tick)
		}

		if len(b.queue) == 0 {
			if b.srcErr != nil {
				if errors.Is(b.srcErr, io.EOF) {
					return 0, io.EOF
				}
				return 0, b.srcErr
			}
			b.waitLocked()
			continue
		}

		n := b.allowanceLocked(len(p))
		if n == 0 {
			// Not enough has accrued yet. Sleeping with the lock released lets
			// the filler keep working while the pacer waits.
			b.mu.Unlock()
			b.opts.sleep(tick)
			b.mu.Lock()
			continue
		}

		copy(p, b.queue[:n])
		b.queue = b.queue[n:]
		b.cond.Broadcast() // the filler may be blocked on a full buffer
		return n, nil
	}
}

// allowanceLocked decides how many bytes may go out now.
func (b *Buffer) allowanceLocked(max int) int {
	queued := len(b.queue)
	if max > queued {
		max = queued
	}

	rate := b.rateLocked()
	if rate <= 0 || b.srcErr != nil {
		// No estimate yet, or the stream has ended and there is nothing left to
		// smooth. Either way, pacing would be a guess.
		b.lastRead = b.opts.now()
		return max
	}

	// Anything beyond the lead is surplus latency; give it back at once.
	if high := int(rate * b.opts.Lead.Seconds()); queued > high {
		b.lastRead = b.opts.now()
		b.credit = 0
		return max
	}

	now := b.opts.now()
	b.credit += rate * now.Sub(b.lastRead).Seconds()
	b.lastRead = now

	n := int(b.credit)
	if n <= 0 {
		return 0
	}
	if n > max {
		n = max
	}
	b.credit -= float64(n)
	return n
}

// waitLocked blocks until something changes, without holding the CPU.
func (b *Buffer) waitLocked() {
	b.cond.Wait()
}

// Close releases the source and wakes anything waiting on this buffer.
func (b *Buffer) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	b.cond.Broadcast()
	b.mu.Unlock()
	return b.src.Close()
}

// Buffered reports how many bytes are held, for logging and tests.
func (b *Buffer) Buffered() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.queue)
}
