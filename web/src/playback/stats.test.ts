import { describe, expect, it } from "vitest";

import {
  LOSS_THRESHOLD,
  format,
  read,
  summarise,
  type Sample,
} from "./stats";

/*
 * The arithmetic behind the statistics overlay.
 *
 * jsdom performs no media, so nothing here can prove the overlay shows the
 * truth about a real film — that needs eyes on a picture. What it can prove is
 * that the reading is honest about what it does and does not know, which is
 * exactly where this class of tool goes wrong: a diagnostic that quietly
 * reports zeroes is worse than no diagnostic, because it is trusted.
 */

function sample(over: Partial<Sample> = {}): Sample {
  return {
    dropped: 0,
    total: 0,
    ahead: 0,
    at: 0,
    clock: 0,
    width: 1920,
    height: 800,
    ...over,
  };
}

describe("read", () => {
  /*
   * The case that must never be shown as "nothing is wrong".
   *
   * Firefox has never implemented getVideoPlaybackQuality, and an audio element
   * has no frames. Reporting zero dropped frames there would be a confident
   * claim built on an absence.
   */
  it("returns null when the browser will not report quality", () => {
    const el = document.createElement("video");
    expect(read(el)).toBeNull();
  });

  it("reads what the element reports", () => {
    const el = document.createElement("video");
    Object.defineProperty(el, "getVideoPlaybackQuality", {
      value: () => ({ droppedVideoFrames: 12, totalVideoFrames: 600 }),
      configurable: true,
    });
    const got = read(el, 5000);
    expect(got?.dropped).toBe(12);
    expect(got?.total).toBe(600);
    expect(got?.clock).toBe(5000);
  });

  // An element with no source throws on `buffered`; the reading still has to
  // come back, because the frame counters are the part that matters.
  it("survives an element that cannot report its buffer", () => {
    const el = document.createElement("video");
    Object.defineProperty(el, "getVideoPlaybackQuality", {
      value: () => ({ droppedVideoFrames: 0, totalVideoFrames: 0 }),
      configurable: true,
    });
    Object.defineProperty(el, "buffered", {
      get() {
        throw new Error("no source");
      },
      configurable: true,
    });
    expect(read(el)?.ahead).toBe(0);
  });
});

describe("summarise", () => {
  it("has no frame rate to report from a single sample", () => {
    // Said as null rather than as zero: "no reading yet" and "zero frames per
    // second" are different, and one of them means playback has stopped.
    expect(summarise(sample({ total: 100 }), null).fps).toBeNull();
  });

  it("derives the frame rate from the interval, not from the totals", () => {
    // The totals span the element's whole life, including any time it sat
    // paused — an average over that describes nothing on screen now.
    const prev = sample({ total: 1000, clock: 10_000 });
    const now = sample({ total: 1024, clock: 11_000 });
    expect(summarise(now, prev).fps).toBeCloseTo(24, 5);
  });

  it("reports no frame rate when the counters have been reset", () => {
    // The element is torn down and rebuilt between items, which zeroes the
    // counters under a poller that is still running. A negative frame count is
    // that, not a measurement.
    const prev = sample({ total: 5000, clock: 10_000 });
    const now = sample({ total: 12, clock: 11_000 });
    expect(summarise(now, prev).fps).toBeNull();
  });

  it("reports no frame rate when two reads land in the same millisecond", () => {
    const prev = sample({ total: 100, clock: 7_000 });
    const now = sample({ total: 124, clock: 7_000 });
    expect(summarise(now, prev).fps).toBeNull();
  });

  it("calls it losing time only once the loss is visible rather than incidental", () => {
    // A few dropped frames are ordinary — a seek, a fullscreen transition, a
    // tab regaining focus. The threshold exists so nobody chases one.
    const light = summarise(sample({ dropped: 1, total: 10_000 }), null);
    expect(light.losing).toBe(false);

    const heavy = summarise(sample({ dropped: 300, total: 10_000 }), null);
    expect(heavy.dropRate).toBeCloseTo(3, 5);
    expect(heavy.losing).toBe(true);
  });

  it("is exactly at the threshold rather than just past it", () => {
    const at = summarise(sample({ dropped: 100, total: 10_000 }), null);
    expect(at.dropRate).toBe(LOSS_THRESHOLD);
    expect(at.losing).toBe(true);
  });

  it("does not divide by zero before any frame has been shown", () => {
    const s = summarise(sample({ dropped: 0, total: 0 }), null);
    expect(s.dropRate).toBe(0);
    expect(s.losing).toBe(false);
  });
});

describe("format", () => {
  it("says in words when the picture is losing time", () => {
    // The number alone only means something to somebody who already knows what
    // a normal drop rate looks like.
    const lines = format(summarise(sample({ dropped: 300, total: 10_000 }), null));
    expect(lines.join(" ")).toContain("dropping frames");
  });

  it("says nothing alarming when the picture is healthy", () => {
    const lines = format(summarise(sample({ dropped: 0, total: 10_000 }), null));
    expect(lines.join(" ")).not.toContain("dropping frames");
  });
});
