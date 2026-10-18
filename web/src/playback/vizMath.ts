/*
 * The visualizers' maths (docs/now-playing-plan.md, "Tier 1 — classic").
 *
 * Pure functions over arrays: no audio, no canvas, no clock. Visualizer.tsx
 * feeds them an AnalyserNode's numbers once a frame and draws what they
 * return, so everything about how the bars *behave* is tested here, in
 * milliseconds, the way probe/decide_test.go tests decisions.
 */

/** A band of FFT bins, [start, end). */
export interface BinRange {
  start: number;
  end: number;
}

/*
 * logBands spreads `count` bars over minHz..maxHz on a log scale, which is how
 * an ear hears pitch: an octave is an octave whether it is 50–100 Hz or
 * 5–10 kHz. Linear bars would give the top two octaves three quarters of the
 * screen and leave the bass, where most of a track's movement is, two bars.
 *
 * At the bottom a log band is narrower than one bin (23 Hz apart at 48 kHz),
 * so each band is at least one bin wide and starts where the last one ended:
 * the lowest bars are one bin each, never the same bin twice, and never empty.
 */
export function logBands(
  count: number,
  binCount: number,
  sampleRate: number,
  minHz = 40,
  maxHz = 16000,
): BinRange[] {
  const nyquist = sampleRate / 2;
  const hzPerBin = nyquist / binCount;
  const top = Math.min(maxHz, nyquist);
  const ratio = top / minHz;
  const out: BinRange[] = [];
  let start = Math.max(1, Math.floor(minHz / hzPerBin)); // bin 0 is DC
  for (let i = 0; i < count; i++) {
    const hiHz = minHz * Math.pow(ratio, (i + 1) / count);
    let end = Math.round(hiHz / hzPerBin);
    end = Math.max(end, start + 1);
    end = Math.min(end, binCount);
    if (start >= binCount) start = binCount - 1;
    if (end <= start) end = start + 1;
    out.push({ start, end });
    start = end;
  }
  return out;
}

/*
 * groupBins reads each band as the loudest bin in it, scaled to 0..1 from the
 * analyser's byte frequency data (0..255, already in decibels between the
 * node's min and max). The loudest, not the mean: a wide treble band averaged
 * is a flat line under a cymbal that is plainly there.
 */
export function groupBins(freq: ArrayLike<number>, bands: BinRange[], out = new Float32Array(bands.length)): Float32Array {
  for (let i = 0; i < bands.length; i++) {
    let m = 0;
    const { start, end } = bands[i];
    for (let b = start; b < end && b < freq.length; b++) if (freq[b] > m) m = freq[b];
    out[i] = m / 255;
  }
  return out;
}

/** How fast a bar follows its band, per second, rising and falling. */
export interface Motion {
  /** Fraction of the gap closed per 1/60 s while rising. */
  attack: number;
  /** Fraction of the gap closed per 1/60 s while falling. */
  decay: number;
  /** How long a peak cap rests before it falls, in seconds. */
  peakHold: number;
  /** How fast a peak cap falls, in bar heights per second per second. */
  peakGravity: number;
}

/*
 * The decay is also the flash limit (the plan, Photosensitivity: no more than
 * three full-field flashes a second). Bars rise at once, so a light-dark-light
 * cycle is as fast as they fall: at 0.08 a full bar takes about 320 ms to drop
 * to a fifth, which holds even a stage of bars all hit together to three
 * cycles a second. vizMath.test.ts holds that as a number.
 */
export const MOTION: Motion = { attack: 0.6, decay: 0.08, peakHold: 0.35, peakGravity: 2.4 };

/*
 * Calm halves the motion (the plan, Photosensitivity): bars close half the gap
 * per frame and caps fall at half the speed. It is the setting for anyone who
 * finds the full version busy, and what a visualizer picked on purpose runs
 * at under prefers-reduced-motion.
 */
export function calmMotion(m: Motion): Motion {
  return { attack: m.attack / 2, decay: m.decay / 2, peakHold: m.peakHold, peakGravity: m.peakGravity / 2 };
}

/*
 * smooth moves each bar toward its band: fast up (attack), slow down (decay),
 * which is the classic look: a hit jumps, then settles. The rates are per
 * sixtieth of a second and scaled by dt, so a 144 Hz screen and a 30 Hz one
 * move the bars at the same speed rather than one at five times the other.
 */
export function smooth(prev: Float32Array, next: ArrayLike<number>, dt: number, m: Motion): Float32Array {
  const frames = Math.max(0, dt) * 60;
  const up = 1 - Math.pow(1 - m.attack, frames);
  const down = 1 - Math.pow(1 - m.decay, frames);
  for (let i = 0; i < prev.length; i++) {
    const target = next[i] ?? 0;
    const k = target > prev[i] ? up : down;
    prev[i] += (target - prev[i]) * k;
  }
  return prev;
}

