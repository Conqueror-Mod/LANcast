/*
 * The full-screen visualizer, through the real provider and the real Player
 * (docs/now-playing-plan.md, step 3, Testing: Wiring).
 *
 * What is asserted is what a person at the keyboard or the remote gets: `v`
 * fills the screen with the visualizer and Escape gives Now Playing back,
 * before it leaves the window's fullscreen and before it leaves the player;
 * the chrome fades after 2.5 s idle there, exactly as over a film, and comes
 * back on the mouse or any key, while over the plain Now Playing it never
 * fades; `,` and `.` step between visualizers and leave the cover alone; and
 * all of it answers to the key map, so a rebound key works and the default it
 * replaced stops.
 *
 * jsdom has no Web Audio, no canvas and no WebGL: the context and the
 * elements are the fakes visualizerTap.test.tsx uses, a 2D context draws
 * nothing, and WebGL2 is whatever the test says it is.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route, useLocation, useNavigate } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback, resetMusicDeckForTests } from "@/playback/PlaybackProvider";
import { getPrefs, resetPrefs, setPrefs } from "@/playback/prefs";
import { setWebGL2ForTests } from "@/playback/trails";
import { writeDevice } from "@/lib/device";
import { KEYS_STORAGE_KEY, playerKeys, type KeyOverrides } from "@/lib/keys";
import { Player } from "./Player";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

class NoLayoutResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}
(globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = NoLayoutResizeObserver;

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


let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
let path = "";
let go: ReturnType<typeof useNavigate>;
let windowFullscreen = false;

function Probe() {
  pb = usePlayback();
  path = useLocation().pathname;
  go = useNavigate();
  return null;
}

const track = (id: number) => ({
  id,
  title: `Track ${id}`,
  kind: "track",
  parent_id: 50,
  series: "An album",
  duration_ms: (lengths[`/api/stream/${id}`] ?? 30) * 1000,
  progress: { position_ms: 0, watched: false },
  streams: [{ index: 0, kind: "audio", codec: "mp3", channels: 2 }],
});

beforeEach(() => {
  FakeAudio.made = [];
  contexts = [];
  windowFullscreen = false;
  localStorage.clear();
  writeDevice<KeyOverrides>(KEYS_STORAGE_KEY, {});
  resetPrefs();
  resetMusicDeckForTests();
  setWebGL2ForTests(false);
  Element.prototype.scrollIntoView = () => {};
  vi.stubGlobal("Audio", FakeAudio);
  vi.stubGlobal("AudioContext", FakeContext);
  // Frames are asked for and never run: the loop is visualizerTap's subject.
  vi.stubGlobal("requestAnimationFrame", () => 1);
  vi.stubGlobal("cancelAnimationFrame", () => {});
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
  vi.stubGlobal(
    "lancastToggleFullscreen",
    vi.fn(async () => {
      windowFullscreen = !windowFullscreen;
      return windowFullscreen;
    }),
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
      if (url.includes("/playback")) return json({ decision: { method: "direct", reason: "" } });
      if (url.includes("/subtitles")) return json({ tracks: [] });
      if (url.includes("/lyrics")) return json({ synced: false, lines: [] });
      const m = url.match(/\/api\/items\/(\d+)(\?|$)/);
      if (m) {
        const id = Number(m[1]);
        if (id === 50) return json({ id: 50, title: "An album", kind: "album" });
        return json(track(id));
      }
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
  setWebGL2ForTests(undefined);
  resetPrefs();
  localStorage.clear();
});

async function settle(ms = 40) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

function App() {
  return (
    <MemoryRouter initialEntries={["/", "/play"]} initialIndex={1}>
      <PlaybackProvider>
        <Probe />
        <Routes>
          <Route path="/play" element={<Player />} />
          <Route path="/" element={<div id="home" />} />
        </Routes>
      </PlaybackProvider>
    </MemoryRouter>
  );
}

async function playing() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <FocusProvider>
          <App />
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
  await act(async () => pb.play(1, [1, 2, 3]));
  await settle(120);
}

/** A key as the keyboard sends it: to the document, bubbling to the window. */
async function key(k: string) {
  await act(async () => {
    document.dispatchEvent(new KeyboardEvent("keydown", { key: k, bubbles: true }));
  });
  await settle();
}

