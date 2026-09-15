/*
 * What "Play all" and "Randomize all" actually queue.
 *
 * Reported from use: pick a genre or an actor, watch the grid narrow, press
 * Randomize all — and it randomises the *entire* library. The button sits
 * directly above a grid that has been filtered down, so "all" reads as "all of
 * these", and it meant all of everything.
 *
 * Nothing failed and nothing logged. The queue was a perfectly good queue; it
 * was simply of the wrong film. That is the shape of fault this file exists to
 * catch, so the assertions are about the request rather than about the result.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { fetchLibraryTracks } from "./hooks";

let asked: string[];

function stub(total = 2) {
  asked = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      asked.push(String(url));
      const u = new URL(String(url), "https://localhost");
      const offset = Number(u.searchParams.get("offset") ?? 0);
      // One page, then nothing — the loop stops on the server's own total.
      const items = offset === 0 ? [{ id: 1 }, { id: 2 }].slice(0, total) : [];
      return new Response(JSON.stringify({ items, total }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
}

function client() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

beforeEach(() => stub());
afterEach(() => vi.unstubAllGlobals());

describe("queueing a library", () => {
  it("carries the filters the grid is showing", async () => {
    // The reported fault, stated as the request that was never made.
    await fetchLibraryTracks(client(), 3, "movie", { genres: ["Horror"] });

    expect(asked[0]).toContain("genre=Horror");
    expect(asked[0]).toContain("library_id=3");
  });

  it("carries an actor the same way", async () => {
    // Reported against genre; the cause was that no filter reached the queue,
    // so the guard is worth having on more than the one that was noticed.
    await fetchLibraryTracks(client(), 3, "movie", { actors: [77] });
    expect(asked[0]).toContain("actor=77");
  });

  it("asks for the whole library when nothing is filtered", async () => {
    // The other half: Play all on an unfiltered grid still means everything.
    await fetchLibraryTracks(client(), 3, "movie");

    expect(asked[0]).toContain("library_id=3");
    expect(asked[0]).not.toContain("genre=");
    expect(asked[0]).not.toContain("actor=");
  });

  it("does not serve one narrowing from another's cache", async () => {
    /*
     * The quiet failure behind the obvious one. The page cache was keyed on the
     * library and the offset alone, so two different filter sets shared an
     * entry and the second queue would have been the first one's contents —
     * which looks like a shuffle doing something strange rather than like a
     * bug.
     */
    const qc = client();
    await fetchLibraryTracks(qc, 3, "movie", { genres: ["Horror"] });
    const first = asked.length;
    await fetchLibraryTracks(qc, 3, "movie", { genres: ["Comedy"] });

    expect(asked.length).toBeGreaterThan(first);
    expect(asked[asked.length - 1]).toContain("genre=Comedy");
  });

  it("keeps the order a queue is meant to play in", async () => {
    // Title order, which for episodes is the server's sort_title/season/episode
    // — a show queued in the order it is watched rather than alphabetically.
    await fetchLibraryTracks(client(), 3, "episode");
    expect(asked[0]).toContain("sort=title");
  });

  it("returns the ids it was given", async () => {
    const ids = await fetchLibraryTracks(client(), 3, "movie");
    expect(ids).toEqual([1, 2]);
  });
});
