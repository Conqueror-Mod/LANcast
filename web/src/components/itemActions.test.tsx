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
  actions = useItemActions();
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
  it("gives a film the full set", () => {
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

  // Played, not watched: "Mark as watched" on a song is the interface reading
  // from the wrong half of itself.
  it("says played for music", () => {
    expect(labels(item({ kind: "track" }))).toContain("Mark as played");
  });

  /*
   * The refusals. "Play" on a folder is not a smaller version of playing, and a
   * photograph is neither watched nor possessed of a page worth visiting. An
   * empty list is what PosterTile turns into no menu at all.
   */
  it("offers nothing on a container", () => {
    expect(labels(item({ kind: "show", child_count: 3 } as Partial<Item>))).toEqual([]);
    expect(labels(item({ kind: "album", child_count: 9 } as Partial<Item>))).toEqual([]);
  });

  it("offers nothing on a photograph", () => {
    expect(labels(item({ kind: "photo" }))).toEqual([]);
  });

  // Removing a title deletes files from disk when the server allows it. The
  // detail page keeps it, behind a dialog that names what is about to go.
  it("never offers to remove a title", () => {
    expect(labels(item({ kind: "movie" })).join(" ")).not.toContain("Remove");
  });
});