const player = () => host.querySelector<HTMLElement>(".player")!;
const surface = () => document.querySelector<HTMLElement>(".playback")!;
const column = () => host.querySelector("section.np");
const idle = () => player().classList.contains("player--idle");
const button = (label: RegExp) =>
  [...host.querySelectorAll("button")].find((b) => label.test(b.getAttribute("aria-label") ?? b.textContent ?? ""));

describe("the full-screen visualizer", () => {
  it("opens on v, over the cover by turning the stage into a visualizer, and takes the column away", async () => {
    await playing();
    expect(getPrefs().stage).toBe("cover");
    await key("v");
    expect(pb.vizFull).toBe(true);
    expect(getPrefs().stage).toBe("spectrum");
    expect(player().classList).toContain("player--vizfull");
    expect(surface().classList).toContain("playback--vizfull");
    expect(column(), "Now Playing's column is still on the field").toBeNull();
    expect(document.querySelector(".playback__viz-canvas")).not.toBeNull();
  });

  it("opens on a trail where WebGL2 can draw one", async () => {
    setWebGL2ForTests(true);
    await playing();
    await key("v");
    expect(getPrefs().stage).toBe("tunnel");
    expect(document.querySelector(".playback__viz-canvas")?.getAttribute("data-viz")).toBe("tunnel");
  });

  it("opens from the stage switcher too, and keeps the visualizer already chosen", async () => {
    setPrefs({ stage: "scope" });
    await playing();
    await act(async () => button(/^Full-screen visualizer$/)?.click());
    await settle();
    expect(pb.vizFull).toBe(true);
    expect(getPrefs().stage).toBe("scope");
  });

  it("toggles back to Now Playing on v", async () => {
    await playing();
    await key("v");
    await key("v");
    expect(pb.vizFull).toBe(false);
    expect(column()).not.toBeNull();
  });
});

describe("Escape, in order", () => {
  it("leaves the full-screen visualizer first, then the window's fullscreen, then the player", async () => {
    await playing();
    await act(async () => pb.toggleFullscreen());
    await settle();
    await key("v");
    expect(windowFullscreen).toBe(true);
    expect(pb.vizFull).toBe(true);

    await key("Escape");
    expect(pb.vizFull, "Escape did not return to Now Playing").toBe(false);
    expect(windowFullscreen, "Escape left the window's fullscreen before the visualizer").toBe(true);
    expect(path).toBe("/play");
    expect(column()).not.toBeNull();

    await key("Escape");
    expect(windowFullscreen).toBe(false);
    expect(path).toBe("/play");

    await key("Escape");
    expect(path).toBe("/");
  });

  it("is what the top bar's back does there too, rather than closing the player", async () => {
    await playing();
    await key("v");
    await act(async () => button(/^Back to Now Playing$/)?.click());
    await settle();
    expect(pb.vizFull).toBe(false);
    expect(path).toBe("/play");
  });

  it("ends when the player docks, so coming back is Now Playing and not the field", async () => {
    await playing();
    await key("v");
    expect(pb.vizFull).toBe(true);
    // Away from the player, as the dock button goes: the track plays on, docked.
    await act(async () => go("/"));
    await settle();
    expect(pb.surface).toBe("mini");
    expect(pb.vizFull).toBe(false);
    await act(async () => go("/play"));
    await settle();
    expect(pb.vizFull).toBe(false);
    expect(column()).not.toBeNull();
  });
});

