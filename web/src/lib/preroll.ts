/*
 * How long a live channel buffers before it starts playing.
 *
 * Measured on a real IPTV source, reading the response body directly and timing
 * every chunk over twenty seconds:
 *
 *	chunks 376   total 2.68 MB
 *	gap median   0 ms
 *	gap p90      3 ms
 *	gap max  5,071 ms      ← five seconds with no bytes at all
 *
 * Bytes arrive in tight bursts separated by multi-second silences, which is what
 * HLS segment pacing looks like from the far end: ffmpeg pulls a segment as fast
 * as the network allows, then waits for the next one to be published. The server
 * copies video through unchanged, so it relays that pacing verbatim — and this
 * is upstream of anything LANcast can fix. The provider decides when a segment
 * exists.
 *
 * What *is* ours is when playback starts. A plain `<video>` has no live buffer
 * policy: `canplay` fires at HAVE_FUTURE_DATA, which on a bursty source means
 * "the first burst arrived", so it began playing with under a second in hand and
 * ran dry at every silence. Stutter, every few seconds, for ever.
 *
 * Waiting for a head start absorbs the silence. The cost is a slower start; the
 * benefit is that a five-second drought never reaches the decoder.
 *
 * Deliberately not adaptive. Measuring the gap pattern per channel before
 * choosing a threshold sounds better and is worse: it means several seconds of
 * measurement before deciding whether to wait, which is the very delay it would
 * be trying to avoid, and it tunes against the last few seconds of a network
 * that changes.
 */

/*
 * Seconds of media to hold before starting.
 *
 * Was 8, chosen against a 5s drought measured at the provider. Re-measured at
 * *our own endpoint*, which is what the element actually experiences, the
 * droughts are twice that:
 *
 *	chunks 367   5.73 MB in 42.5s
 *	gap median      0 ms
 *	gap p90         5 ms
 *	gap p99      5,326 ms
 *	gap max      9,850 ms      <- longer than the cushion meant to absorb it
 *	silences over 1s: 7, totalling 41.7s of the 42s window
 *
 * A head start shorter than the silence it exists to cover is not a head start.
 * Bytes arrive in tight bursts — a segment publishes, we relay it verbatim, and
 * then nothing for up to ten seconds — so the cushion has to outlast the worst
 * silence or playback reaches the end of it every single time.
 *
 * The cost is a slower start, and it is the right trade: live TV that takes a
 * few more seconds to begin and then runs is worth more than one that begins
 * sooner and stutters for as long as you watch it.
 *
 * **That fix now exists**, which is why this number came back down. The server
 * absorbs the burstiness in `internal/livebuf` and hands the stream out at its
 * own rate, so the hole never reaches here and this cushion no longer has to be
 * bigger than a stranger's segment interval. It covers ordinary jitter — a slow
 * frame, a scheduling hiccup — and the server covers the silences.
 *
 * Which is also why it is small again: the two cushions add up, and the wait
 * before a channel starts is the sum of both. Paying for the same protection
 * twice would double the startup for nothing.
 */
export const PREROLL_SECONDS = 3;

/*
 * How long to wait for that head start before starting anyway.
 *
 * A channel that trickles — a low bitrate, a slow provider, a segment list that
 * updates lazily — may never reach the threshold, and a player that waits for
 * ever is worse than one that stutters. Starting late with a short buffer is the
 * honest fallback: it is what the old behaviour did immediately, so the worst
 * case is no worse than before.
 */
export const PREROLL_DEADLINE_MS = 8_000;

