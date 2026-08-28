/*
 * What the picture is actually doing.
 *
 * This exists because a film played badly and the player could not say a word
 * about it. Diagnosing one stuttering title took GPU performance counters,
 * ffprobe, an mp4 atom scan and a forced re-encode to compare against — an
 * evening, from outside the application, to answer a question the media element
 * had been holding the whole time.
 *
 * `getVideoPlaybackQuality()` reports dropped frames. That single number
 * separates the two faults that look identical from the sofa: a picture that
 * judders because frames are being *thrown away*, and one that judders because
 * frames are arriving late or being presented unevenly. The first is a decode
 * or delivery problem; the second is not, and nothing else in the system can
 * tell them apart.
 *
 * Kept pure and away from the overlay so the arithmetic is testable. jsdom
 * performs no media, so a test can only ever check the reading — which is the
 * half that has been wrong here before.
 */

/** One reading, in the shape the element reports it. */
export type Sample = {
  /** Frames the decoder produced and the compositor never showed. */
  dropped: number;
  /** Frames handed to the element, dropped ones included. */
  total: number;
  /** Seconds of media buffered beyond the play head. */
  ahead: number;
  /** Play head, seconds. */
  at: number;
  /** Wall clock of this reading, ms. */
  clock: number;
  width: number;
  height: number;
};

export type Stats = {
  dropped: number;
  total: number;
  /** Percentage of frames dropped over the whole session, 0 when none seen. */
  dropRate: number;
  /** Frames per second presented since the previous sample, null on the first. */
  fps: number | null;
  ahead: number;
  width: number;
  height: number;
  /**
   * Whether the drop rate is bad enough to be the explanation for a complaint.
   *
   * A handful of dropped frames is ordinary — a seek, a tab regaining focus, a
   * fullscreen transition. Sustained loss is not, and the point of a threshold
   * is to stop somebody reading a single dropped frame as a fault and chasing
   * it. One percent of 24fps is a frame every four seconds, which is where a
   * person starts to see it rather than measure it.
   */
  losing: boolean;
};

export const LOSS_THRESHOLD = 1; // percent

/**
 * read pulls one sample from a media element, or null when the browser cannot
 * report quality.
 *
 * Firefox has never implemented `getVideoPlaybackQuality`, and an audio element
 * has no frames to report. Returning null rather than zeroes matters: zero
 * dropped frames is a strong claim, and "this browser will not say" must not be
 * displayed as "nothing is wrong".
 */
export function read(el: HTMLVideoElement, now = Date.now()): Sample | null {
  const q = (
    el as HTMLVideoElement & {
      getVideoPlaybackQuality?: () => VideoPlaybackQuality;
    }
  ).getVideoPlaybackQuality;
  if (typeof q !== "function") return null;

  const quality = q.call(el);
  let ahead = 0;
  try {
    const b = el.buffered;
    if (b.length > 0) ahead = Math.max(0, b.end(b.length - 1) - el.currentTime);
  } catch {
    // buffered throws on an element with no source yet.
  }

  return {
    dropped: quality.droppedVideoFrames,
    total: quality.totalVideoFrames,
    ahead,
    at: el.currentTime,
    clock: now,
    width: el.videoWidth,
    height: el.videoHeight,
  };
}

/**
 * summarise turns two samples into what a person needs to read.
 *
 * The frame rate is derived from the *difference* between samples rather than
 * from the totals, because the totals include everything since the element was
 * created — a film paused for ten minutes would report an average that has
 * nothing to do with what is on screen now.
 */
export function summarise(now: Sample, prev: Sample | null): Stats {
  const dropRate = now.total > 0 ? (now.dropped / now.total) * 100 : 0;

  let fps: number | null = null;
  if (prev) {
    const seconds = (now.clock - prev.clock) / 1000;
    const frames = now.total - prev.total;
    /*
     * Both guards are real cases rather than defensive noise. A zero interval
     * happens when two reads land in the same millisecond; a negative frame
     * count happens when the element is torn down and rebuilt for the next
     * item, which resets the counters to zero under a still-running poller.
     */
    if (seconds > 0 && frames >= 0) fps = frames / seconds;
  }

  return {
    dropped: now.dropped,
    total: now.total,
    dropRate,
    fps,
    ahead: now.ahead,
    width: now.width,
    height: now.height,
    losing: dropRate >= LOSS_THRESHOLD,
  };
}

/** A short line for the overlay, kept here so the format is testable. */
export function format(s: Stats): string[] {
  const lines = [
    `${s.width}×${s.height}`,
    s.fps === null ? "fps —" : `${s.fps.toFixed(1)} fps`,
    `${s.dropped} of ${s.total} frames dropped (${s.dropRate.toFixed(2)}%)`,
    `${s.ahead.toFixed(1)}s buffered`,
  ];
  if (s.losing) {
    // Said in words, because the number is only meaningful to somebody who
    // already knows what a normal drop rate looks like.
    lines.push("dropping frames — the picture is losing time");
  }
  return lines;
}