describe("the chrome", () => {
  it("fades after 2.5 s idle over the full-screen visualizer, as over a film", async () => {
    await playing();
    expect(pb.playing).toBe(true);
    await key("v");
    expect(idle()).toBe(false);
    await settle(2700);
    expect(idle(), "the chrome stayed over the field").toBe(true);
  });

  it("comes back on mouse movement", async () => {
    await playing();
    await key("v");
    await settle(2700);
    await act(async () => {
      player().dispatchEvent(new MouseEvent("mousemove", { bubbles: true }));
    });
    expect(idle()).toBe(false);
  });

  it("comes back on any key, even one bound to nothing", async () => {
    await playing();
    await key("v");
    await settle(2700);
    await key("q");
    expect(idle()).toBe(false);
  });

  it("never fades over the plain Now Playing, and is back for good on leaving full screen", async () => {
    await playing();
    await settle(2700);
    expect(idle()).toBe(false);
    await key("v");
    await settle(2700);
    expect(idle()).toBe(true);
    await key("Escape");
    expect(idle()).toBe(false);
    await settle(2700);
    expect(idle(), "Now Playing's chrome faded after full screen").toBe(false);
    }, 12000);
});

describe("the preset keys", () => {
  it("step forward on . and back on , through the visualizers", async () => {
    setPrefs({ stage: "spectrum" });
    await playing();
    await key(".");
    expect(getPrefs().stage).toBe("scope");
    await key(",");
    expect(getPrefs().stage).toBe("spectrum");
    await key(",");
    // Round, and without WebGL2 the trails are not on the way.
    expect(getPrefs().stage).toBe("scope");
  });

  it("step into the trails where WebGL2 can draw them", async () => {
    setWebGL2ForTests(true);
    setPrefs({ stage: "scope" });
    await playing();
    await key(".");
    expect(getPrefs().stage).toBe("tunnel");
  });

  it("do nothing over the cover: routing the music is not a side effect a stray key gets", async () => {
    await playing();
    await key(".");
    await key(",");
    expect(getPrefs().stage).toBe("cover");
    expect(contexts).toHaveLength(0);
  });

  it("leave [ and ] to the subtitles", async () => {
    setPrefs({ stage: "spectrum" });
    await playing();
    await key("]");
    await key("[");
    expect(getPrefs().stage).toBe("spectrum");
  });
});

describe("the key map", () => {
  it("lists v and the preset pair under ?, with the player's keys", () => {
    const rows = playerKeys();
    expect(rows).toContainEqual(["V", "Full-screen visualizer (music)"]);
    expect(rows).toContainEqual([", · .", "Previous · next visualizer (music)"]);
  });

  it("obeys a rebinding: the new key opens full screen and v stops", async () => {
    writeDevice<KeyOverrides>(KEYS_STORAGE_KEY, { vizfull: ["b"], vizpreset: ["9", "0"] });
    setPrefs({ stage: "spectrum" });
    await playing();
    await key("v");
    expect(pb.vizFull).toBe(false);
    await key("b");
    expect(pb.vizFull).toBe(true);
    await key(".");
    expect(getPrefs().stage).toBe("spectrum");
    await key("0");
    expect(getPrefs().stage).toBe("scope");
  });
});

describe("the stage switcher", () => {
  it("offers Trails only with WebGL2", async () => {
    await playing();
    const radios = [...host.querySelectorAll('[role="radio"]')].map((r) => r.textContent);
    expect(radios).toEqual(["Cover", "Spectrum", "Scope"]);
  });

  it("names the trail under the stage, and steps between trails only", async () => {
    setWebGL2ForTests(true);
    setPrefs({ stage: "drift" });
    await playing();
    const radios = [...host.querySelectorAll('[role="radio"]')];
    expect(radios.map((r) => r.textContent)).toEqual(["Cover", "Spectrum", "Scope", "Trails"]);
    expect(radios[3].getAttribute("aria-checked")).toBe("true");
    expect(host.querySelector(".np__step-name")?.textContent).toBe("Drift");
    await act(async () => button(/^Next visualizer$/)?.click());
    await settle();
    // Round within the trails, not out to the spectrum.
    expect(getPrefs().stage).toBe("tunnel");
  });
});
