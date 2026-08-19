/*
 * Where playback begins, and the bug that made the rule necessary.
 *
 * Taken from a real library: a finished episode keeps a saved position *past*
 * its own duration, because the last save lands after the final frame. Resuming
 * there fires `ended` immediately, the queue advances, the next finished
 * episode does the same, and pressing play on episode one lands you on episode
 * three with nothing visible in between.
 */
import { describe, it, expect } from "vitest";
import { resumeSeconds, FINISHED_WITHIN_MS } from "./resumePoint";

describe("where an item resumes", () => {
  it("starts a part-watched item where it was left", () => {
    expect(resumeSeconds({ positionMs: 100_676, durationMs: 1_352_810 })).toBeCloseTo(100.676);
  });

  it("starts an untouched item at the beginning", () => {
    expect(resumeSeconds({ durationMs: 1_351_423 })).toBe(0);
    expect(resumeSeconds({ positionMs: 0, durationMs: 1_351_423 })).toBe(0);
  });

  /*
   * The real numbers from the report: position 1,351,637ms against a duration of
   * 1,351,423ms — 214ms *past* the end. This is the case that walked the queue.
   */
  it("restarts an episode whose saved position is past its own end", () => {
    expect(
      resumeSeconds({ positionMs: 1_351_637, watched: true, durationMs: 1_351_423 }),
    ).toBe(0);
  });

  // The watched flag is the server's verdict and outranks the arithmetic: an
  // item can be marked finished from anywhere, and a rewatch starts over.
  it("restarts anything marked watched, wherever the position is", () => {
    expect(resumeSeconds({ positionMs: 5_000, watched: true, durationMs: 1_350_000 })).toBe(0);
  });

  // Credits and a few seconds of black still count as over. Resuming into them
  // plays a second of nothing and then advances, which is the same bug quieter.
  it("restarts an item stopped inside the last few seconds", () => {
    const duration = 1_350_000;
    expect(resumeSeconds({ positionMs: duration - 1_000, durationMs: duration })).toBe(0);
    expect(
      resumeSeconds({ positionMs: duration - FINISHED_WITHIN_MS + 500, durationMs: duration }),
    ).toBe(0);
  });

  // But a genuine pause near the end is still a pause. Nobody stops fifteen
  // seconds from the end and means it; twenty minutes in, they do.
  it("keeps a real position outside the finished window", () => {
    const duration = 1_350_000;
    const pos = duration - FINISHED_WITHIN_MS - 60_000;
    expect(resumeSeconds({ positionMs: pos, durationMs: duration })).toBeCloseTo(pos / 1000);
  });

  // A file with no known duration cannot be measured against its end, so the
  // saved position is honoured rather than guessed away.
  it("honours a position when the duration is unknown", () => {
    expect(resumeSeconds({ positionMs: 90_000 })).toBeCloseTo(90);
  });
});
