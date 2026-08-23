/*
 * Which number a library reports, and why it is not always the same one.
 *
 * A library that groups its media has two true counts and they answer different
 * questions. A music library of 1,158 artists holds 9,276 songs; a picture
 * library of 67 galleries holds thousands of photographs. `item_count` is what
 * the grid would show, `media_count` is how many files there are.
 *
 * The settings row reported `item_count` for every kind, so a music library
 * read "1,158 items" straight after a scan that had just found 9,276 songs —
 * a count of performers, labelled as a count of things. Reported as "the music
 * scan shows the total number of artists, not total items found".
 *
 * navCount already drew this line for the nav. The bug was one surface not
 * using it, which is why this tests the rule rather than either caller: two
 * surfaces disagreeing about one library is worse than either being wrong
 * alone.
 */
import { describe, it, expect } from "vitest";
import { navCount } from "./AppShell";
import type { Library } from "@/api/types";

function lib(kind: string, items: number, media: number): Library {
  return {
    id: 1,
    name: kind,
    kind,
    path: "X:/",
    created_at: 0,
    scanned_at: 0,
    item_count: items,
    media_count: media,
  } as unknown as Library;
}

describe("what a library counts", () => {
  // The reported case: tiles are artists, and nobody means artists.
  it("counts a music library in songs, not artists", () => {
    expect(navCount(lib("music", 1158, 9276))).toBe(9276);
  });

  it("counts a picture library in photographs, not galleries", () => {
    expect(navCount(lib("picture", 67, 3535))).toBe(3535);
  });

  /*
   * Films and shows keep the tile count on purpose. A film *is* a tile, and
   * "I have 20 shows" is what somebody means by a TV library — counting it in
   * episodes would answer a question nobody asked.
   */
  it("counts films as films", () => {
    expect(navCount(lib("movie", 1197, 1209))).toBe(1197);
  });

  it("counts shows as shows, not episodes", () => {
    expect(navCount(lib("show", 12, 480))).toBe(12);
  });
});
