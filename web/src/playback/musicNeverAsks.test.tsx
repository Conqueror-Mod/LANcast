/*
 * Music does not count toward "are you still watching".
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

const DURATION_MS = 200_000;

function itemBody(id: number) {
  return {
    id,
    title: `Track ${id}`,
    kind: id >= 900 ? "episode" : "track",
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
      // Converted, so it plays on the element rather than the gapless deck:
      // the path that reaches the counter.
      if (url.includes("/playback")) return json({ decision: { method: "transcode", reason: "alac" } });
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

/*
 * "Are you still watching" is about a screen playing to an empty room.
 *
 * It counts automatic advances and stops at the third. Music was counted
 * too: an album that had to be converted (so it played on the element
 * rather than the gapless deck) stopped three songs in, behind a button
 * that said Keep watching. Tracks are not counted; episodes still are.
 */
async function runOut(ids: number[]) {
  await mount();
  await act(async () => pb.play(ids[0], ids));
  await settle();
  const video = host.querySelector("video")!;
  for (let i = 0; i < ids.length - 1; i++) {
    await act(async () => {
      video.dispatchEvent(new Event("loadedmetadata"));
    });
    await act(async () => reportPosition(DURATION_MS / 1000 - 0.2));
    await act(async () => {
      video.dispatchEvent(new Event("ended"));
    });
    await settle();
  }
}

describe("the still-watching question", () => {
  it("never stops music, however many tracks roll on", async () => {
    await runOut([1, 2, 3, 4, 5]);
    expect(pb.stillWatching).toBeNull();
    expect(pb.itemID).toBe(5);
  });

  it("still asks after three episodes roll on by themselves", async () => {
    await runOut([901, 902, 903, 904, 905]);
    expect(pb.stillWatching).not.toBeNull();
  });
});
