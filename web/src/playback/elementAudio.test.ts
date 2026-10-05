/*
 * Music's night mode graph (docs/audio-pass-plan.md, Phase 2).
 *
 * What it does to sound was measured offline, through Chromium's own Web Audio
 * and ffmpeg's ebur128 (the plan's "Phase 2, measured"). jsdom has no Web
 * Audio, so what is pinned here is the part a measurement cannot see once it
 * has passed: which nodes the sound goes through, and the numbers it was
 * measured with.
 */
import { describe, it, expect } from "vitest";
import { buildGraph, fxApplies, softCeiling, NIGHT } from "./elementAudio";

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

describe("the night graph's routing", () => {
  it("is a straight wire when off", () => {
    const { input, output } = graph();
    expect(input.out).toEqual([output]);
  });

  it("goes through the compressor, the trim and the ceiling when on", () => {
    const { g, input, output } = graph();
    g.set({ night: true });
    const p = path(input);
    expect(p).toEqual(["gain", "comp", "gain", "shaper", "gain"]);
    expect(input.out).not.toContain(output);
  });

  it("returns to a straight wire, rather than leaving the chain attached", () => {
    const { g, input, output, ctx } = graph();
    g.set({ night: true });
    g.set({ night: false });
    expect(input.out).toEqual([output]);
    // The ceiling no longer feeds the output either, so nothing plays twice
    // if the chain is ever fed again.
    const shaper = ctx.made.find((n) => n.kind === "shaper")!;
    expect(shaper.out).toEqual([]);
  });

  it("lets neither end fold channels, for the films that share the element", () => {
    const { input, output } = graph();
    expect(input.channelCountMode).toBe("max");
    expect(output.channelCountMode).toBe("max");
  });

  it("uses the measured numbers", () => {
    const { ctx } = graph();
    const comp = ctx.made.find((n) => n.kind === "comp")!;
    expect(comp.threshold.value).toBe(NIGHT.threshold);
    expect(comp.ratio.value).toBe(NIGHT.ratio);
    expect(comp.release.value).toBe(NIGHT.release);
    // The trim sits after the compressor and lowers: the compressor's own
    // make-up gain left every track at ordinary listening level.
    const trim = ctx.made.filter((n) => n.kind === "gain")[2];
    expect(NIGHT.trimDb).toBeLessThan(0);
    expect(trim.gain.value).toBeCloseTo(Math.pow(10, NIGHT.trimDb / 20));
  });
});

describe("fxApplies", () => {
  it.each([
    [0, false],
    [1, true],
    [2, true],
    [6, false],
  ])("on %i channels engages night mode: %s", (channels, want) => {
    expect(fxApplies({ night: true }, channels).night).toBe(want);
  });

  it("never engages what was not asked for", () => {
    expect(fxApplies({ night: false }, 2).night).toBe(false);
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
