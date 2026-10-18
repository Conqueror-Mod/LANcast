/*
 * The feedback-trail visualizers' maths (docs/now-playing-plan.md, "Tier 2 —
 * feedback trails").
 *
 * Pure functions over numbers and arrays, like vizMath.ts beside it: no
 * WebGL, no audio, no clock. trails.ts runs the two shaders; everything about
 * how a preset *moves* — how far a frame zooms, how fast a beat may change the
 * colour, when a beat is a beat — is decided here and tested in milliseconds.
 */

import type { RGB } from "./vizMath";

// ---- presets -----------------------------------------------------------------

/*
 * A preset is a plain object of numbers. There is no code in one and nothing
 * is evaluated: that is the line between these and MilkDrop's presets, which
 * compile to JavaScript (the plan, "Not in this plan"). Every rate is per
 * sixtieth of a second, and frameParams scales it by the real frame time.
 */
export interface TrailPreset {
  /** Scale of the previous frame per 1/60 s: above 1 rushes out, below 1 falls in. */
  zoom: number;
  /** Turn of the previous frame per 1/60 s, in radians. */
  rotate: number;
  /** How much of the previous frame survives per 1/60 s, below 1 so trails die. */
  fade: number;
  /** Blur of the previous frame, in buffer pixels. */
  blur: number;
  /** Upward drift of the previous frame per 1/60 s, as a fraction of the height. */
  drift: number;
  /** How much the bass adds to zoom, at full bass. */
  bassZoom: number;
  /** The waveform's shape: 0 a line across, 1 a ring. */
  ring: number;
  /** The waveform's height, as a fraction of the half field. */
  amp: number;
  /** Where the line sits between the palette's two colours, 0..1. */
  mix: number;
  /** How far a beat swings the colour along that blend, 0 for not at all. */
  beatMix: number;
  /** How much a beat brightens the line, on top of BASE_BRIGHT, 0..0.3. */
  beatGlow: number;
}

export type TrailId = "tunnel" | "spiral" | "smoke" | "bloom" | "drift";

export const TRAIL_PRESETS: Record<TrailId, TrailPreset> = {
  // A ring, zooming out: the trails rush past you.
  tunnel: { zoom: 1.018, rotate: 0, fade: 0.955, blur: 0.6, drift: 0, bassZoom: 0.035, ring: 1, amp: 0.22, mix: 0.2, beatMix: 0.6, beatGlow: 0.3 },
  // A ring, zooming out and turning: the rush curls.
  spiral: { zoom: 1.012, rotate: 0.014, fade: 0.962, blur: 0.4, drift: 0, bassZoom: 0.02, ring: 1, amp: 0.2, mix: 0.5, beatMix: 0.5, beatGlow: 0.25 },
  // A line, rising and blurring: it smokes away.
  smoke: { zoom: 1.0, rotate: 0, fade: 0.95, blur: 1.4, drift: 0.004, bassZoom: 0.006, ring: 0, amp: 0.3, mix: 0.3, beatMix: 0.4, beatGlow: 0.2 },
  // A line the bass pushes outward hard, with short trails.
  bloom: { zoom: 1.006, rotate: 0, fade: 0.92, blur: 0.8, drift: 0, bassZoom: 0.06, ring: 0, amp: 0.34, mix: 0.6, beatMix: 0.5, beatGlow: 0.3 },
  // Half a ring, falling slowly inward and turning back: the calmest of them.
  drift: { zoom: 0.992, rotate: -0.005, fade: 0.972, blur: 0.5, drift: 0, bassZoom: 0.01, ring: 0.5, amp: 0.18, mix: 0.8, beatMix: 0.3, beatGlow: 0.15 },
};

export const TRAIL_IDS = Object.keys(TRAIL_PRESETS) as TrailId[];

export function isTrail(id: string): id is TrailId {
  return Object.prototype.hasOwnProperty.call(TRAIL_PRESETS, id);
}

// ---- easing -------------------------------------------------------------------

