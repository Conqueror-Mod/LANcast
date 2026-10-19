/*
 * The MilkDrop output pass's maths, one named case per behaviour (ADR 0091
 * §5 and §6). Synthetic luminances in, numbers out: no WebGL, no butterchurn.
 */
import { describe, it, expect } from "vitest";
import {
  applyCorrection,
  correction,
  flashTimes,
  GUARD,
  guardColour,
  guardHue,
  limitRate,
  limitSeries,
  LIMIT_MAX_DT,
  LIMIT_RATE,
  linearToSrgb,
  luminance,
  slewLuma,
  srgbToLinear,
  worstFlashesPerSecond,
} from "./limiterMath";
import { awayFromGold, isGoldHue, rgbToHsl, type RGB } from "./vizMath";

const FRAME = 1 / 60;

/** A full-field square wave between `lo` and `hi`, at `hz`, for `seconds`. */
function strobe(hz: number, seconds: number, fps = 60, lo = 0, hi = 1): { lum: number[]; t: number[] } {
  const lum: number[] = [];
  const t: number[] = [];
  const n = Math.round(seconds * fps);
  for (let i = 0; i < n; i++) {
    const at = i / fps;
    lum.push(Math.floor(at * hz * 2) % 2 === 0 ? lo : hi);
    t.push(at);
  }
  return { lum, t };
}

function times(n: number, dt = FRAME): number[] {
  return Array.from({ length: n }, (_, i) => i * dt);
}

describe("the flash detector", () => {
  // The spike's first detector read zero for everything, strobes included.
  // A gate that cannot see a flash certifies every preset, so it is checked
  // against controls before anything is measured with it.
  it("reads a 5 Hz strobe as five flashes a second", () => {
    const { lum, t } = strobe(5, 4);
    expect(worstFlashesPerSecond(flashTimes(lum, t))).toBe(5);
  });

  it("reads a 2 Hz strobe as two", () => {
    const { lum, t } = strobe(2, 4);
    expect(worstFlashesPerSecond(flashTimes(lum, t))).toBe(2);
  });

  it("reads a steady field as none", () => {
    const lum = new Array(240).fill(0.4);
    expect(flashTimes(lum, times(240))).toEqual([]);
  });

  it("does not count changes smaller than 0.1", () => {
    const { lum, t } = strobe(10, 2, 60, 0.3, 0.38);
    expect(flashTimes(lum, t)).toEqual([]);
  });

  it("does not count swings that stay above 0.8", () => {
    const { lum, t } = strobe(10, 2, 60, 0.82, 1);
    expect(flashTimes(lum, t)).toEqual([]);
  });

  it("follows a slow rise to its top before calling the fall", () => {
    // Up by small steps to 0.6, then straight down: one transition up, one
    // down, one flash, however finely the rise was sampled.
    const lum = [0, 0.05, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0];
    expect(flashTimes(lum, times(lum.length))).toHaveLength(1);
  });
});

describe("the limiter", () => {
  it("holds a 10 Hz full-field strobe to three flashes a second or fewer", () => {
    const { lum, t } = strobe(10, 5);
    expect(worstFlashesPerSecond(flashTimes(lum, t))).toBe(10);
    const out = limitSeries(lum, FRAME, LIMIT_RATE);
    expect(worstFlashesPerSecond(flashTimes(out, t))).toBeLessThanOrEqual(3);
  });

  it("holds a strobe that flips every frame to three or fewer", () => {
    const { lum, t } = strobe(30, 5);
    const out = limitSeries(lum, FRAME, LIMIT_RATE);
    expect(worstFlashesPerSecond(flashTimes(out, t))).toBeLessThanOrEqual(3);
  });

  it("holds every strobe from 1 to 30 Hz to three or fewer", () => {
    for (let hz = 1; hz <= 30; hz++) {
      const { lum, t } = strobe(hz, 4);
      const out = limitSeries(lum, FRAME, LIMIT_RATE);
      expect(worstFlashesPerSecond(flashTimes(out, t)), `${hz} Hz`).toBeLessThanOrEqual(3);
    }
  });

  it("holds the rule at 144 Hz and at 30 Hz, because the rate is per second", () => {
    for (const fps of [30, 144]) {
      const { lum, t } = strobe(12, 4, fps);
      const out = limitSeries(lum, 1 / fps, LIMIT_RATE);
      expect(worstFlashesPerSecond(flashTimes(out, t)), `${fps} fps`).toBeLessThanOrEqual(3);
    }
  });

  it("does not hand a stalled frame the whole gap's allowance", () => {
    // A one-second stall would otherwise let the next frame move by 0.6.
    expect(slewLuma(0, 1, 1, LIMIT_RATE)).toBeCloseTo(LIMIT_RATE * LIMIT_MAX_DT, 9);
  });

  it("holds the rule under jittery frame times", () => {
    const { lum } = strobe(8, 5);
    // Frames between 4 ms and 40 ms, in a fixed pattern.
    const dts = lum.map((_, i) => 0.004 + ((i * 7919) % 37) / 1000);
    const t: number[] = [];
    dts.reduce((at, dt) => (t.push(at), at + dt), 0);
    const out = limitSeries(lum, dts, LIMIT_RATE);
    expect(worstFlashesPerSecond(flashTimes(out, t))).toBeLessThanOrEqual(3);
  });

  it("eases a cut from black to white over 300 ms or more", () => {
    const lum = [0, ...new Array(120).fill(1)];
    const out = limitSeries(lum, FRAME, LIMIT_RATE);
    // Nothing gets more than 0.18 of the way in under 300 ms…
    expect(out[Math.round(0.3 / FRAME)]).toBeLessThanOrEqual(0.18 + 1e-9);
    // …and the whole cut takes over a second and a half.
    expect(out.findIndex((v) => v >= 1) * FRAME).toBeGreaterThanOrEqual(1 / LIMIT_RATE - FRAME);
  });

  it("leaves a picture that moves slower than the limit as drawn", () => {
    const lum = Array.from({ length: 120 }, (_, i) => 0.3 + 0.2 * Math.sin(i / 30));
    const out = limitSeries(lum, FRAME, LIMIT_RATE);
    out.forEach((v, i) => expect(v).toBeCloseTo(lum[i], 9));
  });

  it("halves the allowance in calm, which holds a strobe to one and a half", () => {
    expect(limitRate(true)).toBe(LIMIT_RATE / 2);
    expect(limitRate(false)).toBe(LIMIT_RATE);
    const { lum, t } = strobe(10, 6);
    const out = limitSeries(lum, FRAME, limitRate(true));
    expect(worstFlashesPerSecond(flashTimes(out, t))).toBeLessThanOrEqual(1.5);
  });
});

