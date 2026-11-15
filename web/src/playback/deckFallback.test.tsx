/*
 * A music track that fails direct play (R14-3).
 *
 * A directly played track sounds through the music deck. When it failed, the
 * conversion was loaded into the video element while the provider still
 * treated the deck as the player, so Pause, seek and volume reached a dead
 * deck, and Stop left the converted track sounding underneath with nothing
 * able to reach it. The fallback hands the track over from the deck.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback, resetMusicDeckForTests } from "./PlaybackProvider";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const lengths: Record<string, number> = { "/api/stream/301": 240 };

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
  error: { code: number; message: string } | null = null;
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
/** Which kind of element each pause reached, video or the deck. */
let pausedOn: string[] = [];

function Probe() {
  pb = usePlayback();
  return null;
}

/** A FLAC track the server is happy to play directly. */
const track = (id: number) => ({
  id,
  title: `Track ${id}`,
  kind: "track",
  container: "flac",
  duration_ms: 240_000,
  progress: { position_ms: 0, watched: false },
  streams: [{ index: 0, kind: "audio", codec: "flac", channels: 2 }],
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
  proto.pause = vi.fn(function (this: HTMLMediaElement) {
    pausedOn.push(this.tagName.toLowerCase());
  });
  pausedOn = [];
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

const video = () => document.querySelector("video")!;

async function failOnDeck() {
  await render();
  await act(async () => pb.play(301, [301]));
  await settle(80);
  const deck = FakeAudio.made.find((a) => a.src === "/api/stream/301");
  expect(deck, "the track did not go to the deck").toBeDefined();
  await act(async () => {
    deck!.error = { code: 3, message: "" };
    deck!.dispatchEvent(new Event("error"));
  });
  await settle(80);
  return deck!;
}

describe("a track that fails on the deck", () => {
  it("is converted on the element, with the deck let go", async () => {
    const deck = await failOnDeck();
    expect(video().getAttribute("src") ?? "").toMatch(/\/api\/stream\/301\/(transcode|hls)/);
    expect(deck.src).toBe("");
    expect(FakeAudio.made.every((a) => a.src === "")).toBe(true);
  });

  it("takes Pause on the element that is playing it", async () => {
    const deck = await failOnDeck();
    const deckPauses = deck.pauses;
    // The conversion is playing.
    Object.defineProperty(video(), "paused", { configurable: true, get: () => false });
    await act(async () => pb.togglePlay());
    await settle();
    expect(pausedOn).toContain("video");
    expect(deck.pauses).toBe(deckPauses);
  });

  it("is silenced by Stop", async () => {
    await failOnDeck();
    await act(async () => pb.stop());
    await settle();
    expect(video().getAttribute("src") ?? "").toBe("");
  });
});
