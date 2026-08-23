/*
 * A season renders as rows, and the rows say the right things.
 *
 * The parts worth pinning are the ones that go wrong quietly: a progress bar on
 * an episode nobody has started, a bar sitting at 100% on one already finished,
 * and the no-artwork state rendering as a hole rather than as a number. None of
 * those throw; they just make the screen look broken in a way that reads as
 * missing data.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { EpisodeList } from "./EpisodeList";
import type { Item } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

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

function episode(over: Partial<Item> = {}): Item {
  return {
    id: 1,
    library_id: 1,
    kind: "episode",
    title: "Space Pilot 3000",
    sort_title: "space pilot 3000",
    season: 1,
    episode: 1,
    duration_ms: 1_351_423,
    missing: false,
    ...over,
  } as Item;
}

function render(episodes: Item[]) {
  // The list marks episodes watched through a mutation, so it needs a client.
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  act(() => {
    root.render(
      <QueryClientProvider client={qc}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter>
              <EpisodeList
                episodes={episodes}
                queue={episodes.map((e) => e.id)}
                parentID={99}
              />
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
}

const rows = () => [...host.querySelectorAll(".eprow")];
const bars = () => [...host.querySelectorAll(".eprow__bar-fill")];

describe("the episode list", () => {
  it("renders a row per episode, in the order given", () => {
    render([
      episode({ id: 1, episode: 1, title: "Space Pilot 3000" }),
      episode({ id: 2, episode: 2, title: "The Series Has Landed" }),
    ]);

    expect(rows()).toHaveLength(2);
    expect(rows()[0].textContent).toContain("Space Pilot 3000");
    expect(rows()[1].textContent).toContain("The Series Has Landed");
  });

  /*
   * The synopsis is the reason a season page exists — it is what the poster grid
   * had nowhere to put.
   *
   * Watched, because the spoiler rule now hides the synopsis of an episode
   * nobody has started (see the spoiler tests below). This test predates that
   * rule and is updated rather than worked around: an episode you have seen is
   * the case where "does the synopsis render at all" is the question being
   * asked.
   */
  it("shows the synopsis", () => {
    render([
      episode({
        overview: "Fry is cryogenically frozen.",
        progress: { position_ms: 0, watched: true },
      }),
    ]);
    expect(host.textContent).toContain("Fry is cryogenically frozen.");
  });

  // An untouched season should not be a wall of empty bars.
  it("draws no progress bar on an episode nobody has started", () => {
    render([episode()]);
    expect(bars()).toHaveLength(0);
  });

  /*
   * Nor a wall of full ones. Watched is said by the row's own state — a tick
   * and a dimmed title — not by a bar pinned at 100%.
   */
  it("draws no progress bar on a finished episode", () => {
    render([
      episode({ progress: { position_ms: 1_351_637, watched: true } }),
    ]);

    expect(bars()).toHaveLength(0);
    expect(rows()[0].className).toContain("eprow--watched");
    expect(host.querySelector(".eprow__mark.is-on")).not.toBeNull();
  });

  it("draws a bar only for an episode part way through", () => {
    render([
      episode({ duration_ms: 1_000_000, progress: { position_ms: 250_000, watched: false } }),
    ]);

    const bar = bars();
    expect(bar).toHaveLength(1);
    expect((bar[0] as HTMLElement).style.width).toBe("25%");
    // And says how much is left, which is the number somebody is deciding on.
    expect(host.textContent).toMatch(/left/);
  });

  /*
   * The no-artwork state, which is every row until stills are stored: the
   * episode number where the still will be. A number drawn as a number reads as
   * a design; an empty box reads as a broken image.
   */
  it("renders the episode number when there is no still", () => {
    render([episode({ episode: 7 })]);

    const empty = host.querySelector(".eprow__still--empty");
    expect(empty).not.toBeNull();
    expect(empty?.textContent).toBe("7");
    expect(host.querySelector(".eprow__still img")).toBeNull();
  });

  /*
   * And gets out of the way once there is one, without the row changing shape.
   *
   * The src is built from the hash rather than being the hash. The first version
   * of this row used the raw value, which is truthy — so it took the image
   * branch and rendered a broken image instead of falling back to the number,
   * on the 993 episodes that already had stills stored.
   */
  it("builds the still's URL from its hash", () => {
    render([episode({ artwork: { thumb: "847b43c1" } })]);

    const img = host.querySelector<HTMLImageElement>(".eprow__still img");
    expect(img).not.toBeNull();
    expect(img!.getAttribute("src")).toBe("/api/artwork/847b43c1?size=poster");
    expect(host.querySelector(".eprow__still--empty")).toBeNull();
  });

  // The row is the play control, so it has to say so to anybody not looking at
  // it.
  it("labels each row as a play action", () => {
    render([episode({ episode: 3, title: "I, Roommate" })]);
    expect(
      host.querySelector(".eprow__play")?.getAttribute("aria-label"),
    ).toBe("Play episode 3, I, Roommate");
  });
});

