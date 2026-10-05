/*
 * Routing the media element through Web Audio, and the three traps the plan
 * names (docs/audio-pass-plan.md, Phase 2): a source can be made once per
 * element, the element's own sink stops counting once it is routed, and a
 * context starts suspended. jsdom has no Web Audio, so AudioContext is a fake
 * that records what was asked of it.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { applyElementFX, engineFor, resume, setContextSink } from "./elementEngine";
import { FX_OFF } from "./elementAudio";

class FakeNode {
  channelCountMode = "explicit";
  channelCount = 2;
  gain = { value: 1 };
  threshold = { value: 0 };
  knee = { value: 0 };
  ratio = { value: 0 };
  attack = { value: 0 };
  release = { value: 0 };
  curve: unknown = null;
  oversample = "none";
  connect = vi.fn();
  disconnect = vi.fn();
}

let made: FakeContext[] = [];

class FakeContext {
  state = "suspended";
  destination = { channelCount: 2, maxChannelCount: 6 };
  sinks: string[] = [];
  sources = 0;
  resume = vi.fn(async () => {
    this.state = "running";
  });
  constructor() {
    made.push(this);
  }
  createMediaElementSource() {
    this.sources++;
    return new FakeNode();
  }
  createGain = () => new FakeNode();
  createChannelSplitter = () => new FakeNode();
  createChannelMerger = () => new FakeNode();
  createDynamicsCompressor = () => new FakeNode();
  createWaveShaper = () => new FakeNode();
  setSinkId = vi.fn(async (id: string) => {
    this.sinks.push(id);
  });
}

const NIGHT_ON = { night: true, vocals: 0 };

beforeEach(() => {
  made = [];
  vi.stubGlobal("AudioContext", FakeContext);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("applyElementFX", () => {
  it("leaves an element alone until something is asked of it", () => {
    const el = document.createElement("video");
    expect(applyElementFX(el, FX_OFF, "")).toBeUndefined();
    expect(made).toHaveLength(0);
    expect(engineFor(el)).toBeUndefined();
  });

  it("routes the element once, however often it is asked", () => {
    const el = document.createElement("video");
    applyElementFX(el, NIGHT_ON, "");
    applyElementFX(el, FX_OFF, "");
    applyElementFX(el, { night: false, vocals: 2 }, "");
    expect(made).toHaveLength(1);
    expect(made[0].sources).toBe(1);
  });

  it("keeps the routed engine when everything is turned off", () => {
    const el = document.createElement("video");
    const first = applyElementFX(el, NIGHT_ON, "");
    // createMediaElementSource cannot be called again, so off must not drop it.
    expect(applyElementFX(el, FX_OFF, "")).toBe(first);
    expect(engineFor(el)).toBe(first);
  });

  it("opens the output to every channel the device has", () => {
    const el = document.createElement("video");
    applyElementFX(el, NIGHT_ON, "");
    expect(made[0].destination.channelCount).toBe(6);
  });

  it("sends the context to the chosen device, since the element's sink no longer counts", () => {
    const el = document.createElement("video");
    const engine = applyElementFX(el, NIGHT_ON, "speakers-id")!;
    expect(made[0].sinks).toEqual(["speakers-id"]);
    setContextSink(engine, "headphones-id");
    expect(made[0].sinks).toEqual(["speakers-id", "headphones-id"]);
  });

  it("resumes a suspended context when a control engages", () => {
    const el = document.createElement("video");
    applyElementFX(el, NIGHT_ON, "");
    expect(made[0].resume).toHaveBeenCalled();
  });

  it("resumes only when suspended", async () => {
    const el = document.createElement("video");
    const engine = applyElementFX(el, NIGHT_ON, "")!;
    await resume(engine);
    made[0].resume.mockClear();
    await resume(engine);
    expect(made[0].resume).not.toHaveBeenCalled();
  });

  it("does nothing where the engine has no Web Audio", () => {
    vi.stubGlobal("AudioContext", undefined);
    const el = document.createElement("video");
    expect(applyElementFX(el, NIGHT_ON, "")).toBeUndefined();
  });
});
