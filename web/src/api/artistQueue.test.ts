import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { QueryClient } from "@tanstack/react-query";

import { fetchArtistQueue } from "./hooks";

/*
 * Flattening an artist into a queue.
 *
 * The rule this was written against — "an artist's children are albums" — is
 * true of a tidy folder tree and not of a real library. A file sitting in an
 * artist's folder with no album folder around it is parsed as a track belonging
 * to that artist and nothing else, so it is parented straight to the artist.
 *
 * Reported as two Play all buttons on one page, one of which did nothing: the
 * loose track's id was passed where an album id was expected, the server was
 * asked for its children, answered none, and the queue came back empty.
 */

let calls: string[];

beforeEach(() => {
  calls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      calls.push(String(url));
      const parent = Number(String(url).match(/parent_id=(\d+)/)?.[1]);
      // Album 10 holds two tracks; album 20 holds one.
      const tracks: Record<number, unknown[]> = {
        10: [
          { id: 101, kind: "track", title: "A1" },
          { id: 102, kind: "track", title: "A2" },
        ],
        20: [{ id: 201, kind: "track", title: "B1" }],
      };
      return new Response(
        JSON.stringify({ items: tracks[parent] ?? [], total: 0 }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }),
  );
});

afterEach(() => vi.unstubAllGlobals());

function qc() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

describe("an artist's play queue", () => {
  it("expands albums into their tracks, in order", async () => {
    const q = await fetchArtistQueue(qc(), [
      { id: 10, kind: "album" },
      { id: 20, kind: "album" },
    ]);
    expect(q).toEqual([101, 102, 201]);
  });

  /*
   * The case that produced the report. Before this, a loose track was treated
   * as an album, asked for children, and contributed nothing — so an artist
   * whose only release was a single had an empty queue.
   */
  it("keeps a track parented straight to the artist", async () => {
    const q = await fetchArtistQueue(qc(), [{ id: 9856, kind: "track" }]);
    expect(q).toEqual([9856]);
  });

  it("does not ask the server for a track's children", async () => {
    // One request per single, on every press, that can only answer "none".
    await fetchArtistQueue(qc(), [{ id: 9856, kind: "track" }]);
    expect(calls).toEqual([]);
  });

  /*
   * Order is the reason the two kinds are taken together rather than gathered
   * separately and concatenated: the queue matches what the page is showing,
   * which no amount of regrouping afterwards recovers.
   */
  it("keeps albums and loose tracks in the order the page lists them", async () => {
    const q = await fetchArtistQueue(qc(), [
      { id: 10, kind: "album" },
      { id: 9856, kind: "track" },
      { id: 20, kind: "album" },
    ]);
    expect(q).toEqual([101, 102, 9856, 201]);
  });

  it("is empty for an artist with nothing under it", async () => {
    expect(await fetchArtistQueue(qc(), [])).toEqual([]);
  });
});
