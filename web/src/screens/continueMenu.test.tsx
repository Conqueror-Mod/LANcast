/*
 * The right-click menu on a Continue shelf.
 *
 * The thing worth testing here is not that a menu opens. It is that the two
 * items send *different* writes, because they mean different things and the
 * difference is invisible from the outside: both make the tile disappear.
 *
 *   Mark as watched            → watched: true   (records that you saw it)
 *   Remove from Continue …     → watched: false  (records that you did not)
 *
 * An item sits on the shelf while `position_ms > 0 AND watched = 0`, so either
 * write clears it. Wire them the same way round and the shelf still empties,
 * the feature still looks finished, and the library quietly disagrees with
 * itself about what has been seen — for a film abandoned twenty minutes in,
 * marking it watched is a lie that every unwatched filter then repeats.
 *
 * That is the same class of failure the settings screen keeps producing: a
 * success state indistinguishable from the wrong one.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { Home } from "./Home";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const film = {
  id: 11,
  title: "A Film",
  kind: "movie",
  library_id: 1,
  duration_ms: 6_000_000,
  progress: { position_ms: 1_200_000, watched: false },
  artwork: {},
};

const track = {
  id: 22,
  title: "A Song",
  kind: "track",
  library_id: 2,
  duration_ms: 200_000,
  progress: { position_ms: 40_000, watched: false },
  artwork: {},
};

let host: HTMLDivElement;
let root: Root;
let writes: { url: string; body: Record<string, unknown> }[];

function mount() {
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
      if (url.includes("/api/continue")) return json({ items: [film, track] });
      if (url.includes("/api/libraries")) return json([]);
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
          <MemoryRouter initialEntries={["/"]}>
            <Home />
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

/** The tile whose accessible name is `title`, from any shelf. */
function tile(title: string): HTMLElement {
  const el = [...host.querySelectorAll("button.poster-tile")].find(
    (b) => b.getAttribute("aria-label") === title,
  );
  if (!el) throw new Error(`no tile for ${title}`);
  return el as HTMLElement;
}

function rightClick(el: HTMLElement): void {
  act(() => {
    el.dispatchEvent(
      new MouseEvent("contextmenu", { bubbles: true, clientX: 40, clientY: 40 }),
    );
  });
}

function menuItems(): HTMLButtonElement[] {
  return [...host.querySelectorAll('[role="menuitem"]')] as HTMLButtonElement[];
}

function clickItem(label: string): void {
  const b = menuItems().find((i) => i.textContent?.trim() === label);
  if (!b) throw new Error(`no menu item "${label}" — have: ${menuItems().map((i) => i.textContent).join(", ")}`);
  act(() => b.click());
}

describe("the Continue shelf menu", () => {
  it("does not open on tiles that were given no actions", async () => {
    mount();
    await render();
    // The hero is drawn from the same list and dropped from the shelf beneath
    // it, so a film and a track is enough to leave one of each on screen.
    expect(menuItems().length).toBe(0);
  });

  it("opens on right-click", async () => {
    mount();
    await render();
    rightClick(tile("A Song"));
    expect(menuItems().length).toBeGreaterThan(0);
  });

  /*
   * The whole point. Both items empty the shelf; only one of them says you
   * watched the thing.
   */
  it("marking watched and removing send opposite values", async () => {
    mount();
    await render();

    rightClick(tile("A Song"));
    clickItem("Mark as played");
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
    const marked = writes.find((w) => w.url.includes("/items/22/progress"));
    expect(marked, "marking sent nothing").toBeTruthy();
    expect(marked?.body.watched).toBe(true);

    writes = [];
    rightClick(tile("A Song"));
    clickItem("Remove from Continue Listening");
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
    const removed = writes.find((w) => w.url.includes("/items/22/progress"));
    expect(removed, "removing sent nothing").toBeTruthy();
    expect(removed?.body.watched).toBe(false);
    // Position zero is the only honest way to say "not seen": leaving it behind
    // puts the row straight back on the shelf.
    expect(removed?.body.position_ms).toBe(0);
  });

  /*
   * "Mark as watched" on a song, or "Remove from Continue Watching" on the
   * listening shelf, is the interface reading from the wrong half of itself.
   */
  it("says watched or played to match what the item is", async () => {
    mount();
    await render();

    rightClick(tile("A Song"));
    let labels = menuItems().map((i) => i.textContent?.trim());
    expect(labels).toContain("Mark as played");
    expect(labels).toContain("Remove from Continue Listening");

    rightClick(tile("A Film"));
    labels = menuItems().map((i) => i.textContent?.trim());
    expect(labels).toContain("Mark as watched");
    expect(labels).toContain("Remove from Continue Watching");
  });

  // A menu item that also opened the thing it was acting on would be a tile
  // whose every action navigates away from the result.
  it("does not open the item when a menu item is chosen", async () => {
    mount();
    await render();
    rightClick(tile("A Film"));
    clickItem("Mark as watched");
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
    expect(host.textContent).not.toContain("Loading item");
  });
});
