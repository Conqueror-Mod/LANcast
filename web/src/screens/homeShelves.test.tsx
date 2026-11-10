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
import { MemoryRouter } from "react-router";
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
/** The signed-in account's role. */
let role = "admin";
/** What /api/profile/ratings/pending answers with. */
let toRate: unknown[] = [];
/** What /api/watchlist answers with. */
let watchlist: unknown[] = [];
/** What the comics window of New this week answers with. */
let newComics: unknown[] = [];
/** The built-in shelves the account turned off (/api/profile/home). */
let hiddenShelves: string[] = [];

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
          user: { id: "u1", name: "dana", role },
        });
      }
      if (url.includes("/api/libraries") && !url.includes("/trending")) return json(libs);
      if (url.includes("/api/resume")) return json({ cards: resumeCards, more: false });
      if (url.includes("/api/continue")) return json({ items: continuing });
      if (url.includes("/api/profile/home")) {
        return json({
          account: true, spotlight: null, spotlight_item: null, spoilers: null,
          hidden_shelves: hiddenShelves,
        });
      }
      if (url.includes("/api/profile/ratings/pending")) return json({ items: toRate });
      if (url.includes("/api/watchlist")) return json({ items: watchlist, total: watchlist.length });
      if (url.includes("kind=comic")) return json({ items: newComics, total: newComics.length });
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
  role = "admin";
  toRate = [];
  watchlist = [];
  newComics = [];
  hiddenShelves = [];
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

/*
 * No state of the first screen is an empty frame (ADR 0118 §3).
 */
describe("the first screen with nothing to resume", () => {
  it("lists the libraries in the console, as places to start", async () => {
    mount([library(1, "Movies", "movie"), library(2, "Music", "music")]);
    await render();
    const panel = host.querySelector("section.resume");
    expect(panel?.textContent).toContain("Your libraries");
    const links = [...(panel?.querySelectorAll("a") ?? [])].map((a) => a.getAttribute("href"));
    expect(links).toEqual(["/library/1", "/library/2"]);
    // And only there: the masthead no longer carries them.
    expect(host.querySelector(".masthead .masthead__libs")).toBeNull();
  });

  it("welcomes a new install with the one thing to do", async () => {
    mount([]);
    await render();
    const welcome = host.querySelector(".home__welcome");
    expect(welcome?.textContent).toContain("Add your first library");
    expect(welcome?.querySelector("a")?.getAttribute("href")).toBe("/settings?pane=libraries");
    expect(host.querySelector("section.resume")).toBeNull();
  });

  it("offers somebody who cannot add a library no button that would fail", async () => {
    role = "member";
    mount([]);
    await render();
    const welcome = host.querySelector(".home__welcome");
    expect(welcome?.textContent).toContain("Nothing here yet");
    expect(welcome?.querySelector("a")).toBeNull();
  });
});

/*
 * The order beneath the spotlight. New this week leads when the week brought
 * anything; a quiet week draws no section, so Rate what you've watched leads
 * instead. Nothing else changes place.
 */
describe("the first shelf under the spotlight", () => {
  const comic = (id: number, title: string, addedAt: number) => ({
    id,
    title,
    kind: "comic",
    library_id: 2,
    artwork: {},
    added_at: addedAt,
  });

  /** Shelf headings in page order, New this week's section heading included. */
  const headings = () =>
    [...host.querySelectorAll(".home__shelves .new-week__title, .home__shelves .section-label")]
      .filter((e) => !e.closest(".new-week") || e.classList.contains("new-week__title"))
      .map((e) => e.textContent ?? "");

  beforeEach(() => {
    toRate = [film(20, "Finished Lately")];
    watchlist = [film(30, "Meant To Watch")];
  });

  it("is New this week when the week brought something", async () => {
    newComics = [comic(40, "Fresh Issue", Math.floor(Date.now() / 1000) - 3600)];
    mount([library(1, "Movies", "movie")]);
    await render();
    expect(headings()).toEqual([
      "New this week",
      "Rate what you've watched",
      "Watchlist",
      "Unwatched",
    ]);
  });

  it("is Rate what you've watched when the week was quiet", async () => {
    // Added a month ago: inside the comics window, outside the week.
    newComics = [comic(40, "Old Issue", Math.floor(Date.now() / 1000) - 30 * 86400)];
    mount([library(1, "Movies", "movie")]);
    await render();
    expect(headings()).toEqual(["Rate what you've watched", "Watchlist", "Unwatched"]);
  });
});

/*
 * Shelves turned off in Your account › Home (settings-3-plan step 3). A hidden
 * shelf is not drawn and, where it has a list of its own, not asked for.
 */
describe("shelves the account turned off", () => {
  beforeEach(() => {
    toRate = [film(20, "Finished Lately")];
    watchlist = [film(30, "Meant To Watch")];
  });

  const shelfLabels = () =>
    [...host.querySelectorAll(".home__shelves .section-label")].map((e) => e.textContent ?? "");

  it("draws only the ones left on, and asks nothing for the rest", async () => {
    hiddenShelves = ["watchlist", "unwatched:2"];
    mount([library(1, "Movies", "movie"), library(2, "TV Shows", "show")]);
    await render();
    const shown = shelfLabels();
    expect(shown).toContain("Rate what you've watched");
    expect(shown).not.toContain("Watchlist");
    expect(shown.some((l) => /Unwatched in Movies/.test(l))).toBe(true);
    expect(shown.some((l) => /Unwatched in TV Shows/.test(l))).toBe(false);
    expect(gets.some((u) => u.includes("/api/watchlist"))).toBe(false);
  });

  it("draws everything when nothing is turned off", async () => {
    mount([library(1, "Movies", "movie")]);
    await render();
    expect(shelfLabels()).toEqual(expect.arrayContaining(["Rate what you've watched", "Watchlist"]));
  });
});

/*
 * A right-click on any title on Home opens the same menu a poster opens in a
 * library grid. Every shelf here used to pass no actions, so a tile left the
 * browser's own menu, and the spotlight had none unless its title was in
 * progress or in a collection.
 */
describe("a right-click on a title on Home", () => {
  beforeEach(() => {
    toRate = [film(20, "Finished Lately")];
    watchlist = [film(30, "Meant To Watch")];
  });

  async function menuFor(title: string): Promise<string[]> {
    const el = [...host.querySelectorAll(".home__shelves *")].find(
      (e) => e.children.length === 0 && e.textContent === title,
    );
    expect(el, `no tile titled ${title}`).toBeTruthy();
    await act(async () => {
      el!.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 5, clientY: 5 }));
    });
    const menu = document.body.querySelector('[role="menu"]');
    const items = [...(menu?.querySelectorAll('[role="menuitem"]') ?? [])].map((e) => e.textContent ?? "");
    await act(async () => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    });
    return items;
  }

  it("opens the poster's menu on every shelf", async () => {
    mount([library(1, "Movies", "movie")]);
    await render();
    for (const title of ["Finished Lately", "Meant To Watch", "Never Seen"]) {
      const items = await menuFor(title);
      expect(items, title).toContain("Go to details");
    }
  });

  it("offers the watchlist entry while the Watchlist shelf is on", async () => {
    mount([library(1, "Movies", "movie")]);
    await render();
    expect(await menuFor("Meant To Watch")).toContain("Remove from Watchlist");
  });
});
