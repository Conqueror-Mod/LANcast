/*
 * Noticing that a file is playing badly rather than not playing.
 *
 * # What was measured
 *
 * Two HEVC Main 10 films and one H.264 film, direct-played from the same
 * library on the same machine, minutes apart:
 *
 * | file | codec | decoded | dropped |
 * |---|---|---|---|
 * | I Still Know (1998) | HEVC Main 10 | 288 | **57 (19.8%)** |
 * | I Know (1997)       | HEVC Main 10 | 287 | **57 (19.9%)** |
 * | I'll Always Know (2006) | H.264 High | 288 | **0** |
 *
 * A fifth of every frame thrown away, sustained, on the codec — reported as
 * *"heavy frame lag"*. The H.264 control in the same folder dropped nothing, so
 * it is not the disk, the network or the machine being busy.
 *
 * # Why the capability APIs cannot be asked instead
 *
 * Both of them said this was fine. `canPlayType` answers `"probably"` for HEVC
 * Main 10, and `mediaCapabilities.decodingInfo()` — which exists precisely to
 * answer *will this be smooth* — returned `supported: true, smooth: true,
 * powerEfficient: true` for the exact resolution, frame rate and bitrate of the
 * file that drops a fifth of its frames.
 *
 * So there is no question to ask before playing. The only honest signal is the
 * playback itself, which is why this measures rather than predicts. That is the
 * same conclusion `fileTransport.ts` reached about playlists, arrived at from
 * the opposite direction: there the API was too vague to trust, here it is
 * confidently wrong.
 *
 * # Why this is a claim withdrawal and not a warning
 *
 * The client tells the server which codecs it can take, and the server
 * direct-plays anything on that list. A claim that produces a fifth of a film
 * is a false claim, and `capabilities.ts` already has the machinery for exactly
 * this — `deny` records "this did not work here", it expires after a fortnight
 * so a driver update repairs it, and it is visible and clearable in settings.
 * All that was missing is that denial could only be triggered by an *error*.
 *
 * A file that plays badly has not errored. It is the failure that looks like
 * success, which is the kind this project keeps finding late.
 */

/** One reading of the element's decode counters. */
export type Sample = {
  /** Milliseconds, from the same clock throughout. */
  at: number;
  decoded: number;
  dropped: number;
};

/**
 * How long the evidence must span before it is allowed to mean anything.
 *
 * A seek, a resize, or the first moments after a codec change all drop frames
 * legitimately. Ten seconds of steady playing is long enough that a burst
 * cannot carry the average and short enough that nobody watches a fifth of a
 * film in slideshow before anything happens.
 */
export const MIN_SPAN_MS = 10_000;

/**
 * And how many frames, so a stalled element cannot qualify on a tiny sample.
 *
 * Ten seconds at 24fps is 240 frames; 120 is half that, which keeps the rule
 * working on 12fps animation without letting a handful of frames decide.
 */
export const MIN_FRAMES = 120;

/**
 * The fraction of dropped frames that stops being a hiccup.
 *
 * Measured: the bad case is ~20% sustained and the good case is 0%. Ten percent
 * sits between them with room on both sides — high enough that ordinary
 * playback never reaches it, low enough that nobody has to watch 20% before the
 * fallback happens.
 */
export const DROP_LIMIT = 0.1;

/**
 * struggling reports whether the gap between two readings is bad enough to
 * withdraw a codec claim.
 *
 * Takes two samples rather than a running average because the counters are
 * cumulative for the life of the element: the difference is the only thing that
 * describes *now*, and a lifetime average would be dragged down for ever by a
 * bad first minute — or, worse, hide a file that got bad later.
 */
export function struggling(first: Sample, last: Sample): boolean {
  const span = last.at - first.at;
  const decoded = last.decoded - first.decoded;
  const dropped = last.dropped - first.dropped;
  if (span < MIN_SPAN_MS) return false;
  if (decoded < MIN_FRAMES) return false;
  if (dropped <= 0) return false;
  return dropped / decoded >= DROP_LIMIT;
}
