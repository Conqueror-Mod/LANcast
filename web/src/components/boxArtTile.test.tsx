/*
 * A game's box art is shown whole (ADR 0073).
 *
 * Reported as thumbnails "very off-center and scaled improperly". A game tile
 * was framed as a 2:3 film poster with object-fit: cover, and box art is
 * almost never 2:3 — an American SNES or N64 box is wide — so every wide box
 * was zoomed onto its middle third. A game now gets a square tile, the box
 * contained inside it, over a blurred wash of itself.
 *
 * jsdom performs no layout and cannot see a crop, so this holds the wiring
 * the fix rests on: which frame a game gets, and that its image is the
 * contained one. Whether it looks right is still a thing to look at.
 */
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { FocusProvider } from "@/focus/FocusController";
import { PosterTile } from "./PosterTile";
import type { Item } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

function item(over: Partial<Item>): Item {
  return {
    id: 9,
    library_id: 1,
    kind: "rom",
    platform: "snes",
    title: "Bubsy II",
    parent_id: null,
    artwork: { poster: "boxart123" },
    ...over,
  } as Item;
}

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

function render(it: Item) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  act(() => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter>
            <PosterTile item={it} />
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
}

describe("box art", () => {
  it("frames a game square and shows its box whole, over a wash of itself", () => {
    render(item({}));
    const art = host.querySelector(".poster-tile__art")!;
    expect(art.classList.contains("poster-tile__art--square")).toBe(true);
    const box = art.querySelector("img.poster-tile__box");
    const wash = art.querySelector("img.poster-tile__box-wash");
    expect(box).not.toBeNull();
    expect(wash).not.toBeNull();
    expect(box!.getAttribute("src")).toBe(wash!.getAttribute("src"));
    // The wash is decoration; a screen reader hears the tile, not two images.
    expect(wash!.getAttribute("aria-hidden")).toBe("true");
  });

  it("leaves a film's poster as it was", () => {
    render(item({ kind: "movie", platform: undefined, title: "1408" }));
    const art = host.querySelector(".poster-tile__art")!;
    expect(art.classList.contains("poster-tile__art--square")).toBe(false);
    expect(art.querySelector(".poster-tile__box")).toBeNull();
    expect(art.querySelectorAll("img")).toHaveLength(1);
  });

  // A book is square like a record, and an abridged one says so where its
  // unabridged twin says only its part count (ADR 0116).
  it("frames a book square and marks it abridged", () => {
    render(item({ kind: "book", platform: undefined, title: "Emissary", child_count: 4, edition: "Abridged" }));
    const art = host.querySelector(".poster-tile__art")!;
    expect(art.classList.contains("poster-tile__art--square")).toBe(true);
    expect(host.querySelector(".poster-tile__year")?.textContent).toBe("Abridged · 4 parts");
    render(item({ kind: "book", platform: undefined, title: "Emissary", child_count: 4 }));
    expect(host.querySelector(".poster-tile__year")?.textContent).toBe("4 parts");
  });

  it("still names a game with no box art", () => {
    render(item({ artwork: {} }));
    expect(host.querySelector(".poster-tile__placeholder")?.textContent).toContain("Bubsy II");
  });
});
