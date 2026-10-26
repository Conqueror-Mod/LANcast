/*
 * The line under a collection's title. A collection has no year, runtime or
 * certificate of its own, so the line said only "5 films".
 */
import { describe, it, expect } from "vitest";
import { collectionSummary, missingFilms } from "./collectionSummary";
import type { CollectionFilm, Item } from "@/api/types";

const coll = (p?: { total: number; watched: number; in_progress: number }) =>
  ({ id: 50, library_id: 1, kind: "collection", title: "Saga", collection_progress: p }) as Item;
const film = (id: number, year: number, mins: number) =>
  ({ id, library_id: 1, kind: "movie", title: `F${id}`, year, duration_ms: mins * 60_000 }) as Item;
const listed = (id: string, over: Partial<CollectionFilm> = {}): CollectionFilm => ({
  external_id: id, title: `T${id}`, ...over,
});

describe("collectionSummary", () => {
  it("states the span, the total running time and how far through it you are", () => {
    const s = collectionSummary(
      coll({ total: 3, watched: 1, in_progress: 0 }),
      [film(1, 2001, 178), film(2, 2003, 201), film(3, 2002, 179)],
      undefined,
    );
    expect(s.years).toBe("2001–2003");
    expect(s.runtime).toBe("9h 18m");
    expect(s.watched).toBe("1 of 3 watched");
    expect(s.owned).toBe("");
  });

  it("says one year, All watched, and nothing for an untouched box set", () => {
    expect(collectionSummary(coll(), [film(1, 1999, 90)], undefined).years).toBe("1999");
    expect(collectionSummary(coll({ total: 2, watched: 2, in_progress: 0 }), [], undefined).watched).toBe("All watched");
    expect(collectionSummary(coll({ total: 2, watched: 0, in_progress: 1 }), [], undefined).watched).toBe("Started");
    expect(collectionSummary(coll({ total: 2, watched: 0, in_progress: 0 }), [], undefined).watched).toBe("");
  });

  it("counts what you have of the released films only", () => {
    const films = [
      listed("1", { item_id: 1 }),
      listed("2"),
      listed("3", { item_id: 3 }),
      listed("4", { upcoming: true }),
    ];
    expect(collectionSummary(coll(), [], films).owned).toBe("You have 2 of 3");
    // Everything released is here: nothing to say, even with a sequel announced.
    expect(collectionSummary(coll(), [], [listed("1", { item_id: 1 }), listed("4", { upcoming: true })]).owned).toBe("");
  });
});

it("missingFilms keeps the films without a library copy, in order", () => {
  const out = missingFilms([listed("1", { item_id: 1 }), listed("2"), listed("3", { upcoming: true })]);
  expect(out.map((f) => f.external_id)).toEqual(["2", "3"]);
});
