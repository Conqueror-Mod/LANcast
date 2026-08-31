/*
 * Recently Added, when one kind arrives in bulk.
 *
 * The shelves used to share a single window: one query for the newest 40 rows
 * of anything, split by kind after it arrived. That works until a library turns
 * up all at once. Importing 8,882 music tracks put **35 artists into the newest
 * 40 rows**, leaving three films and two collections — so the films shelf
 * showed only what had been added *since* the import, and for a while showed
 * nothing at all and disappeared. Reported as exactly that.
 *
 * The window was the bug, not the sort. Ordering by `added_at` across every
 * kind means whichever shelf just received a thousand rows silently evicts the
 * others, so the more recently you organised your library the emptier the home
 * page looks — the opposite of what it is for.
 *
 * These stub a server whose *unfiltered* recently-added is entirely music,
 * which is the state a music import leaves behind. A films shelf that survives
 * that is asking for its own kinds.
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

/** The film the shelf must keep showing. */
const film = {
  id: 11,
  title: "An Older Film",
  kind: "movie",
  library_id: 1,
  artwork: {},
};

/** What a bulk music import leaves at the top of `added_at`. */
const artists = Array.from({ length: 40 }, (_, i) => ({
  id: 100 + i,
  title: "An Artist " + i,
  kind: "artist",
  library_id: 2,
  artwork: {},
}));

let host: HTMLDivElement;
let root: Root;
let gets: string[];

function mount() {
  gets = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (method !== "GET") return new Response(null, { status: 204 });
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      if (url.includes("/api/libraries")) return json([]);
      if (url.includes("/api/continue")) return json({ items: [] });

      if (url.includes("/api/items")) {
        gets.push(url);
        /*
         * The server the report came from: sorted by added_at across every
         * kind, the newest rows are all music. Only a request that says which
         * kinds it wants can see past them.
         */
        if (url.includes("exclude_kind=") && url.includes("artist")) {
          return json({ items: [film], total: 1 });
        }
        if (url.includes("kind=artist")) {
          return json({ items: artists.slice(0, 20), total: 20 });
        }
        if (url.includes("kind=photo")) return json({ items: [], total: 0 });
        return json({ items: artists, total: artists.length });
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
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
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

describe("Recently Added after a bulk import", () => {
  /*
   * The reported fault. With a shared window this film is invisible — it is not
   * in the newest 40 rows, because 40 artists are.
   */
  it("still shows a film when the newest rows are all music", async () => {
    mount();
    await render();
    expect(host.textContent).toContain("An Older Film");
  });

  // And the shelf asks for its own kinds rather than filtering afterwards,
  // which is the difference between a window that can be emptied and one that
  // cannot.
  it("asks for a window that excludes the other shelves' kinds", async () => {
    mount();
    await render();
    const video = gets.find((u) => u.includes("exclude_kind="));
    expect(video, "no request excluded the music and picture kinds").toBeTruthy();
    for (const kind of ["artist", "album", "track", "gallery", "photo"]) {
      expect(video).toContain(kind);
    }
  });

  // Music keeps a window of its own, so the two cannot starve each other in
  // either direction.
  it("asks for music separately", async () => {
    mount();
    await render();
    expect(gets.some((u) => u.includes("kind=artist"))).toBe(true);
  });
});
