import { describe, it, expect } from "vitest";
import {
  PREROLL_DEADLINE_MS,
  PREROLL_SECONDS,
  bufferedAhead,
  shouldStartPlayback,
} from "./preroll";

/*
 * The rule that stops a live channel stuttering.
 *
 * Measured on a real IPTV source: bytes arrive in bursts with a **5,071 ms**
 * maximum gap between them — HLS segment pacing relayed verbatim, since the
 * server copies video through. `canplay` fires when the first burst lands, so
 * playback used to start with under a second in hand and run dry at every
 * silence.
 */
describe("shouldStartPlayback", () => {
  it("waits while the head start is short", () => {
    expect(shouldStartPlayback(0, 0)).toBe(false);
    expect(shouldStartPlayback(0.5, 500)).toBe(false);
    // The old behaviour started here, which is the bug.
    expect(shouldStartPlayback(1, 1000)).toBe(false);
  });

  // The threshold has to cover the measured drought, or the wait buys nothing.
  it("covers the measured five-second gap with margin", () => {
    expect(PREROLL_SECONDS).toBeGreaterThan(5);
    expect(shouldStartPlayback(5.1, 1000)).toBe(false);
    expect(shouldStartPlayback(PREROLL_SECONDS, 1000)).toBe(true);
  });

  /*
   * A channel that trickles must still start. Waiting for ever is worse than
   * stuttering, and starting late with a short buffer is exactly what the old
   * behaviour did immediately — so the fallback is never worse than before.
   */
  it("starts anyway once it has waited long enough", () => {
    expect(shouldStartPlayback(0, PREROLL_DEADLINE_MS - 1)).toBe(false);
    expect(shouldStartPlayback(0, PREROLL_DEADLINE_MS)).toBe(true);
    expect(shouldStartPlayback(0.2, PREROLL_DEADLINE_MS + 5000)).toBe(true);
  });
});

// A fake TimeRanges, because jsdom neither buffers nor plays — which is the
// reason the rule is a pure function in the first place.
function ranges(pairs: [number, number][]): TimeRanges {
  return {
    length: pairs.length,
    start: (i: number) => pairs[i][0],
    end: (i: number) => pairs[i][1],
  } as TimeRanges;
}

describe("bufferedAhead", () => {
  it("is zero before anything has arrived", () => {
    expect(bufferedAhead({ buffered: ranges([]), currentTime: 0 })).toBe(0);
  });

  it("measures from the play head to the end of what has arrived", () => {
    expect(bufferedAhead({ buffered: ranges([[0, 12]]), currentTime: 4 })).toBe(8);
  });

  // The last range, not the first: a live stream's incoming data is at the end.
  it("uses the last range when there are several", () => {
    expect(
      bufferedAhead({ buffered: ranges([[0, 3], [20, 31]]), currentTime: 25 }),
    ).toBe(6);
  });

  // A play head past the buffer is not negative headroom; it is none.
  it("never reports a negative head start", () => {
    expect(bufferedAhead({ buffered: ranges([[0, 5]]), currentTime: 9 })).toBe(0);
  });
});
