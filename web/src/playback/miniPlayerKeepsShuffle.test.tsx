/*
 * Back from the mini-player, a shuffled queue is where it was.
 *
 * Reported by Chris: shuffle the whole music library, reach track 41 of 9,000,
 * drop to the mini-player and expand it again, and the same track reads
 * "1 of 9000", visibly reloading. shuffleForEntry decided correctly that
 * shuffle should stay on, and its tests said so; the Player screen then
 * *set* it on, and setting shuffle on is a new randomisation (Randomize all
 * relies on that). So the whole order was reshuffled with the playing track
 * moved to the front. The decision was tested; what the call did was not, so
 * this drives the real screen, the real provider and a real return.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route, useNavigate, type NavigateFunction } from "react-router";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback } from "./PlaybackProvider";
import { Player } from "@/screens/Player";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const QUEUE = Array.from({ length: 60 }, (_, i) => 100 + i);

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
let nav: NavigateFunction;

function Probe() {
  pb = usePlayback();
  nav = useNavigate();
  return null;
}

beforeEach(() => {
  const proto = window.HTMLMediaElement.prototype;
  proto.load = vi.fn();
  proto.play = vi.fn(async () => {});
  proto.pause = vi.fn();
  Object.defineProperty(proto, "duration", { configurable: true, get: () => 600 });
  Object.defineProperty(proto, "currentTime", { configurable: true, get: () => 0, set: () => {} });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.includes("/progress")) return new Response(null, { status: 204 });
      if (url.includes("/playback")) return json({ decision: { method: "direct", reason: "" } });
      if (url.includes("/preroll")) return json({ clip: null });
      const m = url.match(/\/api\/items\/(\d+)(\?|$)/);
      if (m) {
        const id = Number(m[1]);
        return json({ id, title: `Film ${id}`, kind: "movie", library_id: 1, duration_ms: 600_000, container: "mp4", artwork: {}, streams: [] });
      }
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

async function randomizeAll() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={[{ pathname: `/watch/${QUEUE[0]}`, state: { queue: QUEUE, shuffle: true } }]}>
          <FocusProvider>
            <PlaybackProvider>
              <Probe />
              <Routes>
                <Route path="/watch/:id" element={<Player />} />
                <Route path="/" element={null} />
              </Routes>
            </PlaybackProvider>
          </FocusProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await settle();
}

describe("returning from the mini-player", () => {
  it("keeps the shuffled order and the place in it", async () => {
    await randomizeAll();
    expect(pb.shuffle).toBe(true);
    const order = [...pb.playOrder];
    expect(order).toHaveLength(QUEUE.length);

    // Somewhere down the queue, as after a few dozen songs.
    await act(async () => pb.playFromQueue(order[40], 40));
    await settle();
    expect(pb.itemID).toBe(order[40]);

    // Down to the mini-player, and back up: MiniPlayer's expand is exactly this.
    await act(async () => nav("/"));
    await settle();
    await act(async () => nav(`/watch/${pb.itemID}`));
    await settle();

    expect(pb.shuffle).toBe(true);
    expect(pb.playOrder).toEqual(order);
    expect(pb.playOrder.indexOf(pb.itemID)).toBe(40);
  });

  it("still gives Randomize all a fresh order when it is pressed again", async () => {
    await randomizeAll();
    const first = [...pb.playOrder];
    // Randomize all starts on a random track of its own (queueOrder.ts).
    await act(async () => nav(`/watch/${QUEUE[7]}`, { state: { queue: QUEUE, shuffle: true } }));
    await settle();
    // 60 items: the chance of the same order twice is nil.
    expect(pb.playOrder).not.toEqual(first);
  });
});
