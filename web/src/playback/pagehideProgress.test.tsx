/*
 * The last progress write when a browser tab closes (R14-7).
 *
 * The provider forces a save on `pagehide`, and it went through a plain fetch.
 * A browser cancels ordinary requests from a page being unloaded, so the
 * final write was dropped and resume came back up to five seconds early.
 * BookPlayer's own pagehide write already sent keepalive; this one now does.
 * jsdom unloads nothing, so what is asserted is the flag on the request.
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
let writes: { id: string; positionMS: number; watched: boolean; keepalive: boolean }[] = [];
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
        writes.push({
          id: progress[1],
          positionMS: body.position_ms,
          watched: body.watched,
          keepalive: init.keepalive === true,
        });
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

describe("closing the tab", () => {
  it("sends the last progress write with keepalive", async () => {
    await opened();
    await act(async () => reportPosition(1800));
    await settle();
    writes = [];
    await act(async () => {
      window.dispatchEvent(new Event("pagehide"));
    });
    await settle();
    expect(writes).toEqual([{ id: "201", positionMS: 1_800_000, watched: false, keepalive: true }]);
  });

  // Only the unload write: an ordinary throttled save needs no keepalive.
  it("leaves ordinary writes as ordinary requests", async () => {
    await opened();
    await act(async () => reportPosition(1800));
    await settle();
    await act(async () => pb.seekTo(1700));
    await settle();
    expect(writes.length).toBeGreaterThan(0);
    expect(writes.every((w) => !w.keepalive)).toBe(true);
  });
});
