/*
 * Gapless through the real provider (docs/gapless-plan.md, step 1).
 *
 * The deck's own tests prove the deck; this proves the wiring, which is where
 * a gapless join can still be lost: the provider has to choose the deck for a
 * directly played track, tell it what comes next only when the queue really
 * will advance, and then advance onto the track that is already sounding
 * without pausing it, reloading it, or starting it again.
 *
 * `Audio` is stubbed, so the deck's two elements are ones the test can drive:
 * play one to its end, and watch what the other does.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback, resetMusicDeckForTests } from "./PlaybackProvider";
import { setPrefs, resetPrefs } from "./prefs";

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
  loads = 0;
  pauses = 0;
  plays = 0;
  constructor() {
    super();
    FakeAudio.made.push(this);
  }
  private fire(t: string) {
    this.dispatchEvent(new Event(t));
  }
  load() {
    this.loads++;
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
    this.plays++;
    this.paused = false;
    this.fire("play");
    this.fire("playing");
    return Promise.resolve();
  }
  pause() {
    if (this.paused) return;
    this.pauses++;
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

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
let notes: string[];

function Probe() {
  pb = usePlayback();
  return null;
}

const track = (id: number) => ({
  id,
  title: `Track ${id}`,
  kind: "track",
  parent_id: 50,
  series: "Live: Beside You in Time",
  duration_ms: (lengths[`/api/stream/${id}`] ?? 30) * 1000,
  progress: { position_ms: 0, watched: false },
  streams: [{ index: 0, kind: "audio", codec: "mp3", channels: 2 }],
});

beforeEach(() => {
  FakeAudio.made = [];
  resetMusicDeckForTests();
  notes = [];
  vi.stubGlobal("Audio", FakeAudio);
  window.lancastClientNote = vi.fn(async (_l: string, _a: string, m: string) => {
    notes.push(m);
    return true;
  });
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
  vi.unstubAllGlobals();
  delete window.lancastClientNote;
});

async function settle(ms = 40) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter>
              <Probe />
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

const playingOn = (url: string) => FakeAudio.made.find((a) => a.src === url);

describe("an album played through the deck", () => {
  it("joins track 1 to track 2 without stopping, reloading or restarting track 2", async () => {
    await render();
    await act(async () => pb.play(1, [1, 2]));
    await settle(80);

    const one = playingOn("/api/stream/1");
    expect(one, "track 1 did not go to the deck").toBeDefined();
    expect(document.querySelector("video")!.getAttribute("src") ?? "").not.toContain("/api/stream/1");

    // Twenty seconds from the end: the provider asks about track 2 and queues it.
    await act(async () => one!.at(15));
    await settle(80);
    const two = playingOn("/api/stream/2");
    expect(two, "track 2 was not loaded ahead").toBeDefined();
    expect(two!.paused).toBe(true);

    // Just before the end the deck starts track 2 on its own.
    await act(async () => one!.at(29.7));
    await settle(400);
    expect(two!.paused, "track 2 did not start before track 1 ended").toBe(false);

    // Track 1 ends; the provider advances and adopts what is already playing.
    await act(async () => one!.end());
    await settle(120);

    expect(pb.itemID).toBe(2);
    expect(two!.pauses, "the advance paused track 2").toBe(0);
    expect(two!.loads, "the advance loaded track 2 again").toBe(1);
    expect(two!.plays, "track 2 was started again").toBe(1);
    expect(notes.some((n) => n.startsWith("track join gapless"))).toBe(true);
  });
});

describe("the taper on the queue's start and end", () => {
  it("fades a session in, leaves the middle alone, and fades the last track out", async () => {
    await render();
    await act(async () => pb.play(1, [1]));
    await settle(80);
    const one = playingOn("/api/stream/1")!;
    // Just after play was pressed: well under full level.
    expect(one.volume).toBeLessThan(0.6);
    await settle(1700);
    expect(one.volume).toBeCloseTo(1, 2);
    // The only track in the queue: its last seconds fade.
    await act(async () => one.at(28));
    await settle(120);
    expect(one.volume).toBeLessThan(0.9);
    expect(one.volume).toBeGreaterThan(0);
  });

  it("does not fade a track that something follows", async () => {
    await render();
    await act(async () => pb.play(1, [1, 2]));
    await settle(1800);
    const one = playingOn("/api/stream/1")!;
    await act(async () => one.at(28));
    await settle(120);
    expect(one.volume).toBeCloseTo(1, 2);
  });

  it("does nothing with the setting off", async () => {
    setPrefs({ taper: false });
    try {
      await render();
      await act(async () => pb.play(1, [1]));
      await settle(80);
      expect(playingOn("/api/stream/1")!.volume).toBeCloseTo(1, 2);
    } finally {
      resetPrefs();
    }
  });
});

describe("with night mode on", () => {
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
    connect() {}
    disconnect() {}
  }
  const contexts: { sources: number; level?: FakeNode }[] = [];
  class FakeContext {
    state = "running";
    destination = { channelCount: 2, maxChannelCount: 2 };
    sources = 0;
    constructor() {
      contexts.push(this);
    }
    resume = async () => {};
    createMediaElementSource() {
      this.sources++;
      return new FakeNode();
    }
    createGain = () => new FakeNode();
    createChannelSplitter = () => new FakeNode();
    createChannelMerger = () => new FakeNode();
    createDynamicsCompressor = () => new FakeNode();
    createWaveShaper = () => new FakeNode();
    setSinkId = async () => {};
  }

  it("still plays music on the deck, through one graph for both elements, and joins gaplessly", async () => {
    contexts.length = 0;
    vi.stubGlobal("AudioContext", FakeContext);
    setPrefs({ nightMusic: true, taper: false });
    try {
      await render();
      await act(async () => pb.play(1, [1, 2]));
      await settle(80);
      expect(notes).toContain("music on deck");
      expect(notes.some((n) => n.startsWith("music on element"))).toBe(false);
      // Both deck elements feed the same context.
      const deckContexts = contexts.filter((c) => c.sources === 2);
      expect(deckContexts).toHaveLength(1);
      // Held at full level before the graph; the slider lives after it.
      const one = playingOn("/api/stream/1")!;
      expect(one.volume).toBe(1);

      await act(async () => one.at(15));
      await settle(80);
      const two = playingOn("/api/stream/2")!;
      await act(async () => one.at(29.7));
      await settle(400);
      await act(async () => one.end());
      await settle(120);
      expect(pb.itemID).toBe(2);
      expect(two.pauses).toBe(0);
      expect(notes.some((n) => n.startsWith("track join gapless"))).toBe(true);
    } finally {
      resetPrefs();
    }
  });
});

describe("asking for the next track", () => {
  it("does not ask for the one after next at the moment of a join", async () => {
    // Seen in the log: "deck queued" for track 3 at the instant track 2 was
    // adopted, five minutes early, because the clock still read track 1's end.
    await render();
    await act(async () => pb.play(1, [1, 2, 3]));
    await settle(80);
    const one = playingOn("/api/stream/1")!;
    await act(async () => one.at(15));
    await settle(80);
    await act(async () => one.at(29.7));
    await settle(400);
    await act(async () => one.end());
    await settle(150);
    expect(pb.itemID).toBe(2);
    expect(notes).toContain("deck queued 2");
    expect(notes, "track 3 was asked for at the join").not.toContain("deck queued 3");
  });
});

