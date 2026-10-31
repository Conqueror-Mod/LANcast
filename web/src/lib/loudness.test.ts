import { describe, it, expect } from "vitest";
import { levelGain } from "./loudness";

const db = (f: number) => 20 * Math.log10(f);

describe("levelling from ReplayGain (ADR 0108)", () => {
  const loud = { track_gain_db: -8, track_peak: 0.9, album_gain_db: -6, album_peak: 1 };

  it("does nothing when off, untagged, or not a number", () => {
    expect(levelGain(loud, "off")).toBe(1);
    expect(levelGain(undefined, "track")).toBe(1);
    expect(levelGain({}, "album")).toBe(1);
  });

  it("turns a loud track down by its own gain, or its album's", () => {
    expect(db(levelGain(loud, "track"))).toBeCloseTo(-8);
    expect(db(levelGain(loud, "album"))).toBeCloseTo(-6);
  });

  it("falls back to whichever gain was measured", () => {
    expect(db(levelGain({ track_gain_db: -5 }, "album"))).toBeCloseTo(-5);
    expect(db(levelGain({ album_gain_db: -4 }, "track"))).toBeCloseTo(-4);
  });

  it("never raises a quiet track, because an element cannot go past 1", () => {
    expect(levelGain({ track_gain_db: 6, track_peak: 0.3 }, "track")).toBe(1);
  });

  it("lets the peak cap the factor so nothing clips", () => {
    // -1 dB asks for 0.891; a peak of 1.5 allows only 0.667.
    expect(levelGain({ track_gain_db: -1, track_peak: 1.5 }, "track")).toBeCloseTo(1 / 1.5);
  });
});
