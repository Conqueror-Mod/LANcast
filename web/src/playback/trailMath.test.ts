/*
 * The feedback trails' maths, one named case per behaviour
 * (docs/now-playing-plan.md, Testing). Synthetic arrays in, numbers out: no
 * WebGL and no audio.
 */
import { describe, it, expect } from "vitest";
import {
  bandEnergy,
  BASE_BRIGHT,
  BEAT,
  BEAT_EASE_MIN,
  calmPreset,
  detectBeat,
  frameParams,
  isTrail,
  lowBins,
  mixRGB,
  newBeat,
  newTrailState,
  slew,
  thickStrip,
  TRAIL_IDS,
  TRAIL_MAX_SIDE,
  TRAIL_PRESETS,
  trailSize,
  wavePoints,
} from "./trailMath";
import { blendCrossesGold, NEBULA_PALETTE, paletteFrom, type RGB } from "./vizMath";

const FRAME = 1 / 60;

/** Low energy per frame: a kick of `kick` every `every` seconds over `bed`. */
function kicks(seconds: number, every: number, fps = 60, kick = 0.5, bed = 0.05): number[] {
  const out: number[] = [];
  const n = Math.round(seconds * fps);
  for (let i = 0; i < n; i++) {
    const t = i / fps;
    const phase = t % every;
    out.push(phase < 1 / fps ? kick : bed);
  }
  return out;
}

function countBeats(energy: number[], dt: number): number[] {
  const s = newBeat();
  const at: number[] = [];
  energy.forEach((e, i) => {
    if (detectBeat(s, e, dt)) at.push(i * dt);
  });
  return at;
}

describe("presets", () => {
  it("offers four to six, named plainly", () => {
    expect(TRAIL_IDS.length).toBeGreaterThanOrEqual(4);
    expect(TRAIL_IDS.length).toBeLessThanOrEqual(6);
    expect(TRAIL_IDS).toContain("tunnel");
    expect(TRAIL_IDS).toContain("spiral");
    expect(TRAIL_IDS).toContain("smoke");
  });

  it("holds numbers only: nothing in a preset is code, a string, or anything that could be run", () => {
    for (const id of TRAIL_IDS) {
      const p = TRAIL_PRESETS[id];
      expect(Object.getPrototypeOf(p)).toBe(Object.prototype);
      for (const [k, v] of Object.entries(p)) {
        expect(typeof v, `${id}.${k}`).toBe("number");
        expect(Number.isFinite(v), `${id}.${k}`).toBe(true);
      }
    }
  });

  it("fades every one below 1 a frame, so a trail always dies and the field cannot fill with light", () => {
    for (const id of TRAIL_IDS) {
      expect(TRAIL_PRESETS[id].fade).toBeLessThan(1);
      expect(TRAIL_PRESETS[id].fade).toBeGreaterThan(0.85);
    }
  });

  it("keeps zoom near still: the most a preset moves, bass at full, is a few percent a frame", () => {
    for (const id of TRAIL_IDS) {
      const p = TRAIL_PRESETS[id];
      expect(Math.abs(p.zoom + p.bassZoom - 1)).toBeLessThan(0.08);
    }
  });

  it("knows its own ids and nothing else", () => {
    expect(isTrail("tunnel")).toBe(true);
    expect(isTrail("spectrum")).toBe(false);
    expect(isTrail("constructor")).toBe(false);
  });
});

describe("slew", () => {
  it("takes at least BEAT_EASE_MIN to go from nothing to everything, however small the frames", () => {
    for (const fps of [30, 60, 144]) {
      let v = 0;
      let t = 0;
      while (v < 1) {
        v = slew(v, 1, 1 / fps, BEAT_EASE_MIN);
        t += 1 / fps;
      }
      expect(t).toBeGreaterThanOrEqual(BEAT_EASE_MIN - 1e-9);
    }
  });

  it("never overshoots its target, either way", () => {
    expect(slew(0.9, 1, 1, 0.3)).toBe(1);
    expect(slew(0.1, 0, 1, 0.3)).toBe(0);
  });
});

