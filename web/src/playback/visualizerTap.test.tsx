/*
 * The visualizer's tap, through the real provider (docs/now-playing-plan.md,
 * step 2, Testing: Wiring).
 *
 * The engine's own tests prove the analyser sits where it should on a graph
 * built by hand; this proves the provider asks for it at the right moments:
 * showing a visualizer builds the engine with every effect off, on the path
 * the music is actually on and no other; the cover or the mini player builds
 * nothing; a crossfade keeps the one analyser; and the one animation loop stops
 * when the surface leaves full or the music pauses.
 *
 * jsdom has no Web Audio and no canvas: AudioContext is a fake that records
 * every connection, and a 2D context is a stub that draws nothing.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback, useFullSurface, resetMusicDeckForTests } from "./PlaybackProvider";
import { setPrefs, resetPrefs, type Prefs } from "./prefs";
import { vizShouldRun } from "./Visualizer";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const lengths: Record<string, number> = { "/api/stream/1": 30, "/api/stream/2": 40, "/api/stream/3": 50 };

class FakeAudio extends EventTarget {
  static made: FakeAudio[] = [];
  src = "";
  currentTime = 0;
  duration = NaN;
  paused = true;
  ended = false;
  volume = 1;
  muted = false;
  playbackRate = 1;
  preload = "";
  readyState = 0;
  error = null;
  constructor() {
    super();
    FakeAudio.made.push(this);
  }
  private fire(t: string) {
    this.dispatchEvent(new Event(t));
  }
  load() {
    this.ended = false;
    this.paused = true;
    this.currentTime = 0;
    this.duration = lengths[this.src] ?? NaN;
    this.readyState = this.src ? 4 : 0;
    if (this.src) {
      this.fire("loadstart");
      this.fire("loadedmetadata");
      this.fire("durationchange");
    }
  }
  play() {
    if (!this.src) return Promise.resolve();
    this.paused = false;
    this.fire("play");
    this.fire("playing");
    return Promise.resolve();
  }
  pause() {
    if (this.paused) return;
    this.paused = true;
    this.fire("pause");
  }
  removeAttribute() {
    this.src = "";
  }
  at(t: number) {
    this.currentTime = t;
    this.fire("timeupdate");
  }
  end() {
    this.currentTime = this.duration;
    this.paused = true;
    this.ended = true;
    this.fire("pause");
    this.fire("ended");
  }
}

// ---- a Web Audio that remembers what was connected to what -----------------

class FakeNode {
  kind: string;
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
  targets: unknown[] = [];
  constructor(kind: string) {
    this.kind = kind;
  }
  connect(t: unknown) {
    this.targets.push(t);
  }
  disconnect() {
    // The graph re-routes its own middle; the ends this test reads are
    // connected once, at build, and never disconnected.
  }
}

class FakeAnalyser extends FakeNode {
  fftSize = 2048;
  smoothingTimeConstant = 0.8;
  frequencyBinCount = 1024;
  context = { sampleRate: 48000 };
  constructor() {
    super("analyser");
  }
  getByteFrequencyData(a: Uint8Array) {
    a.fill(200);
  }
  getByteTimeDomainData(a: Uint8Array) {
    a.fill(128);
  }
}

let contexts: FakeContext[] = [];

class FakeContext {
  state = "running";
  destination = { channelCount: 2, maxChannelCount: 2 };
  sources = 0;
  nodes: FakeNode[] = [];
  analysers: FakeAnalyser[] = [];
  biquads = 0;
  compressors = 0;
  constructor() {
    contexts.push(this);
  }
  resume = async () => {};
  private node(kind: string) {
    const n = new FakeNode(kind);
    this.nodes.push(n);
    return n;
  }
  createMediaElementSource() {
    this.sources++;
    return this.node("source");
  }
  createGain = () => this.node("gain");
  createDynamicsCompressor = () => {
    this.compressors++;
    return this.node("compressor");
  };
  createWaveShaper = () => this.node("shaper");
  createBiquadFilter = () => {
    this.biquads++;
    return Object.assign(this.node("biquad"), { type: "", frequency: { value: 0 }, Q: { value: 0 } });
  };
  createAnalyser = () => {
    const a = new FakeAnalyser();
    this.analysers.push(a);
    return a;
  };
  setSinkId = async () => {};
  /** `level`: the one node connected to the destination. */
  level() {
    return this.nodes.find((n) => n.targets.includes(this.destination));
  }
}

// ---- an animation clock the test can count ---------------------------------

let frames: Map<number, FrameRequestCallback>;
let nextFrame: number;
let requested: number;
let cancelled: number;

function runFrame() {
  const due = [...frames.entries()];
  frames.clear();
  for (const [, cb] of due) cb(performance.now());
}

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;

function Probe() {
  pb = usePlayback();
  return null;
}

function Full() {
  useFullSurface();
  return null;
}

const track = (id: number) => ({
  id,
  title: `Track ${id}`,
  kind: "track",
  parent_id: id === 3 ? 60 : 50,
  series: "An album",
  duration_ms: (lengths[`/api/stream/${id}`] ?? 30) * 1000,
  progress: { position_ms: 0, watched: false },
  streams: [{ index: 0, kind: "audio", codec: "mp3", channels: 2 }],
});