/*
 * slew moves a value toward a target at no more than 1/seconds of the whole
 * range per second. It is how the plan's flash rule becomes arithmetic:
 * anything a beat drives changes through slew with at least 0.3 s, so going
 * from nothing to everything takes at least 300 ms however hard the beat hits,
 * and a light-dark-light cycle cannot happen more than about once and a half a
 * second. Linear on purpose: an exponential ease jumps furthest in its first
 * frame, which is the frame the rule is about.
 */
export const BEAT_EASE_MIN = 0.3;

export function slew(current: number, target: number, dt: number, seconds: number): number {
  const step = Math.max(0, dt) / Math.max(1e-6, seconds);
  if (target > current) return Math.min(target, current + step);
  return Math.max(target, current - step);
}

// ---- beat detection -----------------------------------------------------------

/*
 * The low bins: everything under ~150 Hz, where the kick and the bass line are.
 * Bin 0 is DC and not a sound.
 */
export function lowBins(binCount: number, sampleRate: number, hz = 150): { start: number; end: number } {
  const hzPerBin = sampleRate / 2 / binCount;
  const end = Math.max(2, Math.min(binCount, Math.round(hz / hzPerBin)));
  return { start: 1, end };
}

/** The energy of a band of the analyser's byte frequency data, 0..1. */
export function bandEnergy(freq: ArrayLike<number>, start: number, end: number): number {
  let sum = 0;
  let n = 0;
  for (let i = start; i < end && i < freq.length; i++) {
    const v = freq[i] / 255;
    sum += v * v;
    n++;
  }
  return n ? sum / n : 0;
}

export interface BeatState {
  /** The rolling average of the low energy. */
  avg: number;
  /** Seconds of signal seen, so the first frames do not compare against nothing. */
  seen: number;
  /** Seconds since the last beat. */
  since: number;
  /** Whether the energy has fallen back under the threshold since that beat. */
  armed: boolean;
}

export function newBeat(): BeatState {
  return { avg: 0, seen: 0, since: Infinity, armed: true };
}

export const BEAT = {
  /** A beat is the low energy this far above its rolling average. */
  ratio: 1.3,
  /** And above this, so a quiet passage's noise floor does not beat. */
  floor: 0.02,
  /** The rolling average's time constant, in seconds. */
  window: 1,
  /** Seconds before the average means anything. */
  warmup: 0.5,
  /*
   * The shortest gap between two beats: a third of a second, so at most three
   * a second (the plan, Photosensitivity). A drum'n'bass roll at 170 bpm is
   * still under it; a snare roll is not a beat each hit, and should not be.
   */
  minGap: 1 / 3,
};

/*
 * detectBeat: is this frame a beat? The energy of the low bins against their
 * own rolling average, so a loud track and a quiet one beat alike, and a
 * sustained bass note beats once, when it arrives, not for as long as it is
 * held: a beat disarms the detector until the energy has dropped back under
 * the threshold, because the average takes a second or more to climb up to a
 * held note and would otherwise call it a beat every minGap until it did. The
 * average is exponential in real time, so 30 Hz and 144 Hz frames see the
 * same beats.
 */
export function detectBeat(s: BeatState, energy: number, dt: number): boolean {
  const t = Math.max(0, dt);
  s.since += t;
  s.seen += t;
  const over = energy > BEAT.floor && energy > s.avg * BEAT.ratio;
  if (!over) s.armed = true;
  const beat = over && s.armed && s.seen >= BEAT.warmup && s.since >= BEAT.minGap;
  // The first frame sets the average; after that it follows.
  s.avg = s.seen <= t ? energy : s.avg + (energy - s.avg) * (1 - Math.exp(-t / BEAT.window));
  if (beat) {
    s.since = 0;
    s.armed = false;
  }
  return beat;
}

// ---- one frame ----------------------------------------------------------------

