/*
 * A title played again after it was stopped starts from its saved place, not
 * from where it was last seen playing.
 *
 * Reported as "Play from start on movies doesn't always restart a movie; it
 * often starts it from the last known position". The provider keeps the live
 * position — what item, and where in it — so that reloading the stream for a
 * new audio track does not rewind. It was written on every timeupdate and
 * never cleared, so after watching 1408 to 1:20:00 and stopping, it still said
 * "1408, at 4800 seconds". Play from start forgot the saved position, and the
 * player, finding "already playing this item" in the live position, resumed at
 * 4800 anyway — "often", because only when the title restarted was the last
 * one played.
 *
 * Driven through the real provider, as advanceStart.test.tsx is: a conversion
 * is requested at its start time (?t=), so the stream URL says where it began.
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
let sources: string[] = [];

function Probe() {
  pb = usePlayback();
  return null;
}

// 1408, with its saved position forgotten — what Play from start leaves.
const film = {
  id: 1408,
  title: "1408",
  kind: "movie",
  duration_ms: 6_746_000,
  progress: { position_ms: 0, watched: false },
  media_streams: [
    { index: 0, kind: "video", codec: "hevc" },
    { index: 1, kind: "audio", codec: "aac" },
  ],
};

beforeEach(() => {
  sources = [];
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
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.includes("/playback")) return json({ decision: { method: "transcode", reason: "hevc" } });
      if (/\/api\/items\/1408(\?|$)/.test(url)) return json(film);
      if (url.includes("/api/auth")) return json({ user: { role: "admin" }, can_convert: true });
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function settle(ms = 60) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

function streams() {
  return sources.filter((s) => s.includes("/api/stream/1408"));
}

describe("playing a title again after stopping it", () => {
  it("starts from its saved place, not where it was last playing", async () => {
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
    await act(async () => pb.play(1408, [1408]));
    await settle();
    expect(streams().at(-1)).not.toMatch(/[?&]t=/);

    // Frames arrive, and it is watched to 1:20:00.
    const v = document.querySelector("video")!;
    await act(async () => {
      v.dispatchEvent(new Event("loadeddata"));
    });
    Object.defineProperty(v, "currentTime", { configurable: true, value: 4800 });
    await act(async () => {
      v.dispatchEvent(new Event("timeupdate"));
    });

    await act(async () => pb.stop());
    await settle();
    Object.defineProperty(v, "currentTime", { configurable: true, value: 0 });

    // Played again, its saved position forgotten (as Play from start leaves it).
    await act(async () => pb.play(1408, [1408]));
    await settle();
    const again = streams().at(-1) ?? "";
    expect(again, "never requested again").not.toBe("");
    expect(again, "resumed at the position it was stopped at, 1:20:00").not.toMatch(/[?&]t=4800/);
    expect(again).not.toMatch(/[?&]t=/);
  });
});
