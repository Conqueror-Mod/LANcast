/*
 * What a seek writes as the resume position (R14-1).
 *
 * A seek forces a progress write, and that write ran through the
 * `saveProgress` from the last render, whose clock is the element's clock from
 * *before* the seek. On a converted film the seek has already moved the offset
 * to the target, so the write was the new offset plus the old clock: an hour
 * into a 2 h film, going back 100 s wrote 7,100 s — past 0.92 of the runtime,
 * so **watched**, which on the server logs a viewing, bumps the count and takes
 * the film off the watchlist. A seek writes where it went.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback } from "./PlaybackProvider";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
/** Every progress write: the item it named and what it said. */
let writes: { id: string; positionMS: number; watched: boolean }[] = [];
/** What the server decides for every item in a test. */
let method: "direct" | "transcode" = "transcode";

function Probe() {
  pb = usePlayback();
  return <span>{pb.itemID}</span>;
}

/** A 2 h film. */
const DURATION_MS = 7_200_000;

function itemBody(id: number) {
  return {
    id,
    title: `Film ${id}`,
    kind: "movie",
    duration_ms: DURATION_MS,
    progress: { position_ms: 0, watched: false },
    streams: [
      { index: 0, kind: "video", codec: "h264" },
      { index: 1, kind: "audio", codec: "aac", language: "eng" },
    ],
  };
}

beforeEach(() => {
  writes = [];
  method = "transcode";
  localStorage.clear();

  const proto = window.HTMLMediaElement.prototype;
  proto.load = vi.fn();
  proto.play = vi.fn(async () => {});
  proto.pause = vi.fn();
  Object.defineProperty(proto, "duration", {
    configurable: true,
    get: () => DURATION_MS / 1000,
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
      const progress = url.match(/\/api\/items\/(\d+)\/progress$/);
      if (progress && init?.method === "PUT") {
        const body = JSON.parse(String(init.body)) as {
          position_ms: number;
          watched: boolean;
        };
        writes.push({ id: progress[1], positionMS: body.position_ms, watched: body.watched });
        return new Response(null, { status: 204 });
      }
      if (url.includes("/playback")) return json({ decision: { method, reason: method === "direct" ? "" : "test" } });
      const item = url.match(/\/api\/items\/(\d+)(\?|$)/);
      if (item) return json(itemBody(Number(item[1])));
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

async function mount() {
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
}

/** The element reporting where it is, which is what drives a save. */
function reportPosition(seconds: number) {
  const video = host.querySelector("video");
  if (!video) throw new Error("no media element");
  Object.defineProperty(video, "currentTime", {
    configurable: true,
    get: () => seconds,
    set: () => {},
  });
  video.dispatchEvent(new Event("timeupdate"));
}


async function opened() {
  await mount();
  await act(async () => pb.play(201, [201]));
  await settle();
  const video = host.querySelector("video")!;
  await act(async () => {
    video.dispatchEvent(new Event("loadedmetadata"));
  });
  await settle();
  return video;
}

describe("the progress a seek writes", () => {
  it("is the target on a converted film, not the target plus the old clock", async () => {
    await opened();
    // An hour in, converted from the start (offset 0).
    await act(async () => reportPosition(3600));
    await settle();
    writes = [];

    await act(async () => pb.seekTo(3500)); // back 100 s
    await settle();

    expect(writes[0]).toEqual({ id: "201", positionMS: 3_500_000, watched: false });
    expect(writes.some((w) => w.watched)).toBe(false);
  });

  it("does not mark the film watched when a forward seek plus the old clock passes 92%", async () => {
    await opened();
    await act(async () => reportPosition(600)); // ten minutes in
    await settle();
    writes = [];

    await act(async () => pb.seekTo(6200)); // to 1h43; plus the old clock would be 94%
    await settle();

    expect(writes[0]).toEqual({ id: "201", positionMS: 6_200_000, watched: false });
  });

  it("is the target on a direct film too", async () => {
    method = "direct";
    await opened();
    await act(async () => reportPosition(3600));
    await settle();
    writes = [];

    await act(async () => pb.seekTo(1200));
    await settle();

    expect(writes[0]).toEqual({ id: "201", positionMS: 1_200_000, watched: false });
  });
});
