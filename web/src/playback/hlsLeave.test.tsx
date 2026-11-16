/*
 * Leaving a film tells the server, so its HLS encode stops (T-5).
 *
 * An HLS encode writes segments whether anybody reads them or not. Detaching
 * the element's source ends a progressive stream and did nothing to an HLS
 * one: The Fifth Element kept its ffmpeg for ten minutes after the viewer
 * stopped it (idle_seconds=614), and every title opened in the meantime was
 * slow. Set up as hlsTransport.test.tsx is, with the leave requests recorded.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
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

/*
 * What the *server* does when asked for the playlist.
 *
 * This is the distinction the fallback got wrong for a release. The element
 * raises MEDIA_ERR_SRC_NOT_SUPPORTED both when the engine cannot read a
 * playlist and when the server handed it something that is not one — a 503,
 * say, which is what this server returns when ffmpeg has not written
 * index.m3u8 in time. Only the first says anything about the device.
 */
let playlistServed = true;

/*
 * Which kind of playlist the server says it served — "complete", "growing", or
 * undefined for a server too old to say.
 */
let playlistKind: string | undefined;

/** Every leave sent, and which of them the server has answered. */
let leaves: { url: string; keepalive: boolean }[] = [];
let leaveLanded: string[] = [];
let leaveGate: Promise<void> = Promise.resolve();

beforeEach(() => {
  sources = [];
  canPlay = "maybe";
  playlistServed = true;
  playlistKind = undefined;
  leaves = [];
  leaveLanded = [];
  leaveGate = Promise.resolve();
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
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (init?.method === "DELETE") {
        leaves.push({ url, keepalive: init.keepalive === true });
        await leaveGate;
        leaveLanded.push(url);
        return new Response(null, { status: 204 });
      }
      if (url.includes("/hls/index.m3u8")) {
        return playlistServed
          ? new Response("#EXTM3U", {
              status: 200,
              headers: {
                "Content-Type": "application/vnd.apple.mpegurl",
                ...(playlistKind ? { "X-LANcast-Playlist": playlistKind } : {}),
              },
            })
          : new Response(JSON.stringify({ error: { code: "unavailable" } }), {
              status: 503,
              headers: { "Content-Type": "application/json" },
            });
      }
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


const leftURL = (id: number) => `/api/stream/${id}/hls`;

describe("leaving an item played as segments", () => {
  it("stopping it sends a leave for that item", async () => {
    await render();
    await act(async () => pb.play(7, [7]));
    await settle();
    expect(streams().some((s) => s.includes("/api/stream/7/hls/index.m3u8"))).toBe(true);

    await act(async () => pb.stop());
    await settle();
    expect(leaves.map((l) => l.url)).toEqual([leftURL(7)]);
  });

  it("moving to another title leaves the one before", async () => {
    await render();
    await act(async () => pb.play(7, [7, 8]));
    await settle();
    await act(async () => pb.play(8, [7, 8]));
    await settle();
    expect(leaves.map((l) => l.url)).toEqual([leftURL(7)]);
  });

  it("closing the page leaves with keepalive, so the request outlives it", async () => {
    await render();
    await act(async () => pb.play(7, [7]));
    await settle();
    await act(async () => {
      window.dispatchEvent(new Event("pagehide"));
    });
    expect(leaves).toEqual([{ url: leftURL(7), keepalive: true }]);
  });

  it("a progressive stream sends nothing; detaching it already ends it", async () => {
    canPlay = "";
    await render();
    await act(async () => pb.play(7, [7]));
    await settle();
    expect(streams().some((s) => s.includes("/api/stream/7/transcode"))).toBe(true);
    await act(async () => pb.stop());
    await settle();
    expect(leaves).toEqual([]);
  });

  /*
   * The leave is fire-and-forget, so it could reach the server after the
   * playlist for the same film started again, and stop that encode instead.
   */
  it("playing the same film again waits for its leave to land", async () => {
    await render();
    await act(async () => pb.play(7, [7]));
    await settle();
    let open!: () => void;
    leaveGate = new Promise<void>((r) => (open = r));
    await act(async () => pb.stop());
    await settle();
    const before = streams().length;

    await act(async () => pb.play(7, [7]));
    await settle();
    expect(leaveLanded).toEqual([]);
    expect(streams().length).toBe(before);

    open();
    await settle();
    expect(leaveLanded).toEqual([leftURL(7)]);
    expect(streams().length).toBeGreaterThan(before);
  });
});
