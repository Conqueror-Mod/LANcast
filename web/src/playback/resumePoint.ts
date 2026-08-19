/*
 * Where an item starts when you press play.
 *
 * A saved position is a bookmark, not a destination. The distinction was
 * missing, and it produced a bug that looked like anything but a resume: press
 * play on the first episode of a show you are part way through and the *third*
 * episode plays.
 *
 * The mechanism, from a real library:
 *
 *	S01E01  duration 1,351,423ms   saved position 1,351,637ms   watched
 *	S01E02  duration 1,352,511ms   saved position 1,352,723ms   watched
 *	S01E03  duration 1,352,810ms   saved position   100,676ms   not watched
 *
 * A finished episode keeps a position *past its own end* — the last save lands
 * after the final frame. Resuming there means `ended` fires on the first tick,
 * the queue advances, the next finished episode does the same, and playback
 * walks forward until it reaches something unfinished. The viewer sees a
 * different episode from the one they pressed, and nothing in between is
 * visible at the speed it happens.
 *
 * So: a finished item starts again. That is also what a person means by
 * pressing play on something they have already watched.
 */

/** How close to the end still counts as finished, in milliseconds.
 *
 *  Credits and a few seconds of black are enough to make an item "over" without
 *  the position ever reaching the duration, and resuming into them plays a
 *  second of nothing before the queue moves on. Fifteen seconds is short enough
 *  that it cannot swallow a real pause — nobody stops fifteen seconds from the
 *  end and means to come back to it. */
export const FINISHED_WITHIN_MS = 15_000;

/**
 * resumeSeconds is where playback should begin, in seconds.
 *
 * Zero for anything finished: the watched flag, a position at or past the
 * duration, or a position inside the last few seconds. Zero also when there is
 * nothing saved, which is the ordinary first play.
 */
export function resumeSeconds(o: {
  positionMs?: number | null;
  watched?: boolean | null;
  durationMs?: number | null;
}): number {
  const pos = o.positionMs ?? 0;
  if (pos <= 0) return 0;
  // Watched is the server's own verdict and outranks arithmetic: an item may be
  // marked finished from anywhere, and a rewatch starts at the beginning.
  if (o.watched) return 0;

  const duration = o.durationMs ?? 0;
  if (duration > 0 && pos >= duration - FINISHED_WITHIN_MS) return 0;

  return pos / 1000;
}
