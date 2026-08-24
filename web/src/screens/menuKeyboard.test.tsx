/*
 * Opening a context menu without a pointer.
 *
 * This is the gap the menus shipped with, and it was deferred four times while
 * the surfaces grew. It matters because bigscreen is the same client at ten
 * feet, driven by arrow keys for a remote that has no right button — so
 * anything living only behind right-click does not exist on a television, and
 * by v0.8.2 that included marking a track played and queueing anything at all.
 *
 * The reason it needs tests rather than a look is that nothing on screen shows
 * it is missing. A menu that cannot be opened by keyboard looks exactly like a
 * menu, right up until somebody tries.
 *
 * Three things have to hold together or the route is useless:
 *   - the key opens it,
 *   - focus moves into it, or the arrows keep walking the grid behind,
 *   - focus comes back on the way out, or the grid restarts from the top.
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
import { bindingKeys } from "@/lib/keys";

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
  item_count: 2,
  media_count: 2,
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

// A container, which now has its own menu -- and a photograph, which still has
// none, so the refusal this file was written to guard still has a subject.
const show = {
  id: 13,
  title: "A Show",
  kind: "show",
  library_id: 1,
  child_count: 3,
  artwork: {},
};

const photo = {
  id: 14,
  title: "A Photograph",
  kind: "photo",
  library_id: 1,
  artwork: {},
};

let host: HTMLDivElement;
let root: Root;

function mount(items: unknown[], role = "admin") {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role },
        });
      }
      if (url.includes("/api/items")) return json({ items, total: items.length });
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

async function render(items: unknown[], role = "admin") {
  mount(items, role);
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

/** Presses a key on the document, the way the focus controller listens. */
function press(key: string) {
  act(() => {
    document.dispatchEvent(
      new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true }),
    );
  });
}

function menuItems(): HTMLButtonElement[] {
  return [...host.querySelectorAll('[role="menuitem"]')] as HTMLButtonElement[];
}

/** The key the actions binding actually answers to, not a hardcoded guess. */
const actionsKey = bindingKeys("actions")[0];

describe("opening a menu without a pointer", () => {
  it("has a rebindable key rather than a hardcoded one", () => {
    // Kodi's context-menu key beside the dedicated one, because plenty of
    // laptops have no ContextMenu key at all.
    expect(bindingKeys("actions")).toContain("ContextMenu");
    expect(bindingKeys("actions")).toContain("c");
  });

  it("opens the focused tile's menu", async () => {
    await render([film]);
    act(() => tile("A Film").focus());
    expect(menuItems().length).toBe(0);

    press(actionsKey);
    expect(menuItems().length).toBeGreaterThan(0);
  });

  /*
   * Without this the arrows keep walking the grid underneath the open menu,
   * because the tiles are still registered focusables. On a television that is
   * the focus ring wandering off while the menu sits there.
   */
  it("moves focus into the menu", async () => {
    await render([film]);
    act(() => tile("A Film").focus());
    press(actionsKey);

    const first = menuItems()[0];
    expect(document.activeElement).toBe(first);
  });

  it("walks the items with the arrows", async () => {
    await render([film]);
    act(() => tile("A Film").focus());
    press(actionsKey);

    const items = menuItems();
    expect(document.activeElement).toBe(items[0]);

    act(() => {
      items[0].dispatchEvent(
        new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }),
      );
    });
    expect(document.activeElement).toBe(items[1]);

    act(() => {
      items[1].dispatchEvent(
        new KeyboardEvent("keydown", { key: "ArrowUp", bubbles: true }),
      );
    });
    expect(document.activeElement).toBe(items[0]);
  });

  // Escape already closes it; what matters is where focus lands, or a keyboard
  // is left with nothing and the grid has to be walked from the top again.
  it("gives focus back to the tile on the way out", async () => {
    await render([film]);
    const t = tile("A Film");
    act(() => t.focus());
    press(actionsKey);
    expect(document.activeElement).not.toBe(t);

    press("Escape");
    expect(menuItems().length).toBe(0);
    expect(document.activeElement).toBe(t);
  });

  /*
   * The same refusal the pointer route makes, on the one tile that still has
   * nothing to offer. A photograph is neither watched nor queued and has no
   * page worth visiting, so the key does nothing rather than opening an empty
   * box. A container used to be in this test and has a menu of its own now.
   */
  it("does nothing on an item with no actions", async () => {
    // As an ordinary viewer, for whom a photograph really has nothing: the one
    // thing an admin can do to one is remove it.
    await render([photo], "user");
    act(() => tile("A Photograph").focus());
    press(actionsKey);
    expect(menuItems().length).toBe(0);
  });

  // And the container it replaced does open one, keyboard route included --
  // the half that would otherwise quietly regress to the old refusal.
  it("opens a container's own menu from the key", async () => {
    await render([show]);
    act(() => tile("A Show").focus());
    press(actionsKey);
    expect(menuItems().map((i) => i.textContent?.trim())).toContain("Play all");
  });
});
