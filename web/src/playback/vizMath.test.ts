/*
 * The visualizers' maths, one named case per behaviour (docs/now-playing-plan.md,
 * Testing). No audio and no canvas: arrays in, arrays out.
 */
import { describe, it, expect } from "vitest";
import {
  awayFromGold,
  blendAvoidsGold,
  blendCrossesGold,
  calmMotion,
  canvasSize,
  fallPeaks,
  groupBins,
  isGoldHue,
  logBands,
  MOTION,
  NEBULA_PALETTE,
  newPeaks,
  paletteFrom,
  rgbToHsl,
  scopePoints,
  smooth,
  VIZ_MAX_SIDE,
  type RGB,
} from "./vizMath";

const FRAME = 1 / 60;

describe("logBands", () => {
  const bands = logBands(32, 1024, 48000);

  it("makes the number of bars asked for", () => {
    expect(bands).toHaveLength(32);
  });

  it("never gives a bar an empty range, even in the bass where a band is narrower than a bin", () => {
    for (const b of bands) expect(b.end).toBeGreaterThan(b.start);
  });

  it("tiles the bins in order: each bar starts where the last ended, so no bin is read twice", () => {
    for (let i = 1; i < bands.length; i++) expect(bands[i].start).toBe(bands[i - 1].end);
  });

  it("skips bin 0, which is DC and not a sound", () => {
    expect(bands[0].start).toBeGreaterThanOrEqual(1);
  });

  it("gives the treble wider bars than the bass, because pitch is heard on a log scale", () => {
    const w = (i: number) => bands[i].end - bands[i].start;
    expect(w(31)).toBeGreaterThan(w(0) * 10);
  });

  it("stops at 16 kHz rather than spending bars on what most masters filter out", () => {
    const hzPerBin = 24000 / 1024;
    expect(bands[31].end * hzPerBin).toBeLessThanOrEqual(16000 + hzPerBin);
  });

  it("stays inside the bins at a low sample rate where 16 kHz is past Nyquist", () => {
    const low = logBands(32, 1024, 22050);
    for (const b of low) expect(b.end).toBeLessThanOrEqual(1024);
  });
});

describe("groupBins", () => {
  it("reads a band as its loudest bin, so a cymbal in a wide treble band still shows", () => {
    const freq = new Uint8Array(16);
    freq[9] = 255;
    const out = groupBins(freq, [
      { start: 1, end: 4 },
      { start: 4, end: 16 },
    ]);
    expect(out[0]).toBe(0);
    expect(out[1]).toBe(1);
  });

  it("scales the analyser's bytes to 0..1", () => {
    const out = groupBins(Uint8Array.of(0, 51), [{ start: 1, end: 2 }]);
    expect(out[0]).toBeCloseTo(0.2, 5);
  });
});

describe("smooth (attack and decay)", () => {
  it("rises fast: most of the way to a hit in one frame", () => {
    const bars = smooth(new Float32Array(1), [1], FRAME, MOTION);
    expect(bars[0]).toBeGreaterThan(0.5);
  });

  it("falls slowly: a frame after silence the bar has barely moved", () => {
    const bars = smooth(Float32Array.of(1), [0], FRAME, MOTION);
    expect(bars[0]).toBeGreaterThan(0.85);
  });

  it("moves the same distance per second at 144 Hz as at 60 Hz", () => {
    const at60 = Float32Array.of(1);
    for (let i = 0; i < 60; i++) smooth(at60, [0], 1 / 60, MOTION);
    const at144 = Float32Array.of(1);
    for (let i = 0; i < 144; i++) smooth(at144, [0], 1 / 144, MOTION);
    expect(at144[0]).toBeCloseTo(at60[0], 3);
  });

  it("limits flashes to three a second: a full bar takes at least 300 ms to fall to a fifth", () => {
    const bars = Float32Array.of(1);
    let t = 0;
    while (bars[0] > 0.2 && t < 2) {
      smooth(bars, [0], FRAME, MOTION);
      t += FRAME;
    }
    expect(t).toBeGreaterThanOrEqual(0.3);
  });

  it("does not move on a frame of no time, such as the first after a pause", () => {
    const bars = smooth(Float32Array.of(0.5), [1], 0, MOTION);
    expect(bars[0]).toBe(0.5);
  });
});

describe("calm", () => {
  it("halves the motion: half the rise and fall per frame, half the fall of a cap", () => {
    const c = calmMotion(MOTION);
    expect(c.attack).toBeCloseTo(MOTION.attack / 2);
    expect(c.decay).toBeCloseTo(MOTION.decay / 2);
    expect(c.peakGravity).toBeCloseTo(MOTION.peakGravity / 2);
  });

  it("moves a bar less toward a hit than full motion does", () => {
    const full = smooth(new Float32Array(1), [1], FRAME, MOTION)[0];
    const calm = smooth(new Float32Array(1), [1], FRAME, calmMotion(MOTION))[0];
    expect(calm).toBeLessThan(full);
  });
});