describe("marking an episode watched", () => {
  function stubFetch() {
    const calls: { url: string; body: unknown }[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        calls.push({ url, body: init?.body ? JSON.parse(String(init.body)) : null });
        return new Response("{}", {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );
    return calls;
  }

  afterEach(() => vi.unstubAllGlobals());

  it("marks an unwatched episode watched", async () => {
    const calls = stubFetch();
    render([episode({ id: 12 })]);

    await act(async () => {
      host.querySelector<HTMLButtonElement>(".eprow__mark")!.click();
      await Promise.resolve();
    });

    expect(calls).toHaveLength(1);
    expect(calls[0].url).toContain("/api/items/12/progress");
    expect(calls[0].body).toEqual({ position_ms: 0, watched: true });
  });

  /*
   * Clearing it sends a position of zero as well. Leaving the position behind
   * would put the episode straight back on the Continue shelf, which is the
   * opposite of what "I have not seen this" means.
   */
  it("clears the position when marking unwatched", async () => {
    const calls = stubFetch();
    render([
      episode({ id: 12, progress: { position_ms: 900_000, watched: true } }),
    ]);

    await act(async () => {
      host.querySelector<HTMLButtonElement>(".eprow__mark")!.click();
      await Promise.resolve();
    });

    expect(calls[0].body).toEqual({ position_ms: 0, watched: false });
  });

  // The control says which way it goes, since a tick alone does not tell a
  // screen-reader user whether pressing it sets or clears.
  it("labels the control by what pressing it does", () => {
    render([episode({ title: "I, Roommate" })]);
    expect(
      host.querySelector(".eprow__mark")?.getAttribute("aria-label"),
    ).toBe("Mark I, Roommate watched");

    render([
      episode({ title: "I, Roommate", progress: { position_ms: 0, watched: true } }),
    ]);
    expect(
      host.querySelector(".eprow__mark")?.getAttribute("aria-label"),
    ).toBe("Mark I, Roommate unwatched");
  });
});

/*
 * Spoiler protection, as the list applies it.
 *
 * The rule itself is tested in lib/spoilers.test.ts; these assert the list obeys
 * it, and that hiding a synopsis says so rather than leaving a gap — silence
 * would read as missing metadata, which is the failure this screen was built to
 * stop looking like.
 *
 * Default mode, since these render without touching the device setting.
 */
describe("spoilers on the list", () => {
  it("hides the synopsis of an episode nobody has started, and says so", () => {
    render([episode({ overview: "Leela learns who her parents are." })]);

    expect(host.textContent).not.toContain("Leela learns");
    expect(host.textContent).toContain("Synopsis hidden until watched");
  });

  // Two minutes in, the guard gets out of the way.
  it("shows the synopsis once an episode has been started", () => {
    render([
      episode({
        overview: "Leela learns who her parents are.",
        progress: { position_ms: 120_000, watched: false },
      }),
    ]);

    expect(host.textContent).toContain("Leela learns");
    expect(host.textContent).not.toContain("Synopsis hidden");
  });

  it("shows the synopsis of a watched episode", () => {
    render([
      episode({
        overview: "Leela learns who her parents are.",
        progress: { position_ms: 0, watched: true },
      }),
    ]);

    expect(host.textContent).toContain("Leela learns");
  });

  /*
   * The still survives the default setting: a frame rarely gives a plot away,
   * and it is what makes a row identifiable at a glance.
   */
  it("keeps the still on an unstarted episode at the default setting", () => {
    render([episode({ artwork: { thumb: "847b43c1" } })]);
    expect(host.querySelector(".eprow__still img")).not.toBeNull();
  });
});
