/*
 * Pressing the second copy of a repeated song in a playlist (R16-6).
 *
 * A playlist may hold the same track at two positions (ADR 0030). The row's
 * Play carried only the id, so the player's cursor fell back to the first copy
 * and playback went on from there: everything between the two was heard again.
 * The row now carries its position through route state, and play() seeds the
 * cursor from it.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback } from "./PlaybackProvider";
import { TrackList } from "@/components/TrackList";
import type { Item } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

function track(id: number, title: string): Item {
  return {
    id,
    library_id: 1,
    kind: "track",
    title,
    year: null,
    series: null,
    season: null,
    episode: null,
    artist: null,
  } as unknown as Item;
}

// 7 at positions 0 and 2.
const tracks = [track(7, "Opener"), track(9, "Middle"), track(7, "Opener"), track(11, "Closer")];

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
let landed: { path: string; state: unknown } | null;

function Probe() {
  pb = usePlayback();
  return null;
}

function Landing() {
  const loc = useLocation();
  landed = { path: loc.pathname + loc.search, state: loc.state };
  return null;
}

beforeEach(() => {
  const proto = window.HTMLMediaElement.prototype;
  proto.load = vi.fn();
  proto.play = vi.fn(async () => {});
  proto.pause = vi.fn();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  landed = null;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      const item = url.match(/\/api\/items\/(\d+)(\?|$)/);
      if (item) return json({ id: Number(item[1]), title: "Song", kind: "track", duration_ms: 200_000 });
      if (url.includes("/playback")) return json({ decision: { method: "direct", reason: "" } });
      if (url.includes("/api/auth")) return json({ user: { role: "user" } });
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function render(children: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <FocusProvider>
            <PlaybackProvider>
              <Probe />
              {children}
            </PlaybackProvider>
          </FocusProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
}

describe("the second copy of a repeated song", () => {
  it("carries its position to the player when its row is pressed", async () => {
    await render(
      <Routes>
        <Route path="/" element={<TrackList tracks={tracks} playlistID={3} />} />
        <Route path="/watch/:id" element={<Landing />} />
      </Routes>,
    );
    const plays = [...host.querySelectorAll<HTMLButtonElement>("button")].filter((b) =>
      /^Play /.test(b.getAttribute("aria-label") ?? ""),
    );
    expect(plays).toHaveLength(4);
    await act(async () => plays[2].click());
    expect(landed?.path).toBe("/watch/7?queue=7,9,7,11");
    expect(landed?.state).toEqual({ at: 2 });
  });

  it("plays on from the copy that was pressed, not the first", async () => {
    await render(null);
    await act(async () => pb.play(7, [7, 9, 7, 11], false, 2));
    expect(pb.playPos).toBe(2);
    expect(pb.nextItemID).toBe(11);
  });

  it("starts from the first copy when no position is given", async () => {
    await render(null);
    await act(async () => pb.play(7, [7, 9, 7, 11]));
    expect(pb.playPos).toBe(0);
    expect(pb.nextItemID).toBe(9);
  });

  it("ignores a position that does not hold the id", async () => {
    await render(null);
    await act(async () => pb.play(7, [7, 9, 7, 11], false, 1));
    expect(pb.playPos).toBe(0);
  });
});