let method = "direct";

beforeEach(() => {
  FakeAudio.made = [];
  contexts = [];
  method = "direct";
  frames = new Map();
  nextFrame = 1;
  requested = 0;
  cancelled = 0;
  resetMusicDeckForTests();
  vi.stubGlobal("Audio", FakeAudio);
  vi.stubGlobal("AudioContext", FakeContext);
  vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
    requested++;
    const id = nextFrame++;
    frames.set(id, cb);
    return id;
  });
  vi.stubGlobal("cancelAnimationFrame", (id: number) => {
    if (frames.delete(id)) cancelled++;
  });
  // A 2D context that draws nothing, so the loop can run to its end.
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockImplementation(
    () =>
      new Proxy(
        {},
        {
          get: (_t, k) =>
            k === "createLinearGradient" ? () => ({ addColorStop() {} }) : typeof k === "string" ? () => {} : undefined,
          set: () => true,
        },
      ) as unknown as CanvasRenderingContext2D,
  );
  window.lancastClientNote = vi.fn(async () => true);
  const proto = window.HTMLMediaElement.prototype;
  proto.load = vi.fn();
  proto.play = vi.fn(async () => {});
  proto.pause = vi.fn();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      const json = (b: unknown) =>
        new Response(JSON.stringify(b), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.includes("/playback")) return json({ decision: { method, reason: "" } });
      const m = url.match(/\/api\/items\/(\d+)(\?|$)/);
      if (m) return json(track(Number(m[1])));
      if (url.includes("/api/auth")) return json({ user: { role: "admin" }, can_convert: true });
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  delete window.lancastClientNote;
  resetPrefs();
});

async function settle(ms = 40) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

const client = () => new QueryClient({ defaultOptions: { queries: { retry: false } } });
let qc: QueryClient;