/** What moves between frames, carried by the render loop. */
export interface TrailState {
  beat: BeatState;
  /** The bass level driving zoom, 0..1, eased. */
  bass: number;
  /** The beat's brightening of the line, 0..1, eased. */
  glow: number;
  /** Which end of the swing a beat sends the colour to next, ±1. */
  side: number;
  /** The colour's position along the swing, -1..1, eased. */
  swing: number;
}

export function newTrailState(): TrailState {
  return { beat: newBeat(), bass: 0, glow: 0, side: 1, swing: 0 };
}

/** The line's brightness between beats; a beat adds up to the preset's beatGlow. */
export const BASE_BRIGHT = 0.7;

/** What the shaders draw this frame with, already scaled to the frame time. */
export interface FrameParams {
  zoom: number;
  rotate: number;
  fade: number;
  blur: number;
  drift: number;
  amp: number;
  ring: number;
  /** Position along the palette blend for the line, 0..1. */
  mix: number;
  /** The line's brightness, 0..1. */
  bright: number;
  beat: boolean;
}

/*
 * Calm halves the motion (the plan, Photosensitivity): the zoom's distance
 * from still, the turn, the drift, and how far the bass pushes. It also turns
 * beat-driven palette shifts off, and halves the beat's brightening.
 */
export function calmPreset(p: TrailPreset): TrailPreset {
  return {
    ...p,
    zoom: 1 + (p.zoom - 1) / 2,
    rotate: p.rotate / 2,
    drift: p.drift / 2,
    bassZoom: p.bassZoom / 2,
    beatMix: 0,
    beatGlow: p.beatGlow / 2,
  };
}

/*
 * frameParams advances one frame: the beat, the eased bass and glow, the
 * colour's swing, and the preset's per-1/60 s rates turned into this frame's.
 * A frame twice as long zooms by the square, turns twice as far and fades by
 * the square, so the picture moves at one speed on any screen.
 */
export function frameParams(p: TrailPreset, s: TrailState, energy: number, dt: number): FrameParams {
  const t = Math.max(0, dt);
  const frames = t * 60;
  const beat = detectBeat(s.beat, energy, t);
  if (beat && p.beatMix > 0) s.side = -s.side;
  // Bass up fast, but no faster than the beat rule allows; down slower.
  const bassTarget = Math.min(1, Math.sqrt(Math.max(0, energy)));
  s.bass = slew(s.bass, bassTarget, t, bassTarget > s.bass ? BEAT_EASE_MIN : 0.6);
  // The glow rises for BEAT_EASE_MIN after a beat, reaching full as it ends,
  // then falls over twice that.
  const rising = s.beat.since < BEAT_EASE_MIN;
  s.glow = slew(s.glow, rising ? 1 : 0, t, rising ? BEAT_EASE_MIN : BEAT_EASE_MIN * 2);
  s.swing = slew(s.swing, p.beatMix > 0 ? s.side : 0, t, BEAT_EASE_MIN * 2);
  const zoom = Math.pow(p.zoom + p.bassZoom * s.bass, frames);
  return {
    zoom,
    rotate: p.rotate * frames,
    fade: Math.pow(Math.min(0.999, Math.max(0, p.fade)), frames),
    blur: p.blur,
    drift: p.drift * frames,
    amp: p.amp,
    ring: p.ring,
    mix: Math.min(1, Math.max(0, p.mix + (p.beatMix / 2) * s.swing)),
    bright: Math.min(1, BASE_BRIGHT + p.beatGlow * s.glow),
    beat,
  };
}

// ---- the waveform -------------------------------------------------------------

/*
 * wavePoints lays the time-domain buffer (bytes, 128 = silence) out as `n`
 * points in clip space (-1..1). `ring` blends a line across the field into a
 * circle round its middle, so a preset picks its shape as a number rather
 * than as a mode. `aspect` (width / height) keeps the ring round in a wide
 * field.
 */
