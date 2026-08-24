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

/** How far in still counts as not having started, in milliseconds.
 *
 *  The other end of the same idea, and it was missing. Progress is written on a
 *  five-second throttle, so skipping through a shuffled library to find
 *  something to watch leaves a five-second position on every film passed over —
 *  and every one of them then "resumes" at 0:05 and sits on the Continue
 *  Watching shelf, having never been watched at all. That is the reported bug:
 *  many films starting at :05 rather than :00.
 *
 *  Sixty seconds, because a resume point is a claim that you were *watching*
 *  and stopped. Under a minute you were deciding, and the honest place to start
 *  a film you looked at for forty seconds is the beginning. It is well clear of
 *  the throttle that creates these, without being long enough to discard a real
 *  early interruption — a phone call two minutes in still resumes.
 *
 *  This is applied when reading as well as when writing, which is what makes it
 *  fix libraries that already carry these positions: no migration, and a film
 *  wrongly showing 0:05 today starts at 0:00 on the next play. */
export const STARTED_AFTER_MS = 60_000;

/**
 * startedFloorMs is the position below which nothing is worth remembering.
 *
 * Proportional as well as absolute, or the rule quietly breaks music: a
 * three-minute song is a third gone at sixty seconds, and refusing to resume a
 * third of the way through a track is not the same judgement as refusing forty
 * seconds of a two-hour film. Whichever threshold is *smaller* wins, so short
 * items get the percentage and long ones get the minute.
 *
 * Exported because both sides need it and must agree. The reader refuses to
 * resume below it and the writer refuses to record below it; if those two
 * numbers ever drift apart the result is a position saved that can never be
 * resumed from, which is invisible until somebody wonders why the Continue
 * shelf holds something that always starts at zero.
 */
export function startedFloorMs(durationMs?: number | null): number {
  const d = durationMs ?? 0;
  return d > 0 ? Math.min(STARTED_AFTER_MS, d * 0.05) : STARTED_AFTER_MS;
}

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

  // Too early to be a bookmark. Same floor the writer uses.
  if (pos < startedFloorMs(duration)) return 0;

  return pos / 1000;
}