async function render(full: boolean) {
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter>
              <Probe />
              {full && <Full />}
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

beforeEach(() => {
  qc = client();
});

const playingOn = (url: string) => FakeAudio.made.find((a) => a.src === url);
const effectsOff: Partial<Prefs> = { nightMusic: false, eq: [0, 0, 0, 0, 0], eqPreset: "flat", taper: false };

describe("showing a visualizer", () => {
  it("builds the deck's engine with every effect off: one context, no equaliser, no compressor in the path", async () => {
    setPrefs({ ...effectsOff, stage: "spectrum" });
    await render(true);
    await act(async () => pb.play(1, [1, 2]));
    await settle(80);
    const deck = contexts.filter((c) => c.sources === 2);
    expect(deck, "the deck was not routed for the visualizer").toHaveLength(1);
    expect(deck[0].biquads).toBe(0);
    // The compressor is built once with the graph, and never connected into it.
    const comp = deck[0].nodes.find((n) => n.kind === "compressor")!;
    expect(deck[0].nodes.some((n) => n.targets.includes(comp))).toBe(false);
  });

  it("hangs the analyser off the graph's output, beside level and so before it, and connects it to nothing", async () => {
    setPrefs({ ...effectsOff, stage: "spectrum" });
    await render(true);
    await act(async () => pb.play(1, [1, 2]));
    await settle(80);
    const ctx = contexts.find((c) => c.sources === 2)!;
    expect(ctx.analysers).toHaveLength(1);
    const analyser = ctx.analysers[0];
    const level = ctx.level()!;
    expect(level, "nothing reaches the destination").toBeDefined();
    // One node feeds both: that is graph.output, after the graph and before the slider.
    const output = ctx.nodes.filter((n) => n.targets.includes(analyser) && n.targets.includes(level));
    expect(output).toHaveLength(1);
    expect(analyser.targets).toEqual([]);
    expect(analyser.smoothingTimeConstant).toBe(0);
  });

  it("moves the slider to level when picked mid-track, and the next crossfade is not scaled by it twice", async () => {
    setPrefs({ ...effectsOff, stage: "cover", crossfade: 2 });
    await render(true);
    await act(async () => pb.changeVolume(0.4));
    await act(async () => pb.play(1, [1, 3]));
    await settle(80);
    const one = playingOn("/api/stream/1")!;
    expect(contexts).toHaveLength(0);
    expect(one.volume).toBeCloseTo(0.4, 2);
    // The person picks Scope while the track plays.
    await act(async () => setPrefs({ stage: "scope" }));
    await settle();
    const ctx = contexts.find((c) => c.sources === 2)!;
    expect(ctx, "picking a visualizer did not route the deck").toBeDefined();
    expect(ctx.level()!.gain.value).toBeCloseTo(0.4, 2);
    expect(one.volume).toBe(1);
    // The deck fades its elements against its own level. That has to be 1
    // now the slider is at `level`, or the fade runs at 0.4 and the sum of
    // squares is 0.16, not 1: the slider applied twice.
    await act(async () => one.at(15));
    await settle(80);
    await act(async () => one.at(27.5));
    await settle(1500);
    const three = playingOn("/api/stream/3")!;
    expect(three.paused, "the crossfade did not start").toBe(false);
    expect(one.volume ** 2 + three.volume ** 2).toBeCloseTo(1, 1);
  });

  it("routes nothing while the stage shows the cover", async () => {
    setPrefs({ ...effectsOff, stage: "cover" });
    await render(true);
    await act(async () => pb.play(1, [1, 2]));
    await settle(80);
    expect(contexts).toHaveLength(0);
  });

  it("routes nothing on the mini player, where the cover shows instead", async () => {
    setPrefs({ ...effectsOff, stage: "spectrum" });
    await render(false);
    await act(async () => pb.play(1, [1, 2]));
    await settle(80);
    expect(pb.surface).toBe("mini");
    expect(contexts).toHaveLength(0);
    expect(host.ownerDocument.querySelector(".playback__viz-canvas")).toBeNull();
  });

  it("taps the element, not the deck, when the track plays on the element (converted)", async () => {
    method = "transcode";
    setPrefs({ ...effectsOff, stage: "spectrum" });
    await render(true);
    await act(async () => pb.play(1, [1]));
    await settle(80);
    expect(contexts.filter((c) => c.sources === 2), "the idle deck was routed").toHaveLength(0);
    const el = contexts.filter((c) => c.sources === 1);
    expect(el, "the element was not routed for the visualizer").toHaveLength(1);
    expect(el[0].analysers).toHaveLength(1);
  });

  it("keeps the one analyser through a crossfade: no second context, no second tap", async () => {
    setPrefs({ ...effectsOff, stage: "spectrum", crossfade: 2 });
    await render(true);
    await act(async () => pb.play(1, [1, 3]));
    await settle(80);
    const one = playingOn("/api/stream/1")!;
    await act(async () => one.at(15));
    await settle(80);
    await act(async () => one.at(27.5));
    await settle(1500);
    await act(async () => one.end());
    await settle(150);
    expect(pb.itemID).toBe(3);
    expect(contexts).toHaveLength(1);
    expect(contexts[0].analysers).toHaveLength(1);
  });
});

describe("the animation loop", () => {
  it("runs while a visualizer shows on the full surface and the music plays", async () => {
    setPrefs({ ...effectsOff, stage: "spectrum" });
    await render(true);
    await act(async () => pb.play(1, [1, 2]));
    await settle(80);
    expect(document.querySelector(".playback__viz-canvas")).not.toBeNull();
    const before = requested;
    await act(async () => runFrame());
    await act(async () => runFrame());
    expect(requested).toBeGreaterThan(before);
    // One loop, not one per render: a single frame is ever pending.
    expect(frames.size).toBe(1);
  });

  it("stops when the surface leaves full, and the canvas goes with it", async () => {
    setPrefs({ ...effectsOff, stage: "spectrum" });
    await render(true);
    await act(async () => pb.play(1, [1, 2]));
    await settle(80);
    expect(frames.size).toBe(1);
    await render(false);
    expect(pb.surface).toBe("mini");
    expect(frames.size, "a frame is still pending on the mini player").toBe(0);
    expect(cancelled).toBeGreaterThan(0);
    expect(document.querySelector(".playback__viz-canvas")).toBeNull();
    const after = requested;
    await act(async () => runFrame());
    expect(requested).toBe(after);
  });

  it("stops while paused and keeps the canvas, so the last frame holds", async () => {
    setPrefs({ ...effectsOff, stage: "scope" });
    await render(true);
    await act(async () => pb.play(1, [1, 2]));
    await settle(80);
    expect(frames.size).toBe(1);
    await act(async () => playingOn("/api/stream/1")!.pause());
    await settle();
    expect(frames.size).toBe(0);
    expect(document.querySelector(".playback__viz-canvas")).not.toBeNull();
  });

  it("stops when the stage goes back to the cover", async () => {
    setPrefs({ ...effectsOff, stage: "spectrum" });
    await render(true);
    await act(async () => pb.play(1, [1, 2]));
    await settle(80);
    await act(async () => setPrefs({ stage: "cover" }));
    await settle();
    expect(frames.size).toBe(0);
    expect(document.querySelector(".playback__viz-canvas")).toBeNull();
  });
});

describe("vizShouldRun", () => {
  const on = { surface: "full", stage: "spectrum", playing: true, hidden: false } as const;
  it("runs on the full surface, playing, visible, with a visualizer chosen", () => {
    expect(vizShouldRun(on)).toBe(true);
  });
  it("does not run on the mini player", () => {
    expect(vizShouldRun({ ...on, surface: "mini" })).toBe(false);
  });
  it("does not run in a hidden tab", () => {
    expect(vizShouldRun({ ...on, hidden: true })).toBe(false);
  });
  it("does not run while paused", () => {
    expect(vizShouldRun({ ...on, playing: false })).toBe(false);
  });
  it("does not run with the cover on the stage", () => {
    expect(vizShouldRun({ ...on, stage: "cover" })).toBe(false);
  });
});