describe("fallPeaks", () => {
  it("is pushed up by its bar at once", () => {
    const p = fallPeaks(newPeaks(1), [0.8], FRAME, MOTION);
    expect(p.level[0]).toBeCloseTo(0.8);
  });

  it("rests for the hold before it falls", () => {
    const p = fallPeaks(newPeaks(1), [0.8], FRAME, MOTION);
    fallPeaks(p, [0], MOTION.peakHold / 2, MOTION);
    expect(p.level[0]).toBeCloseTo(0.8);
  });

  it("falls faster the longer it falls, under gravity", () => {
    const p = fallPeaks(newPeaks(1), [1], FRAME, MOTION);
    fallPeaks(p, [0], MOTION.peakHold, MOTION); // the rest
    const drops: number[] = [];
    for (let i = 0; i < 3; i++) {
      const before = p.level[0];
      fallPeaks(p, [0], 0.05, MOTION);
      drops.push(before - p.level[0]);
    }
    expect(drops[1]).toBeGreaterThan(drops[0]);
    expect(drops[2]).toBeGreaterThan(drops[1]);
  });

  it("lands on its bar and never falls through it", () => {
    const p = fallPeaks(newPeaks(1), [1], FRAME, MOTION);
    for (let i = 0; i < 200; i++) fallPeaks(p, [0.3], 0.05, MOTION);
    expect(p.level[0]).toBeCloseTo(0.3);
  });

  it("is caught by a bar rising into it, and rests again", () => {
    const p = fallPeaks(newPeaks(1), [1], FRAME, MOTION);
    fallPeaks(p, [0], 1, MOTION);
    for (let i = 0; i < 5; i++) fallPeaks(p, [0], 0.05, MOTION);
    fallPeaks(p, [0.95], FRAME, MOTION);
    expect(p.level[0]).toBeCloseTo(0.95);
    expect(p.rest[0]).toBe(0);
    expect(p.speed[0]).toBe(0);
  });
});

describe("canvasSize", () => {
  it("is the box times devicePixelRatio", () => {
    expect(canvasSize(400, 400, 1.5)).toEqual({ w: 600, h: 600 });
  });

  it("caps the ratio, so a 3x screen draws at 2x", () => {
    expect(canvasSize(400, 400, 3)).toEqual({ w: 800, h: 800 });
  });

  it("caps the side, so a 4K TV does not render 4K buffers", () => {
    const s = canvasSize(1920, 1080, 2);
    expect(Math.max(s.w, s.h)).toBeLessThanOrEqual(VIZ_MAX_SIDE);
    expect(s.w / s.h).toBeCloseTo(1920 / 1080, 2);
  });

  it("is never zero, even before the box has a size", () => {
    expect(canvasSize(0, 0, 0)).toEqual({ w: 1, h: 1 });
  });
});

function pixels(colours: RGB[]): Uint8ClampedArray {
  const out = new Uint8ClampedArray(colours.length * 4);
  colours.forEach((c, i) => out.set([...c, 255], i * 4));
  return out;
}

describe("paletteFrom (the cover's colours)", () => {
  it("draws in the opposite of the most present hue", () => {
    const red: RGB = [200, 30, 40];
    const blue: RGB = [30, 60, 200];
    const pal = paletteFrom(pixels([red, red, red, blue]));
    const [h] = rgbToHsl(pal[0]);
    // Red, mostly: its opposite is cyan.
    expect(h).toBeGreaterThan(160);
    expect(h).toBeLessThan(200);
  });

  it("prefers a saturated stripe to a grey wall", () => {
    const grey: RGB = [120, 120, 120];
    const green: RGB = [40, 180, 70];
    const pal = paletteFrom(pixels([grey, grey, grey, grey, grey, grey, green]));
    const [h] = rgbToHsl(pal[0]);
    // The green stripe decides it: its opposite is magenta.
    expect(h).toBeGreaterThan(290);
    expect(h).toBeLessThan(340);
  });

  it("draws an orange cover cool, not rust", () => {
    // The cover that found this: orange, which the gold nudge used to turn into
    // a dull 8° brick red drawn over a field of the same colour.
    const orange: RGB = [220, 120, 40];
    const [h, s, l] = rgbToHsl(paletteFrom(pixels([orange, orange, [180, 90, 30]]))[0]);
    expect(h).toBeGreaterThan(190);
    expect(h).toBeLessThan(230);
    expect(s).toBeGreaterThanOrEqual(0.69);
    expect(l).toBeGreaterThan(0.6);
  });

  it("moves a blue cover's opposite off gold, since 220° turned round is 40°", () => {
    const blue: RGB = [40, 90, 200];
    for (const c of paletteFrom(pixels([blue, blue, blue]))) {
      const [h, s] = rgbToHsl(c);
      if (s >= 0.12) expect(isGoldHue(h), `hue ${h.toFixed(0)}`).toBe(false);
    }
  });

  it("gives the nebula for a black-and-white cover, which has no colour to give", () => {
    expect(paletteFrom(pixels([[0, 0, 0], [255, 255, 255], [128, 128, 128]]))).toBe(NEBULA_PALETTE);
  });

  it("gives the nebula for no pixels at all", () => {
    expect(paletteFrom(new Uint8ClampedArray(0))).toBe(NEBULA_PALETTE);
  });

  it("lifts a dark cover's colour to one that reads on the dark field", () => {
    const pal = paletteFrom(pixels([[60, 10, 20]]));
    expect(rgbToHsl(pal[0])[2]).toBeGreaterThan(0.45);
  });

  it("never returns a gold hue, even from a gold cover", () => {
    const gold: RGB = [198, 163, 90]; // --gold itself
    const amber: RGB = [230, 150, 30];
    for (const c of paletteFrom(pixels([gold, gold, amber]))) {
      const [h, s] = rgbToHsl(c);
      if (s >= 0.12) expect(isGoldHue(h), `hue ${h.toFixed(0)}`).toBe(false);
    }
  });

  it("never draws gold between its colours: the gradient up a bar stays off gold too", () => {
    // A sepia cover (the one that found this): mostly warm browns.
    const sepia: RGB[] = [
      [150, 120, 80],
      [120, 95, 60],
      [200, 180, 140],
      [90, 70, 50],
      [170, 150, 110],
    ];
    const [a, b] = paletteFrom(pixels(sepia));
    expect(blendCrossesGold(a, b)).toBe(false);
  });

  it("gives three colours from a one-hue cover, the second a step round the wheel", () => {
    const pal = paletteFrom(pixels([[30, 60, 200]]));
    expect(pal).toHaveLength(3);
    expect(Math.abs(rgbToHsl(pal[0])[0] - rgbToHsl(pal[1])[0])).toBeGreaterThan(10);
  });
});

