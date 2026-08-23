/*
 * The live-edge rule.
 *
 * Written against a fault measured in the running app while the server was
 * measured at the same moment and found correct — 30.1 frames per media-second
 * against a declared 30, and audio at 46.9 packets per second against the 46.9
 * that AAC-LC at 48kHz requires:
 *
 *	0:48 / 1:14      play head 26s behind the incoming data
 *	1:23 / 2:17      54s behind, ~30s later
 *
 * The play head ran at 1.0x throughout. The gap grew, because every drought
 * resumed where it stopped and nothing took the lag back.
 */
import { describe, it, expect } from "vitest";
import { PREROLL_SECONDS } from "./preroll";
import {
  CATCHUP_RATE,
  MAX_LAG_SECONDS,
  SETTLED_LAG_SECONDS,
  catchUpRate,
  lagBehindEdge,
  liveEdge,
} from "./liveEdge";

// A stand-in for TimeRanges, which jsdom does not provide.
function ranges(...spans: [number, number][]): TimeRanges {
  return {
    length: spans.length,
    start: (i: number) => spans[i][0],
    end: (i: number) => spans[i][1],
  } as TimeRanges;
}

describe("reading the edge", () => {
  it("takes the end of the last range, where the incoming data is", () => {
    expect(liveEdge({ buffered: ranges([0, 30], [45, 90]) })).toBe(90);
  });

  it("has no edge before anything has arrived", () => {
    expect(liveEdge({ buffered: ranges() })).toBe(0);
    expect(lagBehindEdge({ buffered: ranges(), currentTime: 0 })).toBe(0);
  });

  it("measures the gap the app showed", () => {
    // 0:48 / 1:14, read off the player during the fault.
    expect(lagBehindEdge({ buffered: ranges([0, 74]), currentTime: 48 })).toBe(
      26,
    );
  });

  // A play head past the end is not a negative lag; it is a play head at the
  // edge. Reporting -2 would make every comparison below read backwards.
  it("never reports a negative lag", () => {
    expect(lagBehindEdge({ buffered: ranges([0, 30]), currentTime: 32 })).toBe(
      0,
    );
  });
});

describe("deciding how fast to play", () => {
  it("leaves ordinary buffering at normal speed", () => {
    // The measured drought is 5s and preroll holds for 8s. Neither may change
    // the rate, or every stall becomes an audible speed-up.
    expect(catchUpRate(5, false)).toBe(1);
    expect(catchUpRate(PREROLL_SECONDS, false)).toBe(1);
    expect(catchUpRate(MAX_LAG_SECONDS, false)).toBe(1);
  });

  it("speeds up once the lag is more than buffering explains", () => {
    expect(catchUpRate(MAX_LAG_SECONDS + 1, false)).toBe(CATCHUP_RATE);
    // The 54s gap from the app.
    expect(catchUpRate(54, false)).toBe(CATCHUP_RATE);
  });

  /*
   * Hysteresis. A single threshold flaps around its own boundary — speeding up
   * at 20.1s, stopping at 19.9s, again seconds later — and a rate that keeps
   * changing is audible in a way that neither setting alone is.
   */
  it("keeps catching up below the threshold that started it", () => {
    expect(catchUpRate(MAX_LAG_SECONDS - 5, true)).toBe(CATCHUP_RATE);
    expect(catchUpRate(SETTLED_LAG_SECONDS + 1, true)).toBe(CATCHUP_RATE);
  });

  it("stops once the gap is properly closed", () => {
    expect(catchUpRate(SETTLED_LAG_SECONDS, true)).toBe(1);
    expect(catchUpRate(0, true)).toBe(1);
  });

  it("settles well below where it engages", () => {
    expect(SETTLED_LAG_SECONDS).toBeLessThan(MAX_LAG_SECONDS);
  });

  /*
   * The rate has to be gentle. "The picture ran fast" is the complaint this
   * whole area exists to answer, so a correction loud enough to be mistaken for
   * that fault would be answering it with itself.
   */
  it("nudges rather than races", () => {
    expect(CATCHUP_RATE).toBeGreaterThan(1);
    expect(CATCHUP_RATE).toBeLessThanOrEqual(1.15);
  });

  /*
   * The instrument matters as much as the thresholds, so it is pinned.
   *
   * The live endpoint is an unbounded chunked response with no Accept-Ranges,
   * so a seek outside the buffer strands the element — observed as a channel
   * holding 22 seconds of media, sitting at 0:00, that would not start however
   * many times play was pressed. This module must offer no way back to that.
   */
  it("offers no way to seek", async () => {
    const mod = await import("./liveEdge");
    for (const name of Object.keys(mod)) {
      expect(name).not.toMatch(/seek|target/i);
    }
  });
});