export function wavePoints(
  td: ArrayLike<number>,
  n: number,
  ring: number,
  amp: number,
  aspect: number,
  out = new Float32Array(n * 2),
): Float32Array {
  const len = td.length;
  const r = Math.min(1, Math.max(0, ring));
  const ax = aspect > 1 ? 1 / aspect : 1;
  const ay = aspect < 1 ? aspect : 1;
  for (let i = 0; i < n; i++) {
    const f = n > 1 ? i / (n - 1) : 0;
    const at = len > 1 ? Math.min(len - 1, Math.round(f * (len - 1))) : 0;
    const v = len ? (td[at] - 128) / 128 : 0;
    // The line: across the middle 80%, displaced up and down.
    const lx = -0.8 + 1.6 * f;
    const ly = v * amp;
    // The ring: round the middle, displaced in and out.
    const a = f * Math.PI * 2;
    const rad = 0.42 + v * amp * 0.6;
    const cx = Math.cos(a) * rad * ax;
    const cy = Math.sin(a) * rad * ay;
    out[i * 2] = lx + (cx - lx) * r;
    out[i * 2 + 1] = ly + (cy - ly) * r;
  }
  return out;
}

/*
 * thickStrip turns a polyline into a triangle strip `width` clip units thick
 * (scaled by `sx`/`sy` so it is as thick across as it is up), because WebGL
 * draws lines one pixel wide and a one-pixel line in a zooming field is lost
 * in its first trail. Each point is pushed out both ways along the normal of
 * its neighbours' chord.
 */
export function thickStrip(
  pts: Float32Array,
  width: number,
  sx: number,
  sy: number,
  out = new Float32Array(pts.length * 2),
): Float32Array {
  const n = pts.length / 2;
  for (let i = 0; i < n; i++) {
    const a = Math.max(0, i - 1);
    const b = Math.min(n - 1, i + 1);
    let dx = (pts[b * 2] - pts[a * 2]) / sx;
    let dy = (pts[b * 2 + 1] - pts[a * 2 + 1]) / sy;
    const len = Math.hypot(dx, dy) || 1;
    dx /= len;
    dy /= len;
    const nx = -dy * (width / 2) * sx;
    const ny = dx * (width / 2) * sy;
    out[i * 4] = pts[i * 2] + nx;
    out[i * 4 + 1] = pts[i * 2 + 1] + ny;
    out[i * 4 + 2] = pts[i * 2] - nx;
    out[i * 4 + 3] = pts[i * 2 + 1] - ny;
  }
  return out;
}

/** A colour along the straight blend from a to b, 0..1 each channel. */
export function mixRGB(a: RGB, b: RGB, t: number): [number, number, number] {
  const k = Math.min(1, Math.max(0, t));
  return [(a[0] + (b[0] - a[0]) * k) / 255, (a[1] + (b[1] - a[1]) * k) / 255, (a[2] + (b[2] - a[2]) * k) / 255];
}

// ---- the buffers --------------------------------------------------------------

/*
 * trailSize: the feedback buffers' size. Smaller than the spectrum's canvas,
 * and capped harder: a trail is blurred and zoomed every frame, so detail past
 * ~1280 pixels across is detail the next frame smears away, and a 4K TV at full
 * resolution would run two 33-megabyte buffers through the shader sixty times a
 * second for nothing anybody can see. The canvas is stretched to its box by
 * CSS; the stretch is invisible in a field that is all trails.
 */
export const TRAIL_MAX_RATIO = 1.5;
export const TRAIL_MAX_SIDE = 1280;

export function trailSize(cssW: number, cssH: number, dpr: number): { w: number; h: number } {
  const r = Math.min(Math.max(1, dpr || 1), TRAIL_MAX_RATIO);
  let w = Math.max(1, Math.round(cssW * r));
  let h = Math.max(1, Math.round(cssH * r));
  const over = Math.max(w, h) / TRAIL_MAX_SIDE;
  if (over > 1) {
    w = Math.max(1, Math.round(w / over));
    h = Math.max(1, Math.round(h / over));
  }
  return { w, h };
}
