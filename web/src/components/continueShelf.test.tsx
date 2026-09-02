/*
 * Continue Watching holds shows, and pressing one continues the show.
 *
 * Two things are asserted here and neither is visible to jsdom as *layout* —
 * they are both wiring, which is what this suite can prove. First, that a show
 * tile draws the episode's identity and the episode's progress rather than the
 * show's, because a show has neither. Second, that pressing it re-asks the
 * server instead of trusting the payload it was drawn from: that list is up to
 * ten seconds old, and the endpoint it comes from sends no-store precisely so
 * nobody resumes an episode they already finished.
 *
 * The queue matters as much as the destination. Navigating with no queue is
 * how a show came to play one episode and stop dead, and this holds the fix.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PosterTile } from "./PosterTile";
import { showContinueTarget } from "@/lib/continueShow";
import type { Item } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

function episode(id: number, season: number, ep: number, pos: number): Item {
  return {
    id,
    title: "Some Episode",
    kind: "episode",
    library_id: 1,
    season,
    episode: ep,
    duration_ms: 1_200_000,
    artwork: {},
    progress: { position_ms: pos, watched: false },
  } as unknown as Item;
}

function show(next?: Item): Item {
  return {
    id: 500,
    title: "Futurama",
    kind: "show",
    library_id: 1,
    artwork: { poster: "p.jpg" },
    next_episode: next ?? null,
  } as unknown as Item;
}

async function render(item: Item, onOpen?: () => void) {
  await act(async () => {
    root.render(
      <FocusProvider>
        <MemoryRouter>
          <PosterTile item={item} onOpen={onOpen} />
        </MemoryRouter>
      </FocusProvider>,
    );
  });
}

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

const text = () => host.textContent ?? "";
const bar = () =>
  host.querySelector<HTMLElement>(".poster-tile__progress")?.style.width ?? "";

describe("a show on the Continue Watching shelf", () => {
  it("names itself by the show and the episode it would play", async () => {
    await render(show(episode(9, 2, 1, 600_000)));
    expect(text()).toContain("Futurama");
    // The show carries no season or episode of its own; without reading
    // next_episode this line is empty and the tile says only "Futurama".
    expect(text()).toMatch(/S02E01/i);
  });

  it("draws its resume bar from the episode, not the show", async () => {
    // Half of a 20-minute episode. The show has no duration at all, so
    // reading the show gives 0% under a half-watched series.
    await render(show(episode(9, 2, 1, 600_000)));
    expect(bar()).toBe("50%");
  });

  it("still works for a film, which has its own progress", async () => {
    const film = {
      id: 7,
      title: "Arrival",
      kind: "movie",
      library_id: 1,
      duration_ms: 1_000_000,
      artwork: {},
      progress: { position_ms: 250_000, watched: false },
    } as unknown as Item;
    await render(film);
    expect(bar()).toBe("25%");
  });
});

describe("pressing a show tile", () => {
  it("asks the server what comes next rather than trusting the tile", async () => {
    const calls: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        calls.push(String(url));
        const body = String(url).includes("/continue")
          ? // Deliberately NOT the episode the tile was drawn with: the shelf
            // is stale and the server has moved on.
            { episode: episode(11, 2, 3, 0), resume: false, exhausted: false }
          : {
              episodes: [
                episode(9, 2, 1, 0),
                episode(10, 2, 2, 0),
                episode(11, 2, 3, 0),
                episode(12, 2, 4, 0),
              ],
            };
        return new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );

    const target = await showContinueTarget(500);

    expect(calls.some((u) => u.includes("/api/items/500/continue"))).toBe(true);
    expect(target).toEqual({
      kind: "play",
      episodeID: 11,
      // From this episode onward, never the whole show: including the earlier
      // ones makes Previous walk back through episodes already watched.
      queue: [11, 12],
    });
  });

  it("reports a finished show rather than replaying the finale", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(JSON.stringify({ resume: false, exhausted: true }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
      ),
    );
    expect(await showContinueTarget(500)).toEqual({ kind: "exhausted" });
  });
});
