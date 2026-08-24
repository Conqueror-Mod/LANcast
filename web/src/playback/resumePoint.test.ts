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

/*
 * The other end of the same idea.
 *
 * Reported from a real library: skipping through Randomize all to find
 * something to watch left many films starting at 0:05. Progress is written on a
 * five-second throttle, so every film glanced at got a five-second position —
 * and then "resumed" into it, and sat on the Continue Watching shelf claiming
 * to be part way through.
 */
describe("a position too early to be a bookmark", () => {
  const FILM = 7_200_000; // two hours

  it("ignores the five seconds a skipped film picks up", () => {
    expect(resumeSeconds({ positionMs: 5_000, durationMs: FILM })).toBe(0);
  });

  it("still resumes a real early interruption", () => {
    // Two minutes in is somebody who was watching and stopped.
    expect(resumeSeconds({ positionMs: 120_000, durationMs: FILM })).toBe(120);
  });

  /*
   * The floor is proportional too, or it breaks music: a three-minute song is a
   * third gone at sixty seconds, and refusing to resume a third of the way into
   * a track is not the judgement that was wanted.
   */
  it("scales down for something short", () => {
    const SONG = 180_000; // 5% is 9s
    expect(resumeSeconds({ positionMs: 5_000, durationMs: SONG })).toBe(0);
    expect(resumeSeconds({ positionMs: 30_000, durationMs: SONG })).toBe(30);
  });

  // Nothing known about the length is the one case with no percentage to take,
  // so it falls back to the absolute floor rather than resuming from anywhere.
  it("uses the absolute floor when the duration is unknown", () => {
    expect(resumeSeconds({ positionMs: 5_000 })).toBe(0);
    expect(resumeSeconds({ positionMs: 90_000 })).toBe(90);
  });

  // The floor must not swallow the finished check at the other end: a short
  // item can be finished before it is ever "started".
  it("does not let the floor resurrect a finished item", () => {
    expect(
      resumeSeconds({ positionMs: 100_000, durationMs: 100_000, watched: true }),
    ).toBe(0);
  });
});