describe("awayFromGold", () => {
  it("knows --gold and --gold-bright are gold", () => {
    expect(isGoldHue(rgbToHsl([0xc6, 0xa3, 0x5a])[0])).toBe(true);
    expect(isGoldHue(rgbToHsl([0xe3, 0xc1, 0x79])[0])).toBe(true);
  });

  it("pushes a warm orange redder, to the nearer edge", () => {
    const [h] = rgbToHsl(awayFromGold([230, 140, 40])); // ~32°
    expect(h).toBeLessThan(20);
  });

  it("pushes a yellow greener, to the nearer edge", () => {
    const [h] = rgbToHsl(awayFromGold([220, 210, 40])); // ~57°
    expect(h).toBeGreaterThan(75);
  });

  it("counts an olive as gold: a sepia cover's 68° bars read as a gold spectrum", () => {
    expect(isGoldHue(rgbToHsl([178, 192, 89])[0])).toBe(true);
  });

  it("leaves a colour outside the band exactly as it was", () => {
    expect(awayFromGold([30, 60, 200])).toEqual([30, 60, 200]);
  });

  it("leaves a near-grey alone, since it has no hue to read as gold", () => {
    expect(awayFromGold([130, 128, 120])).toEqual([130, 128, 120]);
  });

  it("keeps the colour's lightness and saturation", () => {
    const before = rgbToHsl([230, 140, 40]);
    const after = rgbToHsl(awayFromGold([230, 140, 40]));
    expect(after[1]).toBeCloseTo(before[1], 1);
    expect(after[2]).toBeCloseTo(before[2], 1);
  });
});

describe("blendAvoidsGold", () => {
  it("sees that red to yellow-green blends through orange and tan", () => {
    expect(blendCrossesGold([210, 90, 70], [170, 200, 100])).toBe(true);
  });

  it("moves the second colour to the first one's far side when the blend crosses gold", () => {
    const red: RGB = [210, 90, 70];
    const b = blendAvoidsGold(red, [170, 200, 100]);
    expect(blendCrossesGold(red, b)).toBe(false);
    expect(isGoldHue(rgbToHsl(b)[0])).toBe(false);
  });

  it("leaves a blend that never nears gold alone", () => {
    const blue: RGB = [40, 80, 200];
    const violet: RGB = [110, 70, 190];
    expect(blendAvoidsGold(blue, violet)).toEqual(violet);
  });
});

describe("scopePoints", () => {
  it("draws silence as a flat line through the middle", () => {
    const pts = scopePoints(new Uint8Array(2048).fill(128), 64, 400, 200);
    for (let i = 0; i < 64; i++) expect(pts[i * 2 + 1]).toBe(100);
  });

  it("spans the box from edge to edge", () => {
    const pts = scopePoints(new Uint8Array(2048).fill(128), 64, 400, 200);
    expect(pts[0]).toBe(0);
    expect(pts[63 * 2]).toBe(400);
  });

  it("draws a positive sample above the middle, within the box", () => {
    const pts = scopePoints(new Uint8Array(4).fill(255), 4, 400, 200);
    expect(pts[1]).toBeLessThan(100);
    expect(pts[1]).toBeGreaterThanOrEqual(0);
  });
});
