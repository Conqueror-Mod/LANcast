/*
 * The right-click menu on a library grid poster.
 *
 * The interesting half is what it refuses to offer. A grid holds shows,
 * seasons, albums and galleries beside films, and a menu that offered "Play" on
 * a folder or "Mark as watched" on a photograph would be guessing — so those
 * open no menu at all and keep the browser's own.
 *
 * That is easy to get wrong in a way nothing notices: returning an empty action
 * list still suppresses the native menu and still opens a box, just an empty
 * one. The tile has to decline *before* preventDefault, not after.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { LibraryView } from "./LibraryView";
import { configForKind } from "./libraryConfig";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const library = {
  id: 1,
  name: "Films",
  kind: "movie",
  path: "D:/Media/Films",
  created_at: 1,
  scanned_at: 2,
  item_count: 3,
  media_count: 3,
};

const film = {
  id: 11,
  title: "A Film",
  kind: "movie",
  library_id: 1,
  duration_ms: 6_000_000,
  progress: { position_ms: 0, watched: false },
  artwork: {},
};

const seenFilm = {
  id: 12,
  title: "A Seen Film",
  kind: "movie",
  library_id: 1,
  duration_ms: 6_000_000,
  progress: { position_ms: 0, watched: true },
  artwork: {},
};

// A container: clicking opens it, and its menu is about the whole of it.
const show = {
  id: 13,
  title: "A Show",
  kind: "show",
  library_id: 1,
  child_count: 3,
  artwork: {},
};

// The one tile left with nothing to offer an ordinary viewer.
const photo = {
  id: 14,
  title: "A Photograph",
  kind: "photo",
  library_id: 1,
  artwork: {},
};

let host: HTMLDivElement;
let root: Root;
let writes: { url: string; body: Record<string, unknown> }[];

function mount(items: unknown[], role = "admin") {
  writes = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (method !== "GET") {
        writes.push({
          url,
          body: init?.body ? JSON.parse(init.body as string) : {},
        });
        return new Response(null, { status: 204 });
      }
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role },
        });
      }
      if (url.includes("/api/items")) return json({ items, total: items.length });
      if (url.includes("/api/facets")) return json({});
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
            <MemoryRouter initialEntries={["/library/1"]}>
              <LibraryView
                library={library as never}
                config={configForKind("movie")}
              />
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

function tile(title: string): HTMLElement {
  const el = [...host.querySelectorAll("button.poster-tile")].find(
    (b) => b.getAttribute("aria-label") === title,
  );
  if (!el) throw new Error(`no tile for ${title}`);
  return el as HTMLElement;
}

/** Returns whether the tile suppressed the browser's own menu. */
function rightClick(el: HTMLElement): boolean {
  const ev = new MouseEvent("contextmenu", {
    bubbles: true,
    cancelable: true,
    clientX: 40,
    clientY: 40,
  });
  act(() => {
    el.dispatchEvent(ev);
  });
  return ev.defaultPrevented;
}

function menuItems(): HTMLButtonElement[] {
  return [...host.querySelectorAll('[role="menuitem"]')] as HTMLButtonElement[];
}

function labels(): (string | undefined)[] {
  return menuItems().map((i) => i.textContent?.trim());
}

describe("the library grid menu", () => {
  it("offers play, watched and details on a film", async () => {
    mount([film]);
    await render();
    expect(rightClick(tile("A Film"))).toBe(true);
    // Pinned in order: Play first because it is what the tile is for, the
    // watched toggle next, then the two queue actions, then details last.
    expect(labels()).toEqual([
      "Play",
      "Mark as watched",
      "Play next",
      "Add to queue",
      "Go to details",
      // Last, and only for an admin. A destructive item anywhere but the end
      // of the list is one a slipped press can reach on the way to something
      // ordinary.
      "Remove from library…",
    ]);
  });

  // The state is a toggle, and a menu that always says "Mark as watched" is a
  // menu that cannot undo itself.
  it("offers to undo on something already watched", async () => {
    mount([seenFilm]);
    await render();
    rightClick(tile("A Seen Film"));
    expect(labels()).toContain("Mark as unwatched");
    expect(labels()).not.toContain("Mark as watched");
  });

  /*
   * A show used to open no menu at all, which meant the most common tile in a
   * television library answered a right-click with the browser's own. It has
   * its own list now -- and pointedly not the film's: a show is not queued, it
   * is gathered into a queue.
   */
  it("gives a container its own menu", async () => {
    mount([show]);
    await render();
    expect(rightClick(tile("A Show")), "left the native menu alone").toBe(true);
    expect(labels()).toEqual([
      "Play all",
      "Shuffle",
      "Mark all as watched",
      "Mark all as unwatched",
      "Go to details",
      "Remove from library…",
    ]);
  });

  /*
   * The refusal itself still has to hold, or PosterTile's guard rots silently:
   * an empty action list still calls preventDefault unless the tile declines
   * first. A photograph seen by an ordinary viewer is the case that is still
   * empty -- neither watched nor queued, and no page worth visiting.
   */
  it("leaves a tile with nothing to offer alone entirely", async () => {
    mount([photo], "user");
    await render();
    expect(
      rightClick(tile("A Photograph")),
      "suppressed the native menu",
    ).toBe(false);
    expect(menuItems().length).toBe(0);
  });

  it("marks watched through the progress endpoint", async () => {
    mount([film]);
    await render();
    rightClick(tile("A Film"));
    const mark = menuItems().find(
      (i) => i.textContent?.trim() === "Mark as watched",
    )!;
    act(() => mark.click());
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
    const w = writes.find((x) => x.url.includes("/items/11/progress"));
    expect(w, "nothing was written").toBeTruthy();
    expect(w?.body.watched).toBe(true);
  });

  /*
   * Removing a title deletes files from disk when the server allows it, so it
   * is behind the same admin gate the track row uses -- and the gate is the
   * assertion, because a permission that is only a hidden button is not one.
   */
  it("does not offer to remove a title to an ordinary viewer", async () => {
    mount([film], "user");
    await render();
    rightClick(tile("A Film"));
    expect(labels().join(" ")).not.toContain("Remove");
  });
});
