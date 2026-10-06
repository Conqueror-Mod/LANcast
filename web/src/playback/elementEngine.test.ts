/*
 * Routing the media element through Web Audio, and the three traps the plan
 * names (docs/audio-pass-plan.md, Phase 2): a source can be made once per
 * element, the element's own sink stops counting once it is routed, and a
 * context starts suspended. jsdom has no Web Audio, so AudioContext is a fake
 * that records what was asked of it.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import {
  applyElementFX,
  engineFor,
  outputChannels,
  resume,
  setContextSink,
  setElementVolume,
} from "./elementEngine";
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
  destination = { channelCount: 2, maxChannelCount: 8 }; // Sonar's virtual 7.1
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

const NIGHT_ON = { night: true };

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
    expect(applyElementFX(el, FX_OFF, "", 2)).toBeUndefined();
    expect(made).toHaveLength(0);
    expect(engineFor(el)).toBeUndefined();
  });

  it("routes the element once, however often it is asked", () => {
    const el = document.createElement("video");
    applyElementFX(el, NIGHT_ON, "", 2);
    applyElementFX(el, FX_OFF, "", 2);
    applyElementFX(el, NIGHT_ON, "", 2);
    expect(made).toHaveLength(1);
    expect(made[0].sources).toBe(1);
  });

  it("keeps the routed engine when everything is turned off", () => {
    const el = document.createElement("video");
    const first = applyElementFX(el, NIGHT_ON, "", 2);
    // createMediaElementSource cannot be called again, so off must not drop it.
    expect(applyElementFX(el, FX_OFF, "", 2)).toBe(first);
    expect(engineFor(el)).toBe(first);
  });

  /*
   * The bug this replaced: the output was opened to the device's full count,
   * so on a virtual 7.1 device (Sonar reports 8) a stereo track went to the
   * mixer as 7.1 once night mode had been used, and was heard as louder.
   */
  it("sends a stereo track as stereo, even to a 7.1 device", () => {
    const el = document.createElement("video");
    applyElementFX(el, NIGHT_ON, "", 2);
    expect(made[0].destination.channelCount).toBe(2);
  });

  it("follows the source, so a 5.1 film after music is not folded to stereo", () => {
    const el = document.createElement("video");
    applyElementFX(el, NIGHT_ON, "", 2);
    applyElementFX(el, FX_OFF, "", 6);
    expect(made[0].destination.channelCount).toBe(6);
    applyElementFX(el, FX_OFF, "", 2);
    expect(made[0].destination.channelCount).toBe(2);
  });

  it("sends the context to the chosen device, since the element's sink no longer counts", () => {
    const el = document.createElement("video");
    const engine = applyElementFX(el, NIGHT_ON, "speakers-id", 2)!;
    expect(made[0].sinks).toEqual(["speakers-id"]);
    setContextSink(engine, "headphones-id");
    expect(made[0].sinks).toEqual(["speakers-id", "headphones-id"]);
  });

  it("resumes a suspended context when a control engages", () => {
    const el = document.createElement("video");
    applyElementFX(el, NIGHT_ON, "", 2);
    expect(made[0].resume).toHaveBeenCalled();
  });

  it("resumes only when suspended", async () => {
    const el = document.createElement("video");
    const engine = applyElementFX(el, NIGHT_ON, "", 2)!;
    await resume(engine);
    made[0].resume.mockClear();
    await resume(engine);
    expect(made[0].resume).not.toHaveBeenCalled();
  });

  it("does nothing where the engine has no Web Audio", () => {
    vi.stubGlobal("AudioContext", undefined);
    const el = document.createElement("video");
    expect(applyElementFX(el, NIGHT_ON, "", 2)).toBeUndefined();
  });
});

describe("outputChannels", () => {
  it.each([
    [2, 8, 2],
    [1, 8, 2],
    [0, 8, 2],
    [6, 8, 6],
    [8, 8, 8],
    [6, 2, 2],
    [Number.NaN, 8, 2],
  ])("source %s on a %s-channel device -> %s", (src, max, want) => {
    expect(outputChannels(src, max)).toBe(want);
  });
});

/*
 * The volume goes after the graph once the element is routed.
 *
 * The bug this pins: the element's own volume scaled the sound before night
 * mode's compressor, so at a listening level of 10% the compressor saw a
 * signal 20 dB down, compressed almost nothing, added its make-up gain, and
 * night mode came out 4 dB louder than off. Measured live; every earlier lab
 * run had been at full volume.
 */
describe("the player's volume", () => {
  it("is the element's own until the element is routed", () => {
    const el = document.createElement("video");
    setElementVolume(el, 0.3);
    expect(el.volume).toBeCloseTo(0.3);
  });

  it("moves after the graph when night mode first routes the element", () => {
    const el = document.createElement("video");
    el.volume = 0.1;
    const engine = applyElementFX(el, NIGHT_ON, "", 2)!;
    expect(el.volume).toBe(1);
    expect(engine.level.gain.value).toBeCloseTo(0.1);
  });

  it("stays after the graph: the element is held at full volume", () => {
    const el = document.createElement("video");
    const engine = applyElementFX(el, NIGHT_ON, "", 2)!;
    setElementVolume(el, 0.25);
    expect(el.volume).toBe(1);
    expect(engine.level.gain.value).toBeCloseTo(0.25);
  });
});