describe("beat detection", () => {
  it("reads the bins under 150 Hz, skipping DC", () => {
    const low = lowBins(1024, 48000);
    expect(low.start).toBe(1);
    expect(Math.abs(low.end * (24000 / 1024) - 150)).toBeLessThanOrEqual(24000 / 1024);
  });

  it("measures the low band's energy from the analyser's bytes, 0..1", () => {
    const f = new Uint8Array(16);
    expect(bandEnergy(f, 1, 7)).toBe(0);
    f.fill(255);
    expect(bandEnergy(f, 1, 7)).toBe(1);
  });

  it("hears a kick every half second as a beat every half second", () => {
    const at = countBeats(kicks(5, 0.5), FRAME);
    // The first half second warms the average up.
    expect(at.length).toBeGreaterThanOrEqual(8);
    for (let i = 1; i < at.length; i++) expect(at[i] - at[i - 1]).toBeCloseTo(0.5, 1);
  });

  it("hears nothing in a steady signal, however loud", () => {
    const steady = Array(300).fill(0.6);
    expect(countBeats(steady, FRAME)).toEqual([]);
  });

  it("hears nothing in silence or in a quiet passage's noise floor", () => {
    expect(countBeats(Array(300).fill(0), FRAME)).toEqual([]);
    expect(countBeats(kicks(5, 0.5, 60, 0.015, 0.001), FRAME)).toEqual([]);
  });

  it("beats once when a held bass note arrives, not for as long as it is held", () => {
    const e = [...Array(60).fill(0.05), ...Array(240).fill(0.5)];
    expect(countBeats(e, FRAME)).toHaveLength(1);
  });

  it("never beats more than three times a second, even on a kick every frame-and-a-bit", () => {
    const at = countBeats(kicks(4, 0.05), FRAME);
    const perSecond = at.length / (4 - BEAT.warmup);
    expect(perSecond).toBeLessThanOrEqual(3);
    for (let i = 1; i < at.length; i++) expect(at[i] - at[i - 1]).toBeGreaterThanOrEqual(BEAT.minGap - 1e-9);
  });

  it("hears the same beats at 30 and 144 frames a second", () => {
    expect(countBeats(kicks(5, 0.5, 30), 1 / 30).length).toBe(countBeats(kicks(5, 0.5, 144), 1 / 144).length);
  });

  it("does not call the first frame a beat, when there is no average yet to beat", () => {
    const s = newBeat();
    expect(detectBeat(s, 0.9, FRAME)).toBe(false);
  });
});

describe("frameParams", () => {
  const tunnel = TRAIL_PRESETS.tunnel;

  it("scales the per-frame rates by the frame time: two short frames move as far as one long one", () => {
    const quiet = 0;
    const one = frameParams({ ...tunnel, bassZoom: 0 }, newTrailState(), quiet, 2 * FRAME);
    const a = frameParams({ ...tunnel, bassZoom: 0 }, newTrailState(), quiet, FRAME);
    expect(one.zoom).toBeCloseTo(a.zoom * a.zoom, 9);
    expect(one.fade).toBeCloseTo(a.fade * a.fade, 9);
    const spiral = TRAIL_PRESETS.spiral;
    expect(frameParams(spiral, newTrailState(), 0, 2 * FRAME).rotate).toBeCloseTo(
      2 * frameParams(spiral, newTrailState(), 0, FRAME).rotate,
      9,
    );
  });

  it("lets the bass push the zoom, and eases it there rather than jumping", () => {
    const s = newTrailState();
    const still = frameParams(tunnel, s, 0, FRAME).zoom;
    const first = frameParams(tunnel, s, 1, FRAME).zoom;
    let later = first;
    for (let i = 0; i < 30; i++) later = frameParams(tunnel, s, 1, FRAME).zoom;
    expect(first).toBeGreaterThan(still);
    expect(later).toBeGreaterThan(first);
    expect(later).toBeCloseTo(tunnel.zoom + tunnel.bassZoom, 6);
  });

  it("brightens the line on a beat over at least 300 ms, and only the line", () => {
    const s = newTrailState();
    const e = kicks(3, 1);
    let t = 0;
    let beatAt = -1;
    let fullAt = -1;
    for (const x of e) {
      const p = frameParams(tunnel, s, x, FRAME);
      if (p.beat && beatAt < 0) beatAt = t;
      if (beatAt >= 0 && fullAt < 0 && p.bright >= BASE_BRIGHT + tunnel.beatGlow - 1e-9) fullAt = t;
      // The field's fade is the preset's whatever the beat does.
      expect(p.fade).toBeCloseTo(Math.pow(tunnel.fade, 1), 9);
      t += FRAME;
    }
    expect(beatAt).toBeGreaterThan(0);
    expect(fullAt - beatAt).toBeGreaterThanOrEqual(BEAT_EASE_MIN - FRAME - 1e-9);
  });

  it("swings the colour on beats, never by more than the preset's swing, and eased", () => {
    const s = newTrailState();
    let prev = frameParams(tunnel, s, 0, FRAME).mix;
    let lo = prev;
    let hi = prev;
    for (const x of kicks(6, 0.5)) {
      const m = frameParams(tunnel, s, x, FRAME).mix;
      // Per frame no more than the full swing over BEAT_EASE_MIN.
      expect(Math.abs(m - prev)).toBeLessThanOrEqual((tunnel.beatMix / 2) * (FRAME / BEAT_EASE_MIN) * 2 + 1e-9);
      prev = m;
      lo = Math.min(lo, m);
      hi = Math.max(hi, m);
    }
    expect(hi - lo).toBeGreaterThan(0.1);
    expect(hi - lo).toBeLessThanOrEqual(tunnel.beatMix + 1e-9);
  });
});

