/*
 * Keeping a live channel near its live edge.
 *
 * # The fault this fixes, measured
 *
 * A live channel drifted further behind reality the longer it ran. Watched in
 * the app, the player's own clock said:
 *
 *	0:48 / 1:14      play head 26s behind the end of what had arrived
 *	1:23 / 2:17      play head 54s behind, ~30s later
 *
 * The play head advanced at 1.0x the whole time. Nothing was slow and nothing
 * was fast: the *gap* was growing, and nothing in the client had any opinion
 * about that.
 *
 * The server was measured at the same moment and is not at fault — 30.1 frames
 * per media-second against a declared 30, 29.9 frames per wall-second, 29.8s of
 * media in 30s. It hands over exactly one correctly-timed second per second.
 *
 * # Why it accumulates rather than settling
 *
 * `preroll.ts` pauses on `waiting` and waits for a head start before resuming.
 * That is right, and on this provider — bursty delivery with measured five
 * second droughts — it fires often. But it resumed *wherever it stopped*, so
 * every drought moved the play head permanently further from the edge. The
 * buffering a viewer sees every few seconds is not the fault; it is the
 * recovery, running again and again, each pass adding lag that nothing ever
 * takes back.
 *
 * Left alone the buffer also grows without bound, because a progressive fMP4
 * has no window and the element never discards what it has played.
 *
 * # The rule
 *
 * Two thresholds, not one. Correct only when the lag is worth correcting, and
 * correct to a position that still has a cushion — seeking to the very edge
 * lands with nothing in hand and the next drought stalls immediately, which
 * turns one visible jump into a permanent cycle of them.
 */

/**
 * How far behind the edge the play head may drift before it is pulled forward.
 *
 * Comfortably more than the measured 5s drought and the 8s head start
 * `preroll.ts` waits for, so ordinary buffering never triggers a seek — only
 * the accumulation of several droughts does.
 */
export const MAX_LAG_SECONDS = 20;

/**
 * Where to land: this far back from the edge, not at it.
 *
 * Slightly more than one drought, so the correction arrives with enough in hand
 * to survive the next one. Landing at the edge is what makes a live player
 * stutter continuously after every catch-up.
 */
export const TARGET_LAG_SECONDS = 6;

/**
 * liveEdge is the end of what has arrived.
 *
 * The last range rather than the first, for the reason `bufferedAhead` gives:
 * a live stream that has been running has one growing range, and where it has
 * more than one, the incoming data is at the end of the last.
 */
export function liveEdge(el: { buffered: TimeRanges }): number {
  const n = el.buffered.length;
  return n === 0 ? 0 : el.buffered.end(n - 1);
}

/**
 * lagBehindEdge is how far the play head sits behind the incoming data.
 */
export function lagBehindEdge(el: {
  buffered: TimeRanges;
  currentTime: number;
}): number {
  const end = liveEdge(el);
  return end === 0 ? 0 : Math.max(0, end - el.currentTime);
}

/**
 * catchUpTarget answers where the play head should jump to, or null to leave it
 * alone.
 *
 * Pure, and separate from the element, because jsdom neither buffers nor plays:
 * the rule is the part worth testing and the wiring is the part worth reading.
 * The same split `preroll.ts` makes, for the same reason.
 *
 * Never returns a target behind the current position. A live correction that
 * could move somebody backwards would re-show what they just watched, and a
 * rule that can only ever move forward cannot do that however the arithmetic
 * comes out.
 */
export function catchUpTarget(
  currentTime: number,
  edge: number,
  lag: number,
): number | null {
  if (edge <= 0) return null;
  if (lag <= MAX_LAG_SECONDS) return null;

  const target = edge - TARGET_LAG_SECONDS;
  if (target <= currentTime) return null;
  return target;
}

/**
 * resumeTarget is where playback should pick up after a stall.
 *
 * This is the half that stops the drift accumulating. `preroll.ts` resumed
 * wherever the drought left the play head, so each one cost lag permanently.
 * Resuming near the edge instead means a stall costs the time it lasted and
 * nothing after that.
 *
 * It is deliberately the same cushion as a catch-up rather than the edge
 * itself: having just waited for a head start, throwing it away by jumping to
 * the end would guarantee the next stall.
 */
export function resumeTarget(
  currentTime: number,
  edge: number,
): number | null {
  if (edge <= 0) return null;
  const target = edge - TARGET_LAG_SECONDS;
  if (target <= currentTime) return null;
  return target;
}
