/*
 * The MilkDrop output pass's maths (ADR 0091 §5, §6 and Decision 7).
 *
 * A MilkDrop preset is written with no flash rule and no gold rule, and
 * nothing it promises can be trusted, so LANcast draws every preset through a
 * pass of its own: the gold guard, then the luminance limiter. milkdropPass.ts
 * runs that pass as shaders; every number and rule it uses is decided here,
 * as pure functions over numbers, and tested in milliseconds the way vizMath
 * and trailMath are.
 *
 * The order matters, and it is guard first. The guard moves a hue at a fixed
 * HSL lightness, which changes relative luminance (yellow is far brighter than
 * red at the same lightness), so a guard after the limiter could turn a slow
 * drift across the band into a step. The limiter after it only scales a colour
 * or mixes it with white, neither of which moves a hue, so what comes out
 * honours both rules.
 */

import { awayFromGold, GOLD_HUE_MAX, GOLD_HUE_MIN, type RGB } from "./vizMath";

// ---- luminance ------------------------------------------------------------------

/** sRGB-encoded 0..1 to linear light, the IEC 61966-2-1 curve WCAG uses. */
export function srgbToLinear(v: number): number {
  return v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
}

export function linearToSrgb(v: number): number {
  const x = Math.min(1, Math.max(0, v));
  return x <= 0.0031308 ? x * 12.92 : 1.055 * Math.pow(x, 1 / 2.4) - 0.055;
}

