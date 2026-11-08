/*
 * Home draws what is in progress from /api/resume (ADR 0118), and never from
 * one mixed /api/continue list.
 *
 * One mixed list of 20 let half-played songs fill every slot: on the owner's
 * library 16 of the newest 20 rows were tracks, and Continue Watching showed
 * three films of the forty it could have. The server now merges a list per
 * kind and caps each kind (store.ResumeCandidates, api.resume, both tested in
 * Go). What is left for the client is not to undo that: ask the console, keep
 * every card it is given, and never fall back to an unscoped ask.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { Home } from "./Home";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const library = (id: number, name: string, kind: string) => ({
  id,
  name,
  kind,
  path: "/x",
  roots: [],
  created_at: 1,
  scanned_at: 1,
  item_count: 10,
  media_count: 10,
  shape_warning: null,
});

const film = (id: number, title: string, progress?: number) => ({
  id,
  title,
  kind: "movie",
  library_id: 1,
  artwork: {},
  ...(progress ? { progress: { position_ms: progress, watched: false } } : {}),
});

const track = (id: number) => ({
  id,
  title: `Song ${id}`,
  kind: "track",
  library_id: 2,
  artwork: {},
  progress: { position_ms: 30_000, watched: false },
});

let host: HTMLDivElement;
let root: Root;
let gets: string[];

function mount(libs: unknown[]) {
  gets = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if ((init?.method ?? "GET") !== "GET") return new Response(null, { status: 204 });
      gets.push(url);
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "dana", role: "admin" },
        });
      }
      if (url.includes("/api/libraries") && !url.includes("/trending")) return json(libs);
      if (url.includes("/api/continue?shelf=watching")) {
        return json({ items: [film(10, "Paused Film", 5000), film(11, "Other Film", 9000)] });
      }
      if (url.includes("/api/continue?shelf=music")) {
        return json({ items: Array.from({ length: 25 }, (_, i) => track(100 + i)) });
      }
      if (url.includes("/api/resume")) {
        return json({
          more: true,
          cards: [
            { kind: "video", touched_at: 3, item: film(10, "Paused Film", 5000) },
            { kind: "video", touched_at: 2, item: film(11, "Other Film", 9000) },
            {
              kind: "music",
              touched_at: 1,
              item: { id: 90, title: "An Album", kind: "album", library_id: 2, artwork: {} },
              track: { item: track(100), number: 1, of: 12 },
            },
          ],
        });
      }
      // An unscoped ask is what let the songs evict the films.
      if (url.includes("/api/continue")) {
        return json({ items: Array.from({ length: 20 }, (_, i) => track(100 + i)) });
      }
      if (url.includes("/api/items") && url.includes("sort=random")) {
        return json({
          items: [film(1, "Never Seen")],
          total: 2,
        });
      }
      return json({ items: [], total: 0 });
    }),
  );
}

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

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={["/"]}>
              <Home />
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

describe("home resume console", () => {
  it("draws every card the server merged, and never asks one mixed list", async () => {
    mount([library(1, "Movies", "movie"), library(2, "Music", "music")]);
    await render();
    expect(gets.some((u) => u.includes("/api/resume"))).toBe(true);
    const asks = gets.filter((u) => u.includes("/api/continue"));
    expect(asks.filter((u) => !u.includes("shelf="))).toEqual([]);
    // The console replaced Continue Listening; nothing asks for it any more.
    expect(asks.some((u) => u.includes("shelf=music"))).toBe(false);

    const panel = host.querySelector("section.resume");
    expect(panel, "no resume console").toBeTruthy();
    expect(panel!.textContent).toContain("Paused Film");
    expect(panel!.textContent).toContain("Other Film");
    expect(panel!.textContent).toContain("An Album");
    expect(panel!.textContent).toContain("Track 1 of 12");
    // The server held something back, so the console offers the rest.
    expect(panel!.querySelector('a[href="/continue"]')?.textContent).toBe("See all");
  });
});