/*
 * Seconds to hold before resuming after the cushion has *already* run dry.
 *
 * Larger than the head start, and that asymmetry is the point. Starting and
 * recovering are not the same problem: at start, nothing is known about the
 * channel and the cost of guessing low is a few seconds of stutter. After a
 * drought, one thing is known for certain — this channel just ran out — and
 * the cost of guessing low is running out again immediately.
 *
 * ExoPlayer draws the same line and steeper: 1,000 ms to begin against 5,000 ms
 * to resume after a rebuffer. The shape is what transfers, not the numbers.
 * Their start value is low because a real player skips gaps and holds 30-60s
 * behind it; a bare element does neither, and *one second in hand* is the exact
 * condition measured at the top of this file as the original bug. So the head
 * start stays where measurement put it and only the resume threshold moves.
 *
 * It also buys hysteresis, which was missing. `shouldHold` releases below
 * PREROLL_SECONDS and, before this, `shouldStartPlayback` resumed at the same
 * value — one boundary, tested from both sides. A channel sitting near it holds
 * at 2.9s, resumes at 3.0s, has that 3.0s eaten by the play head within three
 * seconds, and holds again: a pause every few seconds, each one correct by the
 * rule and wrong by the outcome. A gap between the two thresholds means
 * recovering costs more than falling did, so a recovery has to be real to count.
 * `liveEdge.ts` pairs MAX_LAG_SECONDS with SETTLED_LAG_SECONDS for exactly this
 * reason.
 */
export const REBUFFER_SECONDS = 5;

/**
 * shouldStartPlayback decides whether a live element has waited enough.
 *
 * `afterDrought` distinguishes resuming from starting: a player that has run
 * dry once has to show more in hand than a player that has never played, per
 * REBUFFER_SECONDS. It is not "was this triggered by a `waiting` event" —
 * `waiting` fires during the initial fill too — but "has this element ever
 * played", which is the thing that makes the channel's behaviour known rather
 * than guessed.
 *
 * The deadline does not move with it. It is the escape hatch for a channel that
 * will never reach any threshold, and it answers the same question in both
 * cases: how long is too long to show somebody nothing.
 *
 * Pure, and separate from the element, because jsdom neither buffers nor plays:
 * the rule is the part worth testing and the wiring is the part worth reading.
 */
export function shouldStartPlayback(
  bufferedAheadSeconds: number,
  waitedMs: number,
  afterDrought = false,
): boolean {
  const want = afterDrought ? REBUFFER_SECONDS : PREROLL_SECONDS;
  return bufferedAheadSeconds >= want || waitedMs >= PREROLL_DEADLINE_MS;
}

/**
 * shouldHold decides whether a `waiting` event is evidence of an actual
 * drought.
 *
 * It usually is not. Measured against a real channel in Chrome, `waiting` fired
 * **113 times in 135 seconds** — about once a second — while the element held
 * between 117 and 142 seconds of buffered media. Pausing on each one left the
 * player **paused 28% of wall time** with a median of 131 seconds in hand, and
 * dragged playback to 0.76x real time. That is the stutter, the drift that no
 * catch-up could outrun, and almost certainly the choppy audio: an element
 * paused and resumed fifty times a minute cannot sound right.
 *
 * `waiting` means "I cannot render the next frame right now", which on a
 * progressive stream fires at fragment boundaries and on any brief hiccup. It
 * is not a measurement of how much media is in hand — and the buffer is, so
 * the buffer is what decides.
 *
 * Holding is still correct when the cushion is genuinely gone; that is what
 * this whole file is for.
 *
 * This is the lower edge of the band described on REBUFFER_SECONDS: hold below
 * PREROLL_SECONDS, resume above REBUFFER_SECONDS. It stays at the *lower* value
 * deliberately — raising it would start pausing a player that is merely running
 * thin, which is the mistake `d26f552` was written to undo.
 */
export function shouldHold(bufferedAheadSeconds: number): boolean {
  return bufferedAheadSeconds < PREROLL_SECONDS;
}

/**
 * bufferedAhead is how much media sits between the play head and the end of what
 * has arrived.
 *
 * The last range rather than the first: a live stream that has been running has
 * one growing range, and if it ever has more than one, the end of the last is
 * where the incoming data is.
 */
export function bufferedAhead(el: {
  buffered: TimeRanges;
  currentTime: number;
}): number {
  const n = el.buffered.length;
  if (n === 0) return 0;
  return Math.max(0, el.buffered.end(n - 1) - el.currentTime);
}