/** WCAG relative luminance of a linear-light colour, 0..1. */
export function luminance(r: number, g: number, b: number): number {
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

// ---- the limiter --------------------------------------------------------------------

/*
 * LIMIT_RATE: how far any region's mean luminance may move in a second.
 *
 * It follows from the rule rather than being tuned. WCAG 2.3.1 counts a flash
 * as a pair of opposing changes of at least 0.1 in relative luminance, and
 * allows three a second: six changes of 0.1 is 0.6 of movement. A region that
 * cannot move faster than that cannot flash more than three times a second,
 * however its preset behaves: the rule becomes impossible to break, not
 * unlikely. The spike measured it on the 502-preset pack: 98 presets over
 * three a second before, none after.
 *
 * It also satisfies the plan's other rule, that large changes ease over at
 * least 300 ms: at 0.6 a second, anything larger than 0.18 takes longer than
 * that, and a change smaller than 0.18 is not a large one.
 */
export const LIMIT_RATE = 0.6;

/** Calm halves the allowance (the plan, Photosensitivity). */
export function limitRate(calm: boolean): number {
  return calm ? LIMIT_RATE / 2 : LIMIT_RATE;
}

/*
 * The longest frame the limiter believes. A stall (a dropped tab, a GC pause,
 * a preset compiling) must not hand the next frame a whole second's
 * allowance at once: that frame would be the flash. Longer real gaps move
 * slower than the rule allows, which is the safe side.
 */
export const LIMIT_MAX_DT = 0.1;

/*
 * The regions: a grid of cells, each limited on its own mean. No window's
 * mean can move faster than its fastest cell, so the rule holds for any
 * window at least a cell big. The spike's detector judged windows a third of
 * the screen across; a 16-cell grid is five times finer than that, so it
 * holds for windows far smaller than WCAG's, at the cost of pulling a little
 * more detail than strictly needed.
 */
export const LIMIT_GRID = 16;

/*
 * slewLuma: one cell's limited luminance, one frame on. Linear on purpose,
 * as trailMath's slew is: an exponential ease moves furthest in its first
 * frame, which is the frame the rule is about.
 */
export function slewLuma(prev: number, target: number, dt: number, rate: number): number {
  const step = rate * Math.min(LIMIT_MAX_DT, Math.max(0, dt));
  if (target > prev + step) return prev + step;
  if (target < prev - step) return prev - step;
  return target;
}

/*
 * correction: how a cell's pixels are changed so that their mean becomes the
 * limited value `lim` when the preset drew `src`. One signed number, so that
 * the shader can interpolate it smoothly between cells:
 *  - below zero, the cell is too bright and is scaled by (1 + v);
 *  - above zero, it is too dark, and is mixed toward white by v.
 * Both are linear in the pixel values, so applied to a whole cell the mean
 * lands exactly on `lim`. Neither moves a hue: scaling keeps it, and mixing
 * with white only desaturates. A strobe held by the limiter therefore comes
 * out close to grey, which is the spike's observation of a 5 Hz strobe.
 * Zero is "as drawn", and it is where both halves meet.
 */
export function correction(lim: number, src: number): number {
  if (Math.abs(lim - src) < 1e-6) return 0;
  if (lim < src) return src > 0 ? lim / src - 1 : 0;
  return src < 1 ? Math.min(1, (lim - src) / (1 - src)) : 0;
}

/** A linear-light colour with a correction applied, as the shader does. */
export function applyCorrection(c: RGB, v: number): RGB {
  if (v < 0) return [c[0] * (1 + v), c[1] * (1 + v), c[2] * (1 + v)];
  return [c[0] + (1 - c[0]) * v, c[1] + (1 - c[1]) * v, c[2] + (1 - c[2]) * v];
}

/*
 * The limiter over a series of whole-field luminances, as the shader runs it
 * on each cell: what comes out, frame by frame. The first frame is taken as
 * drawn, because there is nothing before it to flash from; the cover that was
 * there is under an opaque canvas from then on.
 */
export function limitSeries(input: number[], dts: number | number[], rate: number): number[] {
  const out: number[] = [];
  let lim = input[0] ?? 0;
  for (let i = 0; i < input.length; i++) {
    const dt = typeof dts === "number" ? dts : dts[i];
    lim = i === 0 ? input[0] : slewLuma(lim, input[i], dt, rate);
    out.push(lim);
  }
  return out;
}

// ---- measuring flashes --------------------------------------------------------------

/*
 * flashTimes: when the series flashed, by WCAG 2.3.1's definition as the spike
 * implemented it: a pair of opposing changes of at least 0.1 in relative
 * luminance, where the darker state is below 0.8. Returns the time of each
 * completed flash, in seconds.
 *
 * Its own tests come first (a 5 Hz strobe reads 5, steady reads 0), because
 * the spike's first detector read 0 for everything, strobes included, and a
 * gate that cannot see a flash certifies every preset.
 */
export const FLASH_DELTA = 0.1;
export const FLASH_DARK_BELOW = 0.8;

export function flashTimes(lum: number[], times: number[]): number[] {
  const out: number[] = [];
  if (lum.length === 0) return out;
  // Before the first transition there is no direction, so both extremes are
  // kept; after it, only the one the series is heading for.
  let lo = lum[0];
  let hi = lum[0];
  let dir = 0;
  let pending = 0; // counted transitions towards the next flash
  const transition = (from: number, to: number, i: number) => {
    if (Math.min(from, to) >= FLASH_DARK_BELOW) {
      pending = 0; // bright to brighter is not a flash
      return;
    }
    pending++;
    if (pending === 2) {
      out.push(times[i]);
      pending = 0;
    }
  };
  for (let i = 1; i < lum.length; i++) {
    const v = lum[i];
    if (dir === 0) {
      lo = Math.min(lo, v);
      hi = Math.max(hi, v);
      if (v - lo >= FLASH_DELTA) {
        transition(lo, v, i);
        dir = 1;
        hi = v;
      } else if (hi - v >= FLASH_DELTA) {
        transition(hi, v, i);
        dir = -1;
        lo = v;
      }
    } else if (dir > 0) {
      if (v > hi) hi = v;
      else if (hi - v >= FLASH_DELTA) {
        transition(hi, v, i);
        dir = -1;
        lo = v;
      }
    } else if (dir < 0) {
      if (v < lo) lo = v;
      else if (v - lo >= FLASH_DELTA) {
        transition(lo, v, i);
        dir = 1;
        hi = v;
      }
    }
  }
  return out;
}

/** The most flashes in any one-second window. */
export function worstFlashesPerSecond(flashes: number[]): number {
  let worst = 0;
  for (let i = 0, j = 0; i < flashes.length; i++) {
    while (flashes[i] - flashes[j] >= 1 - 1e-9) j++;
    worst = Math.max(worst, i - j + 1);
  }
  return worst;
}

// ---- the gold guard ---------------------------------------------------------------------

/*
 * The guard is vizMath's awayFromGold, per pixel: the same band, the same
 * saturation floor, the same margin, the same rule for which edge a hue goes
 * to. One rule, written once in TypeScript and once in GLSL, and the GLSL is
 * built from these constants so the two cannot drift apart on a number.
 */
export const GUARD_MARGIN = 12;
export const GUARD_MIN_SAT = 0.12;

export function guardColour(c: RGB): RGB {
  return awayFromGold(c, GUARD_MARGIN);
}

/** The band and where its two halves go, for the shader. */
export const GUARD = {
  min: GOLD_HUE_MIN,
  max: GOLD_HUE_MAX,
  mid: (GOLD_HUE_MIN + GOLD_HUE_MAX) / 2,
  below: GOLD_HUE_MIN - GUARD_MARGIN,
  above: GOLD_HUE_MAX + GUARD_MARGIN,
  minSat: GUARD_MIN_SAT,
} as const;

/*
 * guardHue: the hue the guard gives a pixel of hue `h` and HSL saturation `s`.
 * The shader's own function is a transcription of this one.
 */
export function guardHue(h: number, s: number): number {
  const x = ((h % 360) + 360) % 360;
  if (s < GUARD.minSat || x < GUARD.min || x > GUARD.max) return h;
  return x < GUARD.mid ? GUARD.below : GUARD.above;
}
