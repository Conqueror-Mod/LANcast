/*
 * Mark as watched, on the detail page.
 *
 * It existed only in a tile's right-click menu. The page now carries it: a
 * title toggles its own flag, and a show or season marks every episode and
 * reads finished from the count the server sends, so a finished one offers the
 * undo rather than offering to mark it again.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { Detail } from "./Detail";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const film = (watched: boolean) => ({
  id: 700, title: "A Film", kind: "movie", library_id: 1, duration_ms: 6_000_000,
  container: "matroska", artwork: {}, progress: { position_ms: 0, watched },
});
const show = (left: number) => ({
  id: 900, title: "A Show", kind: "show", library_id: 1, child_count: 1, artwork: {},
  unwatched_episodes: left,
});
const track = {
  id: 800, title: "A Song", kind: "track", library_id: 1, duration_ms: 200_000, artwork: {},
};

let host: HTMLDivElement;
let root: Root;
let puts: { url: string; body: unknown }[];

function stub(item: Record<string, unknown>) {
  puts = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
      if (init?.method === "PUT") {
        puts.push({ url, body: JSON.parse(String(init.body)) });
        return new Response(null, { status: 204 });
      }
      if (url.includes("/continue")) return json({});
      if (url.includes("/episodes")) return json({ episodes: [{ id: 101 }, { id: 102 }] });
      if (/\/api\/items\/\d+$/.test(url.split("?")[0])) return json(item);
      if (url.includes("/api/auth/status")) {
        return json({ authenticated: true, configured: true, user: { id: "u1", role: "admin" } });
      }
      return json({ items: [], total: 0 });
    }),
  );
}

async function settle() {
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

async function render(id: number) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={[`/item/${id}`]}>
              <Routes>
                <Route path="/item/:id" element={<Detail />} />
              </Routes>
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

const labels = () => [...host.querySelectorAll("button")].map((b) => (b.textContent ?? "").trim());
const button = (label: string) =>
  [...host.querySelectorAll("button")].find((b) => (b.textContent ?? "").trim() === label) as
    | HTMLButtonElement
    | undefined;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("the detail page's watched button", () => {
  it("marks an unwatched film watched", async () => {
    stub(film(false));
    await render(700);
    const b = button("Mark as watched");
    expect(b).toBeDefined();
    await act(async () => b!.click());
    await settle();
    expect(puts).toEqual([
      { url: "/api/items/700/progress", body: { position_ms: 0, watched: true } },
    ]);
  });

  it("offers the undo on a film already watched", async () => {
    stub(film(true));
    await render(700);
    expect(labels()).toContain("Mark as unwatched");
    expect(labels()).not.toContain("Mark as watched");
  });

  it("marks every episode of a show with some left", async () => {
    stub(show(2));
    await render(900);
    const b = button("Mark all as watched");
    expect(b).toBeDefined();
    await act(async () => b!.click());
    await settle();
    expect(puts.map((p) => p.url).sort()).toEqual([
      "/api/items/101/progress",
      "/api/items/102/progress",
    ]);
  });

  it("offers to unmark a show with nothing left", async () => {
    stub(show(0));
    await render(900);
    expect(labels()).toContain("Mark all as unwatched");
    expect(labels()).not.toContain("Mark all as watched");
  });

  it("leaves music alone", async () => {
    stub(track);
    await render(800);
    expect(labels().some((l) => l.startsWith("Mark"))).toBe(false);
  });
});
