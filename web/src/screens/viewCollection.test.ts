/*
 * "View collection" in a title's More Options: once for one collection, by
 * name for several, and never on a collection's own page.
 */
import { describe, it, expect, vi } from "vitest";
import { collectionsMenu } from "./Detail";
import type { Item } from "@/api/types";

const film = { id: 5, kind: "movie", title: "Saw V" } as Item;
const saw = { id: 40, kind: "collection", title: "Saw Collection" } as Item;
const horror = { id: 41, kind: "collection", title: "Horror Night" } as Item;

describe("View collection", () => {
  it("is one entry for one collection, and opens it", () => {
    const open = vi.fn();
    const menu = collectionsMenu(film, [saw], open);
    expect(menu.map((m) => m.label)).toEqual(["View collection"]);
    menu[0].onSelect();
    expect(open).toHaveBeenCalledWith(40);
  });

  it("names each collection when there are several", () => {
    expect(collectionsMenu(film, [horror, saw], vi.fn()).map((m) => m.label)).toEqual([
      "View Horror Night",
      "View Saw Collection",
    ]);
  });

  it("offers nothing with no collections, or on a collection itself", () => {
    expect(collectionsMenu(film, [], vi.fn())).toEqual([]);
    expect(collectionsMenu(film, undefined, vi.fn())).toEqual([]);
    expect(collectionsMenu(saw, [horror], vi.fn())).toEqual([]);
  });
});