/** Each bar's peak cap: where it is, how long it has rested, how fast it falls. */
export interface Peaks {
  level: Float32Array;
  rest: Float32Array;
  speed: Float32Array;
}

export function newPeaks(n: number): Peaks {
  return { level: new Float32Array(n), rest: new Float32Array(n), speed: new Float32Array(n) };
}

/*
 * fallPeaks: a bar pushes its cap up and the cap stays there for peakHold,
 * then falls under constant gravity, accelerating, until it lands on the bar.
 * The bar never passes through its cap from below; a bar that rises into a
 * falling cap catches it and the cap rests again.
 */
export function fallPeaks(p: Peaks, bars: ArrayLike<number>, dt: number, m: Motion): Peaks {
  const t = Math.max(0, dt);
  for (let i = 0; i < p.level.length; i++) {
    const bar = bars[i] ?? 0;
    if (bar >= p.level[i]) {
      p.level[i] = bar;
      p.rest[i] = 0;
      p.speed[i] = 0;
      continue;
    }
    if (p.rest[i] < m.peakHold) {
      p.rest[i] += t;
      continue;
    }
    p.speed[i] += m.peakGravity * t;
    p.level[i] = Math.max(bar, p.level[i] - p.speed[i] * t);
  }
  return p;
}

/*
 * canvasSize: the canvas's backing size, its box times devicePixelRatio,
 * capped. A 4K TV at a ratio of 2 would otherwise draw bars into a buffer four
 * times the pixels anybody can tell apart at ten feet.
 */
export const VIZ_MAX_RATIO = 2;
export const VIZ_MAX_SIDE = 2048;

export function canvasSize(cssW: number, cssH: number, dpr: number): { w: number; h: number } {
  const r = Math.min(Math.max(1, dpr || 1), VIZ_MAX_RATIO);
  let w = Math.max(1, Math.round(cssW * r));
  let h = Math.max(1, Math.round(cssH * r));
  const over = Math.max(w, h) / VIZ_MAX_SIDE;
  if (over > 1) {
    w = Math.max(1, Math.round(w / over));
    h = Math.max(1, Math.round(h / over));
  }
  return { w, h };
}

// ---- colour ----------------------------------------------------------------

export type RGB = [number, number, number];

/*
 * The nebula, for a record with no cover or a cover with no colour in it
 * (design.md, Colour): blue, violet, and a lighter blue for the caps.
 */
export const NEBULA_PALETTE: RGB[] = [
  [0x26, 0x58, 0xa8],
  [0x5a, 0x4a, 0x9e],
  [0x9d, 0xb6, 0xe8],
];

export function rgbToHsl([r, g, b]: RGB): [number, number, number] {
  const R = r / 255;
  const G = g / 255;
  const B = b / 255;
  const max = Math.max(R, G, B);
  const min = Math.min(R, G, B);
  const l = (max + min) / 2;
  if (max === min) return [0, 0, l];
  const d = max - min;
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
  let h: number;
  if (max === R) h = ((G - B) / d + (G < B ? 6 : 0)) * 60;
  else if (max === G) h = ((B - R) / d + 2) * 60;
  else h = ((R - G) / d + 4) * 60;
  return [h, s, l];
}

export function hslToRgb(h: number, s: number, l: number): RGB {
  const hh = (((h % 360) + 360) % 360) / 360;
  if (s === 0) {
    const v = Math.round(l * 255);
    return [v, v, v];
  }
  const q = l < 0.5 ? l * (1 + s) : l + s - l * s;
  const p = 2 * l - q;
  const ch = (t: number) => {
    if (t < 0) t += 1;
    if (t > 1) t -= 1;
    if (t < 1 / 6) return p + (q - p) * 6 * t;
    if (t < 1 / 2) return q;
    if (t < 2 / 3) return p + (q - p) * (2 / 3 - t) * 6;
    return p;
  };
  return [Math.round(ch(hh + 1 / 3) * 255), Math.round(ch(hh) * 255), Math.round(ch(hh - 1 / 3) * 255)];
}

/*
 * The gold band. --gold is #c6a35a, hue 40°; --gold-bright #e3c179, 40° too.
 * Oranges, ambers and yellows from 25° to 60° read as gold on a dark field at
 * the saturation a visualizer draws at, so nothing a visualizer draws may sit
 * in it (design.md: gold means where you are, and nothing else).
 */
export const GOLD_HUE_MIN = 25;
export const GOLD_HUE_MAX = 60;

