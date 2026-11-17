/*
 * A seek inside a finished remux stays in the stream (ledger row 6).
 *
 * A converted stream used to be re-requested at a new `t=` on every seek. For
 * a copied picture that threw away a remux the server had already finished
 * and paid for it again — 24–25 s on a 22-minute episode — and a Watch
 * Together follower, which seeks to keep up, restarted four times in ninety
 * seconds. The rule is in inPlaceSeek.ts; these hold it, and hold the player
 * to it, with an encoded picture and an unfinished playlist as the controls
 * that must still re-request.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback } from "./PlaybackProvider";
import { seeksInPlace, IN_PLACE_END_MARGIN_S } from "./inPlaceSeek";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

describe("seeksInPlace", () => {
  const finished = { path: "hls", videoAction: "copy", offset: 100, duration: 1200 };

  it("stays in a finished remux when the target is inside it", () => {
    expect(seeksInPlace({ ...finished, target: 100 })).toBe(true);
    expect(seeksInPlace({ ...finished, target: 128 })).toBe(true);
    expect(seeksInPlace({ ...finished, target: 1300 - IN_PLACE_END_MARGIN_S })).toBe(true);
  });

  it("re-requests a target before the stream begins, or past its end", () => {
    expect(seeksInPlace({ ...finished, target: 99 })).toBe(false);
    expect(seeksInPlace({ ...finished, target: 1300 })).toBe(false);
  });

  it("re-requests when the picture is encoded: those segments may not exist yet", () => {
    expect(seeksInPlace({ ...finished, videoAction: "encode", target: 128 })).toBe(false);
    // A fallback decision names no action, and is treated the same way.
    expect(seeksInPlace({ ...finished, videoAction: undefined, target: 128 })).toBe(false);
  });

  it("re-requests a playlist that is still growing, or not known yet", () => {
    expect(seeksInPlace({ ...finished, duration: Infinity, target: 128 })).toBe(false);
    expect(seeksInPlace({ ...finished, duration: NaN, target: 128 })).toBe(false);
    expect(seeksInPlace({ ...finished, duration: 0, target: 128 })).toBe(false);
  });

  it("re-requests the progressive response, which has nothing ahead to seek to", () => {
    expect(seeksInPlace({ ...finished, path: "progressive", target: 128 })).toBe(false);
  });
});

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
/** The server's decision for the film, and what the element says it holds. */
let videoAction: string | undefined = "copy";
let streamDuration = 1320;

function Probe() {
  pb = usePlayback();
  return <span>{pb.itemID}</span>;
}

beforeEach(() => {
  videoAction = "copy";
  streamDuration = 1320; // a 22-minute episode, remuxed from the start
  localStorage.clear();

  const proto = window.HTMLMediaElement.prototype;
  proto.load = vi.fn();
  proto.play = vi.fn(async () => {});
  proto.pause = vi.fn();
  // An engine that plays playlists natively, as Chromium does.
  proto.canPlayType = vi.fn((t: string) => (t.includes("mpegurl") ? "maybe" : "")) as never;
  Object.defineProperty(proto, "duration", {
    configurable: true,
    get: () => streamDuration,
  });

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
      if (/\/progress$/.test(url) && init?.method === "PUT") return new Response(null, { status: 204 });
      if (url.includes("/playback"))
        return json({
          decision: {
            method: "transcode",
            reason: "audio codec ac3 is not supported",
            video_action: videoAction,
            audio_action: "encode",
          },
        });
      const item = url.match(/\/api\/items\/(\d+)(\?|$)/);
      if (item)
        return json({
          id: Number(item[1]),
          title: "An episode",
          kind: "movie",
          duration_ms: 1_320_000,
          progress: { position_ms: 0, watched: false },
          streams: [
            { index: 0, kind: "video", codec: "h264" },
            { index: 1, kind: "audio", codec: "ac3", language: "eng" },
          ],
        });
      if (url.includes("/api/auth")) return json({ user: { role: "admin" }, can_convert: true });
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

async function settle(ms = 80) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

/** Opens the episode and returns its element, recording clock moves. */
async function opened() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <FocusProvider>
            <PlaybackProvider>
              <Probe />
            </PlaybackProvider>
          </FocusProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await settle();
  await act(async () => pb.play(301, [301]));
  await settle();
  const video = host.querySelector("video")!;
  const moves: number[] = [];
  Object.defineProperty(video, "currentTime", {
    configurable: true,
    get: () => 0,
    set: (t: number) => moves.push(t),
  });
  await act(async () => {
    video.dispatchEvent(new Event("loadedmetadata"));
    video.dispatchEvent(new Event("canplay"));
    video.dispatchEvent(new Event("playing"));
  });
  await settle();
  return { video, moves };
}

describe("a seek on a converted episode", () => {
  it("inside a finished remux moves the clock and keeps the stream", async () => {
    const { video, moves } = await opened();
    const src = video.getAttribute("src");
    expect(src).toContain("/hls/");
    expect(pb.seekRestarts(128)).toBe(false);
    expect(pb.loading).toBe(false);

    await act(async () => pb.seekTo(128));
    await settle();

    expect(video.getAttribute("src")).toBe(src);
    expect(moves).toContain(128);
    expect(pb.loading).toBe(false);
  });

  it("re-requests when the picture is encoded (the control)", async () => {
    videoAction = "encode";
    const { video, moves } = await opened();
    const src = video.getAttribute("src");
    expect(pb.seekRestarts(128)).toBe(true);

    await act(async () => pb.seekTo(128));
    await settle();

    expect(video.getAttribute("src")).not.toBe(src);
    expect(video.getAttribute("src")).toContain("t=128");
    expect(moves).not.toContain(128);
  });

  it("re-requests while the remux was served unfinished", async () => {
    streamDuration = Infinity;
    const { video } = await opened();
    expect(pb.seekRestarts(128)).toBe(true);

    await act(async () => pb.seekTo(128));
    await settle();

    expect(video.getAttribute("src")).toContain("t=128");
  });
});
