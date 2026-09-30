/*
 * The home page's shelves after the redundancy cleanup.
 *
 * Two kinds of shelf only repeated the page: each library's first twenty titles
 * alphabetically (the masthead's library buttons already go there) and
 * "Recently Played in …" (Continue Watching again, on a one-person server).
 * Both went, and an Unwatched shelf per film library took their place.
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

/** What /api/continue answers with; empty unless a test says otherwise. */
let continuing: unknown[] = [];

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
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      if (url.includes("/api/libraries") && !url.includes("/trending")) return json(libs);
      if (url.includes("/api/continue")) return json({ items: continuing });
      if (url.includes("/api/items") && url.includes("sort=random")) {
        return json({
          items: [film(1, "Never Seen"), film(2, "Half Watched", 600_000)],
          total: 2,
        });
      }
      return json({ items: [], total: 0 });
    }),
  );
}

beforeEach(() => {
  continuing = [];
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

const labels = () =>
  [...host.querySelectorAll(".section-label")].map((e) => e.textContent ?? "");

describe("home shelves", () => {
  it("offers unwatched films, shuffled, without the half-watched ones", async () => {
    mount([library(1, "Movies", "movie")]);
    await render();
    const ask = gets.find((u) => u.includes("sort=random"));
    expect(ask, "no shuffled request for the Unwatched shelf").toBeTruthy();
    expect(ask).toContain("watched=false");
    expect(ask).toMatch(/seed=\d+/);
    expect(host.textContent).toContain("Never Seen");
    // It is on Continue Watching; the server counts it as unwatched.
    expect(host.textContent).not.toContain("Half Watched");
  });

  it("no longer repeats each library's alphabetical shelf or its play history", async () => {
    mount([library(1, "Movies", "movie"), library(2, "Music", "music")]);
    await render();
    // A library's own shelf was headed by the library's name.
    const names = labels().map((l) => l.trim());
    expect(names).not.toContain("Movies");
    expect(names).not.toContain("Music");
    expect(gets.some((u) => u.includes("/trending"))).toBe(false);
    expect(labels().some((l) => /Recently Played in|Trending in/i.test(l))).toBe(false);
  });

  it("names the library only when there are two to tell apart", async () => {
    mount([library(1, "Movies", "movie")]);
    await render();
    expect(labels().some((l) => /^Unwatched$/i.test(l.trim()))).toBe(true);

    act(() => root.unmount());
    root = createRoot(host);
    mount([library(1, "Movies", "movie"), library(4, "Kids", "movie")]);
    await render();
    expect(labels().some((l) => /Unwatched in Kids/i.test(l))).toBe(true);
  });

  // Only film libraries: music and photographs have no unwatched to offer.
  it("asks nothing of a music library", async () => {
    mount([library(2, "Music", "music")]);
    await render();
    expect(gets.some((u) => u.includes("sort=random"))).toBe(false);
  });
});

/*
 * Next up: the shows move off Continue Watching and are drawn as the episode.
 *
 * The server answers Continue Watching with a show for a series in progress,
 * carrying `next_episode`. Mixed with half-watched films that was a poster
 * indistinguishable from a film you abandoned.
 */
describe("Next up", () => {
  const show = {
    id: 50,
    title: "A Show",
    kind: "show",
    library_id: 3,
    artwork: { poster: "show-poster" },
    next_episode: {
      id: 51,
      title: "The Second One",
      kind: "episode",
      library_id: 3,
      series: "A Show",
      season: 1,
      episode: 2,
      artwork: { thumb: "episode-still" },
    },
  };

  const shelf = (label: string) =>
    [...host.querySelectorAll(".shelf")].find(
      (s) => s.querySelector(".section-label")?.textContent?.trim() === label,
    );

  it("takes shows off Continue Watching and draws their next episode", async () => {
    continuing = [film(9, "Abandoned Film", 60_000), show];
    mount([]);
    await render();

    const cw = shelf("Continue Watching");
    expect(cw?.textContent).toContain("Abandoned Film");
    expect(cw?.textContent).not.toContain("A Show");

    const next = shelf("Next up");
    expect(next, "no Next up shelf").toBeTruthy();
    expect(next?.textContent).toContain("The Second One");
    expect(next?.textContent).toContain("A Show · S01E02");
    // The episode's still, not the show's poster.
    const img = next?.querySelector("img");
    expect(img?.getAttribute("src")).toContain("episode-still");
    expect(next?.querySelector(".poster-tile__art--wide")).toBeTruthy();
  });

  // Nothing to play means nothing to offer, and it does not fall back into
  // Continue Watching as the poster the split removed.
  it("offers no show that has no next episode", async () => {
    continuing = [{ ...show, next_episode: undefined }];
    mount([]);
    await render();
    expect(shelf("Next up")).toBeUndefined();
    expect(shelf("Continue Watching")).toBeUndefined();
  });
});
