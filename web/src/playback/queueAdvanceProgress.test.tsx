/*
 * Which item a progress write belongs to when the queue moves on.
 *
 * Found watching Futurama play in order on the native player: the episode that
 * had just finished was recorded correctly, and **the episode that had played
 * for no time at all was recorded as finished too**, at its full duration, in
 * the same second. The next episode arrived already watched, on the Continue
 * shelf, with its watch count incremented.
 *
 * The cause is a disagreement that only exists for an instant. Progress was
 * written against the *queue's* current item, while the position it wrote is
 * the *stream's* clock. Those are the same thing except at the end of an item:
 * the queue advances immediately, the clock still reads the end of what just
 * finished, and a save landing in that window carries one item's ending onto
 * the next. Because it is an ending, it arrives as watched.
 *
 * So a write follows the stream, never the queue.
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

/** Two episodes of the same length, the shape the report came in. */
const DURATION_MS = 1_351_423;

function itemBody(id: number) {
  return {
    id,
    title: `Episode ${id}`,
    kind: "episode",
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

describe("a progress write at the end of an item", () => {
  it("names the item that was playing, not the one the queue moved to", async () => {
    await mount();

    await act(async () => pb.play(101, [101, 102]));
    await settle();

    // The source reports itself, which is what makes its clock trustworthy.
    const video = host.querySelector("video")!;
    await act(async () => {
      video.dispatchEvent(new Event("loadedmetadata"));
    });
    await settle();

    // Near the end of the first episode, then the end itself.
    await act(async () => reportPosition(DURATION_MS / 1000 - 0.4));
    await settle();
    await act(async () => {
      video.dispatchEvent(new Event("ended"));
    });
    await settle();

    // The queue has moved on.
    expect(pb.itemID).toBe(102);

    /*
     * Tearing the old source down pauses it, and a pause forces a save past
     * the five-second throttle. The clock still reads the end of the episode
     * that finished, because the next source has not reported yet — this is
     * the write that landed on the wrong item. The next episode has not
     * reported itself yet, which is exactly the window being tested.
     */
    await act(async () => {
      reportPosition(DURATION_MS / 1000);
      video.dispatchEvent(new Event("pause"));
    });
    await settle();

    expect(writes.length).toBeGreaterThan(0);
    expect(writes.every((w) => w.id === "101")).toBe(true);
    // And nothing claimed the second episode was finished.
    expect(writes.some((w) => w.id === "102" && w.watched)).toBe(false);
  });
});
