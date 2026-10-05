/*
 * The music graph (docs/audio-pass-plan.md, Phase 2).
 *
 * What it does to sound was measured offline, through Chromium's own Web Audio
 * and ffmpeg's ebur128 (the plan's "Phase 2, measured"). jsdom has no Web
 * Audio, so what is pinned here is the part a measurement cannot see once it
 * has passed: which nodes the sound goes through for each setting, and the
 * arithmetic that makes the vocals matrix unable to clip.
 */
import { describe, it, expect } from "vitest";
import {
  buildGraph,
  fxApplies,
  softCeiling,
  SIDE_GAIN,
  NIGHT,
  type ElementFX,
} from "./elementAudio";

/** A node that records where it is connected. Enough of AudioNode for the graph. */
class FakeNode {
  out: FakeNode[] = [];
  gain = { value: 1 };
  threshold = { value: 0 };
  knee = { value: 0 };
  ratio = { value: 0 };
  attack = { value: 0 };
  release = { value: 0 };
  channelCount = 2;
  channelCountMode = "explicit";
  curve: Float32Array | null = null;
  oversample = "none";
  constructor(public kind: string) {}
  connect(n: FakeNode) {
    this.out.push(n);
    return n;
  }
  disconnect() {
    this.out = [];
  }
}

function fakeContext() {
  const made: FakeNode[] = [];
  const mk = (kind: string) => () => {
    const n = new FakeNode(kind);
    made.push(n);
    return n;
  };
  return {
    made,
    createGain: mk("gain"),
    createChannelSplitter: mk("split"),
    createChannelMerger: mk("merge"),
    createDynamicsCompressor: mk("comp"),
    createWaveShaper: mk("shaper"),
  };
}

/** Every node reachable from `from`, by kind, in the order first reached. */
function path(from: FakeNode): string[] {
  const seen = new Set<FakeNode>();
  const kinds: string[] = [];
  const walk = (n: FakeNode) => {
    if (seen.has(n)) return;
    seen.add(n);
    kinds.push(n.kind);
    n.out.forEach(walk);
  };
  walk(from);
  return kinds;
}

function graph() {
  const ctx = fakeContext();
  const g = buildGraph(ctx as unknown as BaseAudioContext);
  return { ctx, g, input: g.input as unknown as FakeNode, output: g.output as unknown as FakeNode };
}

describe("the music graph's routing", () => {
  it("is a straight wire when everything is off", () => {
    const { input, output } = graph();
    expect(input.out).toEqual([output]);
  });

  it("goes through the compressor and the ceiling for night mode only", () => {
    const { g, input } = graph();
    g.set({ night: true, vocals: 0 });
    const p = path(input);
    expect(p).toContain("comp");
    expect(p).toContain("shaper");
    expect(p).not.toContain("split");
  });

  it("goes through the matrix for vocals only", () => {
    const { g, input } = graph();
    g.set({ night: false, vocals: 2 });
    const p = path(input);
    expect(p).toContain("split");
    expect(p).not.toContain("comp");
  });

  it("puts vocals before night mode, so the compressor levels the new balance", () => {
    const { g, input } = graph();
    g.set({ night: true, vocals: 1 });
    const p = path(input);
    expect(p.indexOf("merge")).toBeGreaterThan(-1);
    expect(p.indexOf("merge")).toBeLessThan(p.indexOf("comp"));
  });

  it("returns to a straight wire, rather than leaving a stage attached", () => {
    const { g, input, output } = graph();
    g.set({ night: true, vocals: 2 });
    g.set({ night: false, vocals: 0 });
    expect(input.out).toEqual([output]);
  });

  it("drops night mode cleanly when only vocals stays on", () => {
    const { g, input } = graph();
    g.set({ night: true, vocals: 2 });
    g.set({ night: false, vocals: 2 });
    // A stage left attached would play the track twice: once through it,
    // once straight to the output.
    expect(path(input)).not.toContain("comp");
  });

  it("lets neither end fold channels, for the films that share the element", () => {
    const { input, output } = graph();
    expect(input.channelCountMode).toBe("max");
    expect(output.channelCountMode).toBe("max");
  });

  it("sets the compressor to the measured numbers", () => {
    const { ctx } = graph();
    const comp = ctx.made.find((n) => n.kind === "comp")!;
    expect(comp.threshold.value).toBe(NIGHT.threshold);
    expect(comp.ratio.value).toBe(NIGHT.ratio);
    expect(comp.release.value).toBe(NIGHT.release);
  });
});

describe("the vocals matrix", () => {
  /*
   * L' = aL + bR with a = (1+g)/2, b = (1-g)/2. a + b = 1 and both are
   * non-negative, so L' is a weighted average of L and R and can never exceed
   * the larger of them: the matrix cannot clip.
   */
  it("weights each side by an average, for every level", () => {
    for (let level = 1; level < SIDE_GAIN.length; level++) {
      const { ctx, g } = graph();
      g.set({ night: false, vocals: level });
      const gains = ctx.made.filter((n) => n.kind === "gain").slice(2); // after input, output
      const [ll, rl, lr, rr] = gains.map((n) => n.gain.value);
      expect(ll + rl).toBeCloseTo(1);
      expect(lr + rr).toBeCloseTo(1);
      expect(Math.min(ll, rl, lr, rr)).toBeGreaterThanOrEqual(0);
      // And it lowers the side by exactly the level's gain: S' = (a - b) S.
      expect(ll - rl).toBeCloseTo(SIDE_GAIN[level]);
    }
  });
});

describe("fxApplies", () => {
  const all: ElementFX = { night: true, vocals: 2 };
  it.each([
    [0, { night: false, vocals: 0 }],
    [1, { night: true, vocals: 0 }],
    [2, { night: true, vocals: 2 }],
    [6, { night: false, vocals: 0 }],
  ])("on %i channels engages %o", (channels, want) => {
    expect(fxApplies(all, channels)).toEqual(want);
  });

  it("clamps a vocals level from storage", () => {
    expect(fxApplies({ night: false, vocals: 9 }, 2).vocals).toBe(SIDE_GAIN.length - 1);
    expect(fxApplies({ night: false, vocals: Number.NaN }, 2).vocals).toBe(0);
    expect(fxApplies({ night: false, vocals: -1 }, 2).vocals).toBe(0);
  });
});

describe("softCeiling", () => {
  const knee = 0.5;
  const max = 0.7;
  const curve = softCeiling(knee, max, 2001);
  const at = (x: number) => curve[Math.round(((x + 1) / 2) * (curve.length - 1))];

  it("leaves everything below the knee exactly alone", () => {
    for (const x of [-0.5, -0.25, 0, 0.1, 0.25, 0.5]) expect(at(x)).toBeCloseTo(x, 3);
  });

  it("never passes the ceiling, even at full scale", () => {
    for (const v of curve) expect(Math.abs(v)).toBeLessThanOrEqual(max);
    expect(at(1)).toBeGreaterThan(0.69);
  });

  it("is odd-symmetric, so it adds no offset", () => {
    expect(at(-0.9)).toBeCloseTo(-at(0.9), 6);
  });
});
