/*
 * One answer to "what can I do to this", shared by every poster.
 *
 * Right-click worked on a poster in the library grid and did nothing on the
 * same poster in search, in a collection, or under a detail page. That is worse
 * than having no menus at all, because it teaches the gesture and then withdraws
 * it — and the surface that would drift is whichever one nobody opened that
 * week.
 *
 * So the actions live in one hook and these assert the *rule*, not any one
 * caller. The alternative was four copies and a test per screen, which is how
 * four screens end up disagreeing while every test passes.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { useItemActions } from "./itemActions";
import type { Item } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let actions: (item: Item) => { label: string }[];

function Probe() {
  actions = useItemActions().actions;
  return null;
}

function item(over: Partial<Item>): Item {
  return {
    id: 1,
    title: "Thing",
    kind: "movie",
    library_id: 1,
    artwork: {},
    ...over,
  } as unknown as Item;
}

beforeEach(async () => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(JSON.stringify({ items: [], total: 0 }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    ),
  );
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter>
              <Probe />
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const labels = (i: Item) => actions(i).map((a) => a.label);

describe("what a poster offers", () => {
  it("gives an untouched film the full set", () => {
    expect(labels(item({ kind: "movie" }))).toEqual([
      "Play",
      "Mark as watched",
      "Play next",
      "Add to queue",
      "Go to details",
    ]);
  });

  // The state is a toggle, or the menu cannot undo itself.
  it("offers the undo on something already watched", () => {
    const l = labels(item({ progress: { position_ms: 0, watched: true } } as Partial<Item>));
    expect(l).toContain("Mark as unwatched");
    expect(l).not.toContain("Mark as watched");
  });

  /*
   * A half-watched film is the one tile that carries a progress bar, and the
   * one thing somebody right-clicking it might mean was the thing this menu
   * could not do. Both halves are asserted: the second verb appears where there
   * is a position to ignore, and does *not* appear where there is none — two
   * verbs meaning the same thing is its own kind of broken.
   */
  it("separates resuming from starting over, only where there is a position", () => {
    const started = labels(
      item({ progress: { position_ms: 90_000, watched: false } } as Partial<Item>),
    );
    expect(started.slice(0, 2)).toEqual(["Resume", "Play from start"]);

    const fresh = labels(item({ kind: "movie" }));
    expect(fresh).toContain("Play");
    expect(fresh).not.toContain("Play from start");
  });

  // A finished item's saved position is past its own end. Offering to resume
  // there is offering to start at the credits.
  it("does not offer to resume something finished", () => {
    const l = labels(
      item({ progress: { position_ms: 5_400_000, watched: true } } as Partial<Item>),
    );
    expect(l).toContain("Play");
    expect(l).not.toContain("Resume");
  });

  // Played, not watched: "Mark as watched" on a song is the interface reading
  // from the wrong half of itself.
  it("says played for music", () => {
    expect(labels(item({ kind: "track" }))).toContain("Mark as played");
  });

  /*
   * Playlists are a music format (ADR 0030). The track row has offered this
   * since playlists shipped; a track's poster never did, which is the same
   * capability shipped half-reachable.
   */
  it("offers a playlist to a track and not to a film", () => {
    expect(labels(item({ kind: "track" }))).toContain("Add to playlist…");
    expect(labels(item({ kind: "movie" }))).not.toContain("Add to playlist…");
  });

  /*
   * A container used to return nothing, so a show — the most common tile in a
   * television library — had no menu at all. It cannot have the leaf's list;
   * this is the list it can have.
   */
  it("gives a show its own set rather than a film's", () => {
    const l = labels(item({ kind: "show", child_count: 3 } as Partial<Item>));
    expect(l).toEqual([
      "Play all",
      "Shuffle",
      "Mark all as watched",
      "Mark all as unwatched",
      "Go to details",
    ]);
    // Not the leaf's. Queueing a show is not queueing a thing.
    expect(l).not.toContain("Add to queue");
    expect(l).not.toContain("Play");
  });

  it("counts an album as heard rather than seen", () => {
    const l = labels(item({ kind: "album", child_count: 9 } as Partial<Item>));
    expect(l).toContain("Mark all as played");
    expect(l).toContain("Shuffle");
  });

  /*
   * A collection's membership runs through item_collection and a playlist's
   * through playlist_entry — neither has children under parent_id, so neither
   * has a queue this can build. A Play all that silently queues nothing is
   * worse than no Play all.
   */
  it("will not offer to play what it cannot gather", () => {
    for (const kind of ["collection", "playlist"]) {
      const l = labels(item({ kind, child_count: 4 } as Partial<Item>));
      expect(l).toEqual(["Go to details"]);
    }
  });

  /*
   * A photograph is neither watched nor queued, and has no page worth visiting
   * — the reason a photo tile selects into the banner rather than navigating.
   * The gallery holding it does have one.
   */
  it("offers a gallery its page and a photograph nothing", () => {
    expect(labels(item({ kind: "gallery", child_count: 40 } as Partial<Item>))).toEqual([
      "Go to details",
    ]);
    expect(labels(item({ kind: "photo" }))).toEqual([]);
  });

  /*
   * Removal deletes files from disk when the server allows it, so it is behind
   * the same admin gate the track row uses. These run without an authenticated
   * admin, so its absence here is the assertion: the gate is real, not decor.
   */
  it("hides removal from someone who is not an admin", () => {
    for (const kind of ["movie", "show", "photo", "gallery"]) {
      expect(labels(item({ kind, child_count: 2 } as Partial<Item>)).join(" ")).not.toContain(
        "Remove",
      );
    }
  });
});
