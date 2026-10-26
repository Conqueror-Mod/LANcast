/*
 * The finished-tick on a tile.
 *
 * jsdom sees no layout, so nothing here proves the mark is in the corner or
 * that it clears the progress bar — only looking at it can. What it can prove
 * is *when* the mark appears, which is where this feature's risk actually sits:
 * a tick is only worth drawing if it is trustworthy, and both ways of being
 * wrong are silent.
 *
 * The one that would have shipped is `!item.unwatched_episodes`. The field is
 * absent on a film, on an album, and on a show with no episodes on disk, and
 * `!undefined` is true — so the obvious falsy test ticks the entire library.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PosterTile } from "./PosterTile";
import { collectionStanding, isWatched, watchedLabel } from "@/lib/watchedMark";
import type { Item } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

function item(over: Partial<Item>): Item {
  return {
    id: 1,
    library_id: 1,
    kind: "movie",
    title: "A Film",
    ...over,
  } as Item;
}

describe("what carries a finished-tick", () => {
  it("marks a film the account has finished", () => {
    expect(
      isWatched(item({ progress: { position_ms: 0, watched: true } })),
    ).toBe(true);
  });

  it("leaves a film nobody finished unmarked", () => {
    expect(
      isWatched(item({ progress: { position_ms: 0, watched: false } })),
    ).toBe(false);
    // No progress row at all — the ordinary state of most of a library.
    expect(isWatched(item({}))).toBe(false);
  });

  it("does not mark a film stopped part-way through", () => {
    // A saved position is not a viewing, and the tick must not say it is.
    expect(
      isWatched(
        item({ progress: { position_ms: 3_600_000, watched: false } }),
      ),
    ).toBe(false);
  });

  it("marks a show with no episodes left", () => {
    expect(isWatched(item({ kind: "show", unwatched_episodes: 0 }))).toBe(true);
  });

  it("does not mark a show one episode short", () => {
    /*
     * The evening before somebody finishes a series is exactly when they are
     * reading this tile, so being wrong here is being wrong at the worst
     * moment.
     */
    expect(isWatched(item({ kind: "show", unwatched_episodes: 1 }))).toBe(
      false,
    );
  });

  it("marks a season with no episodes left, and not one an episode short", () => {
    // A show page is a grid of season cards, and a finished season had no tick.
    expect(isWatched(item({ kind: "season", unwatched_episodes: 0 }))).toBe(true);
    expect(isWatched(item({ kind: "season", unwatched_episodes: 1 }))).toBe(false);
    expect(isWatched(item({ kind: "season" }))).toBe(false);
    expect(watchedLabel(item({ kind: "season", unwatched_episodes: 0 }))).toBe(
      "Every episode watched",
    );
  });

  it("does not mark a show whose episode count is absent", () => {
    /*
     * The falsy-test bug, pinned. A show with no episodes on disk omits the
     * field — an empty series is not a finished one — and `!undefined` would
     * tick it.
     */
    expect(isWatched(item({ kind: "show" }))).toBe(false);
  });

  it("does not tick a film because it has no episode count", () => {
    // The same bug seen from the other side, and the louder half of it: every
    // film in the library carries no `unwatched_episodes` at all.
    expect(isWatched(item({ kind: "movie" }))).toBe(false);
    expect(
      isWatched(item({ kind: "movie", progress: { position_ms: 0, watched: false } })),
    ).toBe(false);
  });

  it("does not ask a show about its own progress row", () => {
    /*
     * A show has no playback row, so anything that reads `progress` on one is
     * reading a field the server never sets. Were a client to synthesise one,
     * the aggregate must still win — otherwise a series is marked finished
     * because somebody opened it once.
     */
    expect(
      isWatched(
        item({
          kind: "show",
          unwatched_episodes: 3,
          progress: { position_ms: 0, watched: true },
        }),
      ),
    ).toBe(false);
  });

  it("leaves music alone, which already says this its own way", () => {
    /*
     * A track carries the same flag a film does, and the client already decided
     * how a finished track reads: TrackList dims the title, because a checkmark
     * against every row of a fifteen-track album is noise. A tick here would be
     * the same signal said twice in two visual languages.
     */
    for (const kind of ["track", "album", "artist"]) {
      expect(
        isWatched(item({ kind, progress: { position_ms: 0, watched: true } })),
      ).toBe(false);
    }
  });

  it("leaves photographs alone", () => {
    for (const kind of ["photo", "gallery"]) {
      expect(
        isWatched(item({ kind, progress: { position_ms: 0, watched: true } })),
      ).toBe(false);
    }
  });

  it("says how a series earned its tick", () => {
    // "Watched" on a series could mean one episode. The tick is the only thing
    // on the tile that answers, so the accessible name carries it.
    expect(watchedLabel(item({ kind: "show" }))).toBe("Every episode watched");
    expect(watchedLabel(item({ kind: "movie" }))).toBe("Watched");
  });
});

