import { describe, it, expect } from "vitest";
import { taperGain, TAPER_IN_MS, TAPER_OUT_S } from "./taper";

const mid = { msSinceStart: null, remainingS: 120, last: false };

describe("the queue's start and end", () => {
  it("fades in from silence when play is pressed, and is at full level after", () => {
    expect(taperGain({ ...mid, msSinceStart: 0 })).toBe(0);
    const half = taperGain({ ...mid, msSinceStart: TAPER_IN_MS / 2 });
    expect(half).toBeGreaterThan(0.5);
    expect(half).toBeLessThan(1);
    expect(taperGain({ ...mid, msSinceStart: TAPER_IN_MS })).toBe(1);
    expect(taperGain(mid)).toBe(1);
  });

  it("fades the last track out over its final seconds", () => {
    expect(taperGain({ ...mid, last: true, remainingS: TAPER_OUT_S + 1 })).toBe(1);
    expect(taperGain({ ...mid, last: true, remainingS: TAPER_OUT_S / 2 })).toBeLessThan(1);
    expect(taperGain({ ...mid, last: true, remainingS: 0 })).toBe(0);
  });

  it("never fades a track something follows, however close its end", () => {
    expect(taperGain({ ...mid, last: false, remainingS: 0.5 })).toBe(1);
  });

  it("does nothing while the length is unknown", () => {
    expect(taperGain({ ...mid, last: true, remainingS: Infinity })).toBe(1);
  });
});
