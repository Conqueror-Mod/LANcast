/*
 * Keeping a live channel near its live edge.
 *
 * # The fault
 *
 * A live channel drifted further behind reality the longer it ran. Measured in
 * the app, the player's own clock said:
 *
 *	0:48 / 1:14      play head 26s behind the incoming data
 *	1:23 / 2:17      54s behind, ~30s later
 *
 * The play head advanced at 1.0x throughout. Nothing was slow and nothing was
 * fast — the *gap* grew, because `preroll.ts` pauses on every drought and
 * resumes wherever it stopped, so each one adds lag that nothing takes back.
 *
 * The server is not involved, and this was measured rather than assumed: the
 * same channel at the same moment delivers 30.1 frames per media-second against
 * a declared 30, 29.9 frames per wall-second, and audio at 46.9 packets per
 * second against the 46.9 that AAC-LC at 48kHz requires — with audio and video
 * spans agreeing to within 70ms, across twenty channels.
 *
 * # Why this nudges the rate rather than seeking
 *
 * The first version of this file seeked, and that was the wrong instrument.
 * The reason is in `channellive.go`: the live endpoint is an **unbounded
 * chunked response with no `Accept-Ranges` and no `Content-Length`**. The
 * browser cannot ask the server for a different offset, so a seek can only ever
 * be a move within data already held — and one that misses leaves the element
 * waiting for bytes nobody will request. Reported from use as a channel that
 * stops and will not restart however many times you press play, and reproduced
 * in the app: 22 seconds buffered, play head at 0:00, play doing nothing.
 *
 * Seeking also forces the audio decoder to re-sync at a point a live stream may
 * not cleanly have. "The speed problem is audio now" arrived with the release
 * that introduced seeking, against audio measured as correct — which is the
 * shape of an artefact of the correction rather than a fault in the stream.
 *
 * So: **never seek.** Play slightly faster until the gap closes. Nothing can be
 * stranded by a rate change, no decoder has to re-sync, and Chromium preserves
 * pitch by default, so the nudge is close to imperceptible. hls.js solves the
 * same problem the same way.
 *
 * The honest limit: a rate nudge *bounds* drift rather than erasing it. On a
 * channel that stalls constantly, holds can add lag faster than 10% recovers
 * it. That is an argument for shortening the hold, not for seeking again.
 */

/**
 * Lag beyond which the player starts catching up.
 *
 * Well past the measured five-second drought and the eight-second head start
 * `preroll.ts` waits for, so ordinary buffering never changes the rate.
 */
export const MAX_LAG_SECONDS = 20;

/**
 * Lag at which it stops.
 *
 * Deliberately far below MAX_LAG_SECONDS: the two together are hysteresis. A
 * single threshold would flap around its own boundary, changing the rate every
 * few seconds, and a rate that keeps changing is audible in a way that neither
 * value alone is.
 */
export const SETTLED_LAG_SECONDS = 8;

/**
 * How much faster to play while catching up.
 *
 * 10% closes a twenty-second gap in a little over three minutes — slow enough
 * to be unobtrusive, fast enough to outrun ordinary drift. Higher values catch
 * up sooner and start being something a viewer notices, and "the picture ran
 * fast" is the complaint this whole area exists to answer, so it is not the
 * direction to err in.
 */
export const CATCHUP_RATE = 1.1;

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
 * catchUpRate answers how fast to play, given the lag and whether the player is
 * already catching up.
 *
 * Pure, and separate from the element, because jsdom neither buffers nor plays:
 * the rule is the part worth testing and the wiring is the part worth reading.
 * The same split `preroll.ts` makes, for the same reason.
 *
 * `catching` is passed in rather than read off the element, because a viewer
 * may have chosen their own speed from the player's own menu — this has to be
 * able to tell "1.1 because we set it" from "1.1 because they did", and the
 * element cannot answer that.
 */
export function catchUpRate(lag: number, catching: boolean): number {
  if (!catching) {
    return lag > MAX_LAG_SECONDS ? CATCHUP_RATE : 1;
  }
  // Already closing the gap: keep going until it is properly closed, rather
  // than stopping the moment it dips under the threshold that started this.
  return lag > SETTLED_LAG_SECONDS ? CATCHUP_RATE : 1;
}