/*
 * And that the tile actually draws it.
 *
 * Separate from the predicate above on purpose. The client suite exists partly
 * because a settings shell once had panes that were not wired to its buttons —
 * every unit answered correctly and the screen did nothing. A predicate with no
 * call site would pass every test in this file.
 */
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
  vi.unstubAllGlobals();
});

function render(it: Item) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
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

const mark = () => host.querySelector(".poster-tile__watched");

describe("the tile that draws it", () => {
  it("puts a mark on a watched film", () => {
    render(item({ progress: { position_ms: 0, watched: true } }));
    expect(mark()).toBeTruthy();
  });

  it("puts no mark on an unwatched one", () => {
    render(item({}));
    expect(mark()).toBeNull();
  });

  it("puts a mark on a show with nothing left", () => {
    render(item({ kind: "show", title: "A Series", unwatched_episodes: 0 }));
    expect(mark()).toBeTruthy();
  });

  it("names the mark, since a tick alone says nothing to a screen reader", () => {
    render(item({ kind: "show", title: "A Series", unwatched_episodes: 0 }));
    expect(mark()?.getAttribute("aria-label")).toBe("Every episode watched");
  });

  it("keeps the tile's own name off the mark", () => {
    /*
     * The tile is a button with an aria-label of its own. A second labelled
     * element inside it is fine; a second *focusable* one would break the grid's
     * keyboard model, which walks tiles.
     */
    render(item({ progress: { position_ms: 0, watched: true } }));
    expect(mark()?.tagName.toLowerCase()).toBe("span");
    expect(mark()?.hasAttribute("tabindex")).toBe(false);
  });
});

/*
 * A collection, which had no way to say it was finished or begun. Reported:
 * the Collections page showed nothing about either.
 */
describe("a collection's standing", () => {
  const coll = (p?: { total: number; watched: number; in_progress: number }) =>
    item({ kind: "collection", title: "A Franchise", child_count: p?.total ?? 3, collection_progress: p });

  it("is finished only when every film is", () => {
    expect(isWatched(coll({ total: 3, watched: 3, in_progress: 0 }))).toBe(true);
    expect(isWatched(coll({ total: 3, watched: 2, in_progress: 1 }))).toBe(false);
    // Absent progress is a collection with no films on disk: not finished.
    expect(isWatched(coll(undefined))).toBe(false);
    expect(watchedLabel(coll({ total: 3, watched: 3, in_progress: 0 }))).toBe("Every film watched");
  });

  it("draws a bar and says how far, once begun and not finished", () => {
    expect(collectionStanding(coll({ total: 3, watched: 0, in_progress: 0 }))).toBeNull();
    expect(collectionStanding(coll({ total: 3, watched: 3, in_progress: 0 }))).toBeNull();
    const begun = collectionStanding(coll({ total: 4, watched: 1, in_progress: 1 }));
    expect(begun?.label).toBe("1 of 4 watched");
    expect(begun?.pct).toBeCloseTo(37.5);
    // Started with nothing finished still shows a bar, not an empty one.
    expect(collectionStanding(coll({ total: 4, watched: 0, in_progress: 1 }))?.pct).toBeGreaterThan(0);
  });

  it("puts it on the tile in place of the film count", () => {
    render(coll({ total: 5, watched: 2, in_progress: 0 }));
    expect(host.querySelector(".poster-tile__year")?.textContent).toBe("2 of 5 watched");
    expect(host.querySelector(".poster-tile__progress")).toBeTruthy();
    expect(mark()).toBeNull();
  });

  it("keeps the film count, and no bar, on one nobody has begun", () => {
    render(coll({ total: 5, watched: 0, in_progress: 0 }));
    expect(host.querySelector(".poster-tile__year")?.textContent).toBe("5 films");
    expect(host.querySelector(".poster-tile__progress")).toBeNull();
  });

  it("ticks a finished one", () => {
    render(coll({ total: 5, watched: 5, in_progress: 0 }));
    expect(mark()?.getAttribute("aria-label")).toBe("Every film watched");
  });
});
