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

/** Seconds of media to hold before starting, chosen to cover the measured 5s
 *  drought with margin. */
export const PREROLL_SECONDS = 8;

/*
 * How long to wait for that head start before starting anyway.
 *
 * A channel that trickles — a low bitrate, a slow provider, a segment list that
 * updates lazily — may never reach the threshold, and a player that waits for
 * ever is worse than one that stutters. Starting late with a short buffer is the
 * honest fallback: it is what the old behaviour did immediately, so the worst
 * case is no worse than before.
 */
export const PREROLL_DEADLINE_MS = 12_000;

/**
 * shouldStartPlayback decides whether a live element has waited enough.
 *
 * Pure, and separate from the element, because jsdom neither buffers nor plays:
 * the rule is the part worth testing and the wiring is the part worth reading.
 */
export function shouldStartPlayback(
  bufferedAheadSeconds: number,
  waitedMs: number,
): boolean {
  return (
    bufferedAheadSeconds >= PREROLL_SECONDS || waitedMs >= PREROLL_DEADLINE_MS
  );
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
