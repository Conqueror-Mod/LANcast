/*
 * Which URL a conversion actually asks for, and what happens when the engine
 * cannot read it.
 *
 * The bug: `All About the Benjamins` — 5.4 Mbps, video copied, audio
 * re-encoded — logged **twelve transcode sessions in eighteen minutes, every
 * one at `start_at=0`**, with no ffmpeg error and nothing reaped. A progressive
 * transcode cannot be range-served, so when Chromium evicts buffered media it
 * can only re-ask from byte zero, and the further into the film the longer that
 * takes. Reported as lagging every few minutes, starting about fifteen minutes
 * in.
 *
 * The unit tests next door cover the choice. These cover the wiring, which is
 * where a correct decision can still reach the element as the wrong URL — and
 * the fallback, which is the part that must work on an engine nobody here has.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback } from "./PlaybackProvider";
import { HLS_VERDICT_KEY } from "./fileTransport";
import { writeDevice } from "@/lib/device";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
let sources: string[] = [];

function Probe() {
  pb = usePlayback();
  return <span data-testid="item">{pb.itemID}</span>;
}

/** A film needing conversion, so the transcode path is the one taken. */
function itemBody(id: number) {
  return {
    id,
    title: "A Film",
    kind: "movie",
    duration_ms: 5_400_000,
    progress: { position_ms: 0, watched: false },
    media_streams: [
      { index: 0, kind: "video", codec: "h264" },
      { index: 1, kind: "audio", codec: "ac3", language: "eng" },
    ],
  };
}

/** What the element claims about playlists. Chromium says "maybe". */
let canPlay = "maybe";

beforeEach(() => {
  sources = [];
  canPlay = "maybe";
  localStorage.clear();
  /*
   * readDevice keeps a module-level cache that outlives clearing localStorage,
   * so the verdict has to be written back rather than merely erased — without
   * this, a test that ends with "refused" silently decides the next one, which
   * then never takes the playlist path it is about.
   */
  writeDevice(HLS_VERDICT_KEY, "unknown");

  const proto = window.HTMLMediaElement.prototype;
  Object.defineProperty(proto, "src", {
    configurable: true,
    get() {
      return this.getAttribute("src") ?? "";
    },
    set(v: string) {
      sources.push(v);
      this.setAttribute("src", v);
    },
  });
  proto.load = vi.fn();
  proto.play = vi.fn(async () => {});
  proto.pause = vi.fn();
  proto.canPlayType = vi.fn(() => canPlay) as unknown as typeof proto.canPlayType;

  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);

  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/playback")) {
        return json({
          decision: {
            method: "transcode",
            reason: "audio codec ac3 is not supported",
          },
        });
      }
      const item = url.match(/\/api\/items\/(\d+)(\?|$)/);
      if (item) return json(itemBody(Number(item[1])));
      if (url.includes("/api/auth")) {
        return json({ user: { role: "admin" }, can_convert: true });
      }
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  localStorage.clear();
});

async function settle(ms = 60) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

async function render() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
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
  await settle(20);
}

const streams = () => sources.filter((s) => s.includes("/api/stream"));
const media = () => host.querySelector("video") as HTMLVideoElement | null;

/** Fire the element's error event with a given MediaError code. */
async function failWith(code: number) {
  const v = media();
  if (!v) throw new Error("no media element");
  Object.defineProperty(v, "error", {
    configurable: true,
    value: { code },
  });
  await act(async () => {
    v.dispatchEvent(new Event("error"));
  });
  await settle();
}

async function start() {
  await render();
  await act(async () => {
    pb.play(1, [1]);
  });
  await settle();
}

describe("delivering a conversion", () => {
  it("asks for a playlist rather than one endless response", async () => {
    await start();
    expect(streams()[0]).toContain("/hls/index.m3u8");
  });

  /*
   * The fallback, which is the part that has to work on an engine nobody here
   * has. canPlayType answers "maybe" whether or not playback will work, so this
   * is discovered by trying — and the cost of finding out must be one reload,
   * not a dead player.
   */
  it("falls back to the endless response when the engine refuses the playlist", async () => {
    await start();
    expect(streams()[0]).toContain("/hls/index.m3u8");

    await failWith(4); // MEDIA_ERR_SRC_NOT_SUPPORTED

    const after = streams();
    expect(after.length).toBeGreaterThan(1);
    expect(after[after.length - 1]).toContain("/transcode");
    expect(after[after.length - 1]).not.toContain("/hls/");
  });

  it("remembers the refusal so the next film does not pay for it again", async () => {
    await start();
    await failWith(4);
    expect(localStorage.getItem(HLS_VERDICT_KEY)).toContain("refused");
  });

  /*
   * A decode error is about this file and a network error about this moment.
   * Treating either as a verdict on the engine would retire the better path for
   * every film, permanently, over one transient fault.
   */
  it("does not blame the playlist for a decode error", async () => {
    await start();
    await failWith(3); // MEDIA_ERR_DECODE
    expect(localStorage.getItem(HLS_VERDICT_KEY)).not.toContain("refused");
  });

  // An engine that says it has no idea what a playlist is, is believed — there
  // is no reason to spend a visibly failed load discovering that.
  it("does not try a playlist on an engine that rules it out", async () => {
    canPlay = "";
    await start();
    expect(streams()[0]).toContain("/transcode");
  });
});
