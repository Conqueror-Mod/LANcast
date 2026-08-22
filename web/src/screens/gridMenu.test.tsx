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

// A container: clicking opens it, and there is nothing here a menu can offer.
const show = {
  id: 13,
  title: "A Show",
  kind: "show",
  library_id: 1,
  child_count: 3,
  artwork: {},
};

let host: HTMLDivElement;
let root: Root;
let writes: { url: string; body: Record<string, unknown> }[];

function mount(items: unknown[]) {
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
          user: { id: "u1", name: "chris", role: "admin" },
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
          <MemoryRouter initialEntries={["/library/1"]}>
            <LibraryView
              library={library as never}
              config={configForKind("movie")}
            />
          </MemoryRouter>
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
    expect(labels()).toEqual(["Play", "Mark as watched", "Go to details"]);
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
   * A container opens no menu *and* does not swallow the browser's own. The
   * second half is the part that would rot silently: an empty action list still
   * calls preventDefault unless the tile declines first.
   */
  it("leaves a container alone entirely", async () => {
    mount([show]);
    await render();
    expect(rightClick(tile("A Show")), "suppressed the native menu").toBe(false);
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

  // Removing a title deletes files from disk when the server allows it. One
  // right-click away is not the right weight for that; the detail page keeps it.
  it("does not offer to remove a title", async () => {
    mount([film]);
    await render();
    rightClick(tile("A Film"));
    expect(labels().join(" ")).not.toContain("Remove");
  });
});
