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
/** What /api/resume answers with. */
let resumeCards: unknown[] = [];

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
      if (url.includes("/api/resume")) return json({ cards: resumeCards, more: false });
      if (url.includes("/api/continue")) return json({ items: continuing });
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
  continuing = [];
  resumeCards = [];
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
  it("offers films not yet begun, shuffled", async () => {
    mount([library(1, "Movies", "movie")]);
    await render();
    const ask = gets.find((u) => u.includes("sort=random"));
    expect(ask, "no shuffled request for the Unwatched shelf").toBeTruthy();
    // Not begun, which leaves out a half-watched film (it is on Continue
    // Watching) and is the only question that means anything for a show.
    expect(ask).toContain("status=unstarted");
    expect(ask).toMatch(/seed=\d+/);
    expect(host.textContent).toContain("Never Seen");
  });

  // A show has no play state of its own, so watched=false would pass every
  // show; unstarted asks about its episodes.
  it("offers shows not yet begun from a TV library too", async () => {
    mount([library(1, "Movies", "movie"), library(3, "TV Shows", "show")]);
    await render();
    const asks = gets.filter((u) => u.includes("sort=random"));
    expect(asks.some((u) => u.includes("library_id=3") && u.includes("status=unstarted"))).toBe(true);
    expect(labels().some((l) => /Unwatched in TV Shows/i.test(l))).toBe(true);
    expect(labels().some((l) => /Unwatched in Movies/i.test(l))).toBe(true);
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

  // Film and show libraries only: music and photographs have no unwatched.
  it("asks nothing of a music library", async () => {
    mount([library(2, "Music", "music")]);
    await render();
    expect(gets.some((u) => u.includes("sort=random"))).toBe(false);
  });
});

/*
 * A show in the resume console (ADR 0118), which replaced the Next up shelf.
 *
 * The server answers with the show, carrying `next_episode`. The card must
 * draw that episode -- its still, its code -- not the show's poster, or a show
 * you finished an episode of looks exactly like a film you abandoned.
 */
describe("a show in the resume console", () => {
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

  const card = (title: string) =>
    [...host.querySelectorAll("button.resume-card")].find((b) =>
      b.getAttribute("aria-label")?.startsWith(title + ","),
    );

  it("draws the next episode, not the show's poster", async () => {
    resumeCards = [
      { kind: "video", touched_at: 2, item: film(9, "Abandoned Film", 60_000) },
      { kind: "show", touched_at: 1, item: show },
    ];
    mount([]);
    await render();

    expect(card("Abandoned Film")).toBeTruthy();
    const c = card("A Show");
    expect(c, "no card for the show").toBeTruthy();
    expect(c!.textContent).toContain("S01E02 · next");
    expect(c!.querySelector("img")?.getAttribute("src")).toContain("episode-still");
    expect(c!.querySelector(".resume-card__art--wide")).toBeTruthy();
  });

  /*
   * Home is one of the surfaces where a cover cannot be lifted (ADR 0051), and
   * the console is on Home: a sensitive card keeps its name, so it can be
   * found, and shows no picture and no place.
   */
  it("covers a sensitive card and offers no way to uncover it", async () => {
    resumeCards = [
      {
        kind: "video",
        touched_at: 1,
        item: { ...film(9, "Marked Film", 60_000), sensitive: true, artwork: { poster: "secret" } },
      },
    ];
    mount([]);
    await render();
    const c = [...host.querySelectorAll("button.resume-card")].find((b) =>
      b.getAttribute("aria-label")?.startsWith("Marked Film"),
    );
    expect(c, "no card").toBeTruthy();
    expect(c!.querySelector("img")).toBeNull();
    expect(c!.textContent).toContain("Sensitive");
    expect(c!.textContent).not.toContain("click to show");
  });

  // Nothing in progress is no panel at all, rather than a heading over nothing.
  it("draws no console when nothing is in progress", async () => {
    mount([]);
    await render();
    expect(host.querySelector("section.resume")).toBeNull();
  });
});
