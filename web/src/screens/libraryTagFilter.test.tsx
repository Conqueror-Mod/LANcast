/*
 * The Tag and Favourites filters reach the server.
 *
 * Reported as a grid that did not narrow: choosing a tag or Favourites drew a
 * pill and put `?tag=` / `?favourite=1` in the address, and the items request
 * carried neither, so every film stayed on screen. The server has filtered on
 * both since they were added. What is checked here is the wire.
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

let host: HTMLDivElement;
let root: Root;
let asked: string[];

beforeEach(() => {
  asked = [];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      asked.push(url);
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.includes("/api/auth/status")) {
        return json({ authenticated: true, configured: true, user: { id: "u1", name: "dana", role: "admin" } });
      }
      if (url.includes("/api/items")) return json({ items: [], total: 0 });
      return json({});
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

async function render(at: string) {
  const library = { id: 9, name: "Films", kind: "movie", path: "D:/x", created_at: 1, scanned_at: 2, item_count: 1, media_count: 1 };
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={[at]}>
              <LibraryView library={library as never} config={configForKind("movie")} />
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

// The grid's own listing: scoped to this library, with collections left out.
// The library also asks for its unrated items, which is not the grid.
const items = () =>
  asked
    .filter((u) => u.includes("/api/items?") && u.includes("library_id=9") && u.includes("exclude_kind="))
    .map((u) => new URLSearchParams(u.split("?")[1]));

describe("the grid's Tag and Favourites filters", () => {
  it("sends every chosen tag and favourite=1 when the address carries them", async () => {
    await render("/library/9?tag=4&tag=11&favourite=1");
    const sent = items();
    expect(sent.length).toBeGreaterThan(0);
    for (const p of sent) {
      expect(p.getAll("tag")).toEqual(["4", "11"]);
      expect(p.get("favourite")).toBe("1");
    }
  });

  it("sends neither when the address has neither", async () => {
    await render("/library/9");
    const sent = items();
    expect(sent.length).toBeGreaterThan(0);
    for (const p of sent) {
      expect(p.has("tag")).toBe(false);
      expect(p.has("favourite")).toBe(false);
    }
  });
});