describe("the correction a cell gets", () => {
  const cell: RGB[] = [
    [0.9, 0.1, 0.1],
    [0.2, 0.6, 0.9],
    [0.05, 0.05, 0.05],
    [1, 1, 1],
  ];
  const meanLum = (px: RGB[]) => px.reduce((a, c) => a + luminance(...c), 0) / px.length;

  it("is zero when the cell is already where the limiter wants it", () => {
    expect(correction(0.4, 0.4)).toBe(0);
  });

  it("lands a too-bright cell's mean exactly on the limited value", () => {
    const src = meanLum(cell);
    const v = correction(0.2, src);
    expect(v).toBeLessThan(0);
    expect(meanLum(cell.map((c) => applyCorrection(c, v)))).toBeCloseTo(0.2, 9);
  });

  it("lands a too-dark cell's mean exactly on the limited value", () => {
    const dark = cell.map((c) => c.map((x) => x * 0.1) as RGB);
    const v = correction(0.5, meanLum(dark));
    expect(v).toBeGreaterThan(0);
    expect(meanLum(dark.map((c) => applyCorrection(c, v)))).toBeCloseTo(0.5, 9);
  });

  it("raises a black cell, which scaling alone never could", () => {
    const black: RGB[] = [[0, 0, 0]];
    const v = correction(0.3, 0);
    expect(meanLum(black.map((c) => applyCorrection(c, v)))).toBeCloseTo(0.3, 9);
  });

  it("never moves a hue, so it cannot undo the gold guard", () => {
    const c: RGB = [0.8, 0.2, 0.5];
    const [h] = rgbToHsl(c.map((x) => x * 255) as RGB);
    for (const v of [-0.7, -0.2, 0.2, 0.7]) {
      const o = applyCorrection(c, v).map((x) => x * 255) as RGB;
      expect(rgbToHsl(o)[0]).toBeCloseTo(h, 6);
    }
  });

  it("stays within -1..1, so the shader can store it", () => {
    for (const [lim, src] of [
      [0, 1],
      [1, 0],
      [0.5, 0.0001],
      [0.0001, 0.9],
    ]) {
      const v = correction(lim, src);
      expect(v).toBeGreaterThanOrEqual(-1);
      expect(v).toBeLessThanOrEqual(1);
    }
  });
});

describe("luminance", () => {
  it("round-trips the sRGB curve", () => {
    for (const x of [0, 0.02, 0.2, 0.5, 0.9, 1]) expect(linearToSrgb(srgbToLinear(x))).toBeCloseTo(x, 9);
  });

  it("weighs green over red over blue, as WCAG does", () => {
    expect(luminance(0, 1, 0)).toBeGreaterThan(luminance(1, 0, 0));
    expect(luminance(1, 0, 0)).toBeGreaterThan(luminance(0, 0, 1));
    expect(luminance(1, 1, 1)).toBeCloseTo(1, 9);
  });
});

describe("the gold guard", () => {
  it("is vizMath's rule: the same band and the same edges", () => {
    expect(GUARD.min).toBe(20);
    expect(GUARD.max).toBe(75);
    expect(GUARD.below).toBeLessThan(GUARD.min);
    expect(GUARD.above).toBeGreaterThan(GUARD.max);
  });

  it("moves every saturated hue in the band out of it", () => {
    for (let h = 0; h < 360; h += 0.5) {
      const out = guardHue(h, 0.8);
      expect(isGoldHue(out) && out >= 0, `${h}°`).toBe(false);
    }
  });

  it("leaves hues outside the band, and greys, alone", () => {
    expect(guardHue(200, 0.8)).toBe(200);
    expect(guardHue(5, 0.8)).toBe(5);
    expect(guardHue(40, 0.05)).toBe(40);
  });

  it("agrees with awayFromGold on the colours a preset draws", () => {
    // Amber, yellow, olive, orange, and two that are not gold at all.
    const colours: RGB[] = [
      [230, 160, 40],
      [240, 220, 30],
      [130, 120, 40],
      [250, 120, 20],
      [40, 90, 220],
      [200, 30, 60],
    ];
    for (const c of colours) {
      expect(guardColour(c)).toEqual(awayFromGold(c, 12));
      const [h, s] = rgbToHsl(c);
      const [gh] = rgbToHsl(guardColour(c));
      if (s >= GUARD.minSat && isGoldHue(h)) expect(Math.round(gh)).toBe(Math.round(guardHue(h, s)));
    }
  });
});