describe("calm", () => {
  const tunnel = TRAIL_PRESETS.tunnel;
  const spiral = TRAIL_PRESETS.spiral;

  it("halves the motion: the zoom's distance from still, the turn, the drift and the bass push", () => {
    const c = calmPreset(spiral);
    expect(c.zoom - 1).toBeCloseTo((spiral.zoom - 1) / 2, 9);
    expect(c.rotate).toBeCloseTo(spiral.rotate / 2, 9);
    expect(calmPreset(TRAIL_PRESETS.smoke).drift).toBeCloseTo(TRAIL_PRESETS.smoke.drift / 2, 9);
    expect(c.bassZoom).toBeCloseTo(spiral.bassZoom / 2, 9);
  });

  it("turns beat-driven palette shifts off: the colour holds still through a run of beats", () => {
    const s = newTrailState();
    const c = calmPreset(tunnel);
    const mixes = kicks(5, 0.5).map((x) => frameParams(c, s, x, FRAME).mix);
    expect(new Set(mixes)).toEqual(new Set([tunnel.mix]));
  });
});

describe("the waveform", () => {
  it("draws silence as a flat line across the middle when the preset is a line", () => {
    const pts = wavePoints(new Uint8Array(512).fill(128), 9, 0, 0.3, 1);
    for (let i = 0; i < 9; i++) expect(pts[i * 2 + 1]).toBe(0);
    expect(pts[0]).toBeCloseTo(-0.8, 6);
    expect(pts[16]).toBeCloseTo(0.8, 6);
  });

  it("draws silence as a circle when the preset is a ring, round even in a wide field", () => {
    const aspect = 16 / 9;
    const pts = wavePoints(new Uint8Array(512).fill(128), 65, 1, 0.3, aspect);
    for (let i = 0; i < 65; i++) {
      const x = pts[i * 2] * aspect;
      const y = pts[i * 2 + 1];
      expect(Math.hypot(x, y)).toBeCloseTo(0.42, 6);
    }
  });

  it("thickens a line into a strip of the width asked for, either side of it", () => {
    const pts = Float32Array.of(-1, 0, 0, 0, 1, 0);
    const strip = thickStrip(pts, 0.1, 1, 1);
    expect(strip).toHaveLength(12);
    expect(strip[1]).toBeCloseTo(0.05, 6);
    expect(strip[3]).toBeCloseTo(-0.05, 6);
  });

  it("blends along the palette's two colours and never past them", () => {
    const a: RGB = [255, 0, 0];
    const b: RGB = [0, 0, 255];
    expect(mixRGB(a, b, 0)).toEqual([1, 0, 0]);
    expect(mixRGB(a, b, 2)).toEqual([0, 0, 1]);
  });

  it("draws in the same blend vizMath already keeps off gold, so a trail cannot be gold either", () => {
    // A sepia cover: the case that once came out gold by blending.
    const sepia: number[] = [];
    for (let i = 0; i < 64; i++) sepia.push(...(i % 2 ? [150, 60, 40, 255] : [120, 140, 50, 255]));
    const [a, b] = paletteFrom(sepia);
    expect(blendCrossesGold(a, b)).toBe(false);
    const [c, d] = NEBULA_PALETTE;
    expect(blendCrossesGold(c, d)).toBe(false);
  });
});

describe("trailSize", () => {
  it("caps a 4K TV's buffers well under 4K", () => {
    const { w, h } = trailSize(3840, 2160, 1);
    expect(Math.max(w, h)).toBe(TRAIL_MAX_SIDE);
    expect(w / h).toBeCloseTo(16 / 9, 2);
  });

  it("caps the pixel ratio, so a phone at 3x is not drawn at 3x", () => {
    const { w } = trailSize(400, 400, 3);
    expect(w).toBe(600);
  });

  it("never gives a zero-sized buffer for a box not laid out yet", () => {
    expect(trailSize(0, 0, 1)).toEqual({ w: 1, h: 1 });
  });
});
