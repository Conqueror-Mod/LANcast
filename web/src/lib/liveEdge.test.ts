/*
 * The live-edge rule.
 *
 * Written against a fault measured in the running app: a channel whose play
 * head sat 26 seconds behind the incoming data, then 54 seconds behind half a
 * minute later, while playing at 1.0x the whole time. Nothing was slow. The gap
 * grew because every stall resumed in place and nothing ever took the lag back.
 */
import { describe, it, expect } from "vitest";
import {
  MAX_LAG_SECONDS,
  TARGET_LAG_SECONDS,
  catchUpTarget,
  lagBehindEdge,
  liveEdge,
  resumeTarget,
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
    expect(
      lagBehindEdge({ buffered: ranges([0, 74]), currentTime: 48 }),
    ).toBe(26);
  });

  // A play head past the end is not a negative lag; it is a play head at the
  // edge. Reporting -2 would make every comparison below read backwards.
  it("never reports a negative lag", () => {
    expect(lagBehindEdge({ buffered: ranges([0, 30]), currentTime: 32 })).toBe(
      0,
    );
  });
});

describe("deciding whether to catch up", () => {
  it("leaves ordinary buffering alone", () => {
    // The measured drought is 5s and preroll holds for 8s. Neither may
    // provoke a seek, or every stall becomes a visible jump.
    expect(catchUpTarget(100, 108, 8)).toBeNull();
    expect(catchUpTarget(100, 118, 18)).toBeNull();
  });

  it("corrects a lag that has accumulated past the threshold", () => {
    // The 54s gap from the app.
    const to = catchUpTarget(83, 137, 54);
    expect(to).toBe(137 - TARGET_LAG_SECONDS);
  });

  /*
   * Landing at the edge is the tempting mistake: it looks like the most
   * "live" answer and it guarantees the next drought stalls immediately,
   * turning one jump into a permanent cycle of them.
   */
  it("lands with a cushion rather than at the edge", () => {
    const to = catchUpTarget(0, 100, 100)!;
    expect(to).toBeLessThan(100);
    expect(100 - to).toBe(TARGET_LAG_SECONDS);
    expect(TARGET_LAG_SECONDS).toBeLessThan(MAX_LAG_SECONDS);
  });

  it("does nothing before any media has arrived", () => {
    expect(catchUpTarget(0, 0, 0)).toBeNull();
  });

  /*
   * The rule may only ever move forward. A correction that moved somebody
   * backwards would re-show what they had just watched, which is a worse
   * failure than the lag it was fixing.
   */
  it("never moves the play head backwards", () => {
    // A big lag, but the cushion would land behind where we already are.
    expect(catchUpTarget(98, 100, 100)).toBeNull();
  });
});

describe("resuming after a stall", () => {
  /*
   * The half that stops the drift accumulating. Resuming in place is what the
   * player did before, and it is why each of several stalls a minute cost lag
   * that was never recovered.
   */
  it("resumes near the edge rather than where the drought stopped", () => {
    expect(resumeTarget(48, 74)).toBe(74 - TARGET_LAG_SECONDS);
  });

  it("keeps the head start it just waited for", () => {
    const to = resumeTarget(0, 40)!;
    expect(40 - to).toBe(TARGET_LAG_SECONDS);
  });

  // Unlike a catch-up, this has no threshold: a stall is already evidence
  // that the position is wrong. But it still may not go backwards.
  it("leaves a play head already at the edge alone", () => {
    expect(resumeTarget(38, 40)).toBeNull();
    expect(resumeTarget(0, 0)).toBeNull();
  });
});
