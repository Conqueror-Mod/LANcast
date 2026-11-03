/*
 * Artists, Albums or Tracks: the levels a music library is shown at (ADR 0114).
 *
 * Reported as a music library with no genre or year filter. The year was on
 * every album and track and the grid showed artists, which have none, so the
 * filter hid itself. What is checked here is the wire: the level decides the
 * kind the grid lists and the kind the facets describe, and they agree.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, useLocation } from "react-router-dom";
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
let location = "";

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

function Where() {
  const l = useLocation();
  location = l.pathname + l.search;
  return null;
}

async function settle() {
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

async function render(kind: string, at: string) {
  const library = { id: 9, name: "Music", kind, path: "D:/x", created_at: 1, scanned_at: 2, item_count: 1, media_count: 1 };
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={[at]}>
              <LibraryView library={library as never} config={configForKind(kind as never)} />
              <Where />
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

const levelButtons = () => [...host.querySelectorAll<HTMLButtonElement>('[aria-label="Show"] [role="radio"]')];
const level = (name: string) => levelButtons().find((b) => b.textContent === name)!;
const sortOptions = () => [...host.querySelectorAll("select option")].map((o) => o.textContent);
// The grid's own listing: the one scoped to this library.
const items = () => asked.filter((u) => u.includes("/api/items?") && u.includes("library_id=9"));
const facets = () => asked.filter((u) => u.includes("/facets"));

describe("a music library's levels", () => {
  it("opens on Artists, listing and describing the top level as before", async () => {
    await render("music", "/library/9");
    expect(levelButtons().map((b) => b.textContent)).toEqual(["Artists", "Albums", "Tracks"]);
    expect(level("Artists").getAttribute("aria-checked")).toBe("true");
    expect(items().every((u) => !/[?&]kind=/.test(u))).toBe(true);
    expect(facets().every((u) => !u.includes("kind="))).toBe(true);
    expect(sortOptions()).toEqual(["Title", "Recently added"]);
  });

  it("lists and describes albums on Albums, with Year back", async () => {
    await render("music", "/library/9?show=albums");
    expect(level("Albums").getAttribute("aria-checked")).toBe("true");
    expect(items().some((u) => /[?&]kind=album/.test(u))).toBe(true);
    expect(facets().some((u) => u.endsWith("/facets?kind=album"))).toBe(true);
    expect(sortOptions()).toContain("Year");
    expect(host.querySelector<HTMLInputElement>(".browse__search")!.placeholder).toBe("Search albums");
  });

  it("lists tracks on Tracks, with the duration sorts", async () => {
    await render("music", "/library/9?show=tracks&sort=longest");
    expect(items().some((u) => /[?&]kind=track/.test(u) && u.includes("sort=longest"))).toBe(true);
    expect(facets().some((u) => u.endsWith("/facets?kind=track"))).toBe(true);
  });

  it("keeps the genre across a switch and drops what the new level does not have", async () => {
    await render("music", "/library/9?show=tracks&genre=Jazz&year=1994&initial=T&sort=longest");
    await act(async () => level("Artists").click());
    await settle();
    const p = new URLSearchParams(location.split("?")[1] ?? "");
    expect(p.getAll("genre")).toEqual(["Jazz"]);
    expect(p.has("year")).toBe(false);
    expect(p.has("initial")).toBe(false);
    expect(p.has("sort")).toBe(false);
    expect(p.has("show")).toBe(false);
    expect(level("Artists").getAttribute("aria-checked")).toBe("true");
  });

  it("treats a sort the level does not offer as its default", async () => {
    await render("music", "/library/9?sort=longest");
    expect(items().every((u) => !u.includes("sort=longest"))).toBe(true);
  });

  it("is not offered on any other library", async () => {
    for (const kind of ["movie", "show", "picture", "retro"]) {
      await render(kind, "/library/9");
      expect(levelButtons()).toHaveLength(0);
    }
  });
});
