/*
 * The provider says when a film played to its end with nothing after it.
 *
 * That signal is what the player screen ends on suggestions against
 * (AfterFilm). It must be set only when nothing follows — a queue that rolls
 * on has its own next thing — and it must clear the moment anything plays, or
 * a later pause would bring the suggestions back over a film being watched.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
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

function Probe() {
  pb = usePlayback();
  return <span>{pb.itemID}</span>;
}

/** Films of one length. */
const DURATION_MS = 1_351_423;

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
      if (url.includes("/playback")) return json({ decision: { method: "direct", reason: "" } });
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
  Object.defineProperty(video, "currentTime", { configurable: true, get: () => seconds });
  video.dispatchEvent(new Event("timeupdate"));
}


async function playToEnd() {
  const video = host.querySelector("video")!;
  await act(async () => {
    video.dispatchEvent(new Event("loadedmetadata"));
  });
  await settle();
  await act(async () => reportPosition(DURATION_MS / 1000 - 0.2));
  await settle();
  await act(async () => {
    video.dispatchEvent(new Event("ended"));
  });
  await settle();
}

describe("the end of a film with nothing after it", () => {
  it("is reported against the film that ended", async () => {
    await mount();
    await act(async () => pb.play(201, [201]));
    await settle();
    expect(pb.endedID).toBeNull();
    await playToEnd();
    expect(pb.itemID).toBe(201);
    expect(pb.endedID).toBe(201);
    expect(pb.playing).toBe(false);
  });

  it("is not reported when the queue rolls on", async () => {
    await mount();
    await act(async () => pb.play(201, [201, 202]));
    await settle();
    await playToEnd();
    expect(pb.itemID).toBe(202);
    expect(pb.endedID).toBeNull();
  });

  it("clears when something plays again", async () => {
    await mount();
    await act(async () => pb.play(201, [201]));
    await settle();
    await playToEnd();
    expect(pb.endedID).toBe(201);
    const video = host.querySelector("video")!;
    await act(async () => {
      video.dispatchEvent(new Event("play"));
      video.dispatchEvent(new Event("playing"));
    });
    await settle();
    expect(pb.endedID).toBeNull();
  });
});