export function isGoldHue(h: number): boolean {
  const x = ((h % 360) + 360) % 360;
  return x >= GOLD_HUE_MIN && x <= GOLD_HUE_MAX;
}

/*
 * awayFromGold nudges a colour out of the gold band to the nearer edge plus a
 * margin: a warm orange goes redder, a yellow goes greener. The record's
 * colour is kept as nearly as it can be; only the one thing gold must not be
 * is taken from it.
 */
export function awayFromGold(c: RGB, margin = 8): RGB {
  const [h, s, l] = rgbToHsl(c);
  if (s < 0.12 || !isGoldHue(h)) return c;
  const mid = (GOLD_HUE_MIN + GOLD_HUE_MAX) / 2;
  const nh = h < mid ? GOLD_HUE_MIN - margin : GOLD_HUE_MAX + margin;
  return hslToRgb(nh, s, l);
}

/*
 * paletteFrom takes the cover, downsampled to a few pixels (RGBA, as a
 * canvas's getImageData returns it), and gives the visualizer three colours:
 * the two most present hues and a light one for the caps.
 *
 * Pixels are counted into twelve hue buckets, weighted by saturation, so a
 * cover that is mostly a grey wall with a red stripe gives red rather than
 * grey. Greys and near-blacks count for nothing; a cover that is all of them
 * (a black-and-white photograph) has no colour to give and gets the nebula.
 * Every colour is lifted to a lightness that reads on the dark field, and
 * nudged off gold.
 */
export function paletteFrom(rgba: ArrayLike<number>): RGB[] {
  const BUCKETS = 12;
  const weight = new Float64Array(BUCKETS);
  const sum: [number, number, number][] = Array.from({ length: BUCKETS }, () => [0, 0, 0]);
  for (let i = 0; i + 3 < rgba.length; i += 4) {
    if (rgba[i + 3] < 128) continue; // transparent
    const c: RGB = [rgba[i], rgba[i + 1], rgba[i + 2]];
    const [h, s, l] = rgbToHsl(c);
    if (s < 0.2 || l < 0.08 || l > 0.95) continue;
    const b = Math.floor((h / 360) * BUCKETS) % BUCKETS;
    const w = s * (1 - Math.abs(l - 0.5));
    weight[b] += w;
    sum[b][0] += c[0] * w;
    sum[b][1] += c[1] * w;
    sum[b][2] += c[2] * w;
  }
  const order = [...weight.keys()].filter((b) => weight[b] > 0).sort((a, b) => weight[b] - weight[a]);
  if (order.length === 0) return NEBULA_PALETTE;
  const mean = (b: number): RGB => [sum[b][0] / weight[b], sum[b][1] / weight[b], sum[b][2] / weight[b]];
  const first = mean(order[0]);
  // A cover with one hue gives that hue twice, the second a step round the
  // wheel, so the gradient up a bar still moves.
  const second = order[1] !== undefined ? mean(order[1]) : shiftHue(first, 30);
  const readable = (c: RGB, l: number): RGB => {
    const [h, s] = rgbToHsl(c);
    return hslToRgb(h, Math.max(0.45, s), l);
  };
  const a = awayFromGold(readable(first, 0.55));
  const b = awayFromGold(readable(second, 0.62));
  const cap = awayFromGold(readable(first, 0.82));
  return [a, b, cap];
}

function shiftHue(c: RGB, by: number): RGB {
  const [h, s, l] = rgbToHsl(c);
  return hslToRgb(h + by, s, l);
}

export function css([r, g, b]: RGB, a = 1): string {
  return `rgba(${Math.round(r)}, ${Math.round(g)}, ${Math.round(b)}, ${a})`;
}

// ---- the scope ---------------------------------------------------------------

/*
 * scopePoints turns a time-domain buffer (bytes, 128 = silence) into `n`
 * points across a box `w` wide, centred on `h / 2`, at `gain` of the half
 * height. Fewer points than samples: a 2048-sample line drawn into 400 css
 * pixels is five segments per pixel nobody can see.
 */
export function scopePoints(td: ArrayLike<number>, n: number, w: number, h: number, gain = 0.9): Float32Array {
  const out = new Float32Array(n * 2);
  const mid = h / 2;
  const len = td.length;
  for (let i = 0; i < n; i++) {
    const at = len > 1 ? Math.min(len - 1, Math.round((i / Math.max(1, n - 1)) * (len - 1))) : 0;
    const v = len ? (td[at] - 128) / 128 : 0;
    out[i * 2] = (i / Math.max(1, n - 1)) * w;
    out[i * 2 + 1] = mid - v * mid * gain;
  }
  return out;
}
