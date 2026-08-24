import { describe, it, expect } from "vitest";
import {
  PREROLL_DEADLINE_MS,
  PREROLL_SECONDS,
  bufferedAhead,
  shouldStartPlayback,
  shouldHold,
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

  /*
   * This used to assert PREROLL_SECONDS > 5, on the premise that the client
   * cushion had to outlast the provider's drought on its own. That premise is
   * gone: `internal/livebuf` holds the jitter buffer on the server now and
   * hands the stream out at its own rate, so the silences never reach here.
   *
   * The cushion covers ordinary jitter — a slow frame, a scheduling hiccup —
   * and is deliberately small, because the two cushions add up and a viewer
   * waits for the sum of both before a channel starts. Paying twice for the
   * same protection would double the startup for nothing.
   */
  it("is a small cushion, not a substitute for the server's", () => {
    expect(PREROLL_SECONDS).toBeGreaterThan(0);
    expect(PREROLL_SECONDS).toBeLessThanOrEqual(5);
    expect(shouldStartPlayback(PREROLL_SECONDS, 0)).toBe(true);
    expect(shouldStartPlayback(PREROLL_SECONDS - 0.1, 0)).toBe(false);
  });

  // The deadline is the escape hatch, so it has to outlast the wait it bounds.
  it("gives the cushion time to arrive before giving up on it", () => {
    expect(PREROLL_DEADLINE_MS / 1000).toBeGreaterThan(PREROLL_SECONDS);
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

/*
 * `waiting` is not a measurement of how much media is in hand.
 *
 * Measured against a real channel in Chrome: it fired 113 times in 135 seconds
 * — about once a second — while the element held between 117 and 142 seconds of
 * buffered media. Pausing on each one left the player paused 28% of wall time
 * with a median of 131 seconds in hand, and dragged playback to 0.76x real
 * time. That was the stutter, the drift no catch-up could outrun, and almost
 * certainly the choppy audio.
 */
describe("deciding whether a stall is real", () => {
  it("does not hold when the cushion is already there", () => {
    expect(shouldHold(PREROLL_SECONDS)).toBe(false);
    expect(shouldHold(PREROLL_SECONDS + 1)).toBe(false);
    // The measured case: two minutes in hand, and a `waiting` every second.
    expect(shouldHold(131)).toBe(false);
  });

  it("still holds when the cushion is genuinely gone", () => {
    expect(shouldHold(0)).toBe(true);
    expect(shouldHold(0.2)).toBe(true);
    expect(shouldHold(PREROLL_SECONDS - 1)).toBe(true);
  });

  /*
   * The two rules are complements and must stay that way: a hold entered on a
   * buffer that already satisfies shouldStartPlayback would release on its very
   * next tick, which is the 250ms-per-event tax this exists to stop.
   */
  it("never holds on a buffer that would immediately resume", () => {
    for (const ahead of [0, 1, 4, 7.9, 8, 12, 60, 131]) {
      if (shouldStartPlayback(ahead, 0)) {
        expect(shouldHold(ahead)).toBe(false);
      }
    }
  });
});
