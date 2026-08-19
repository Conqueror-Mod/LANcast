/*
 * A season renders as rows, and the rows say the right things.
 *
 * The parts worth pinning are the ones that go wrong quietly: a progress bar on
 * an episode nobody has started, a bar sitting at 100% on one already finished,
 * and the no-artwork state rendering as a hole rather than as a number. None of
 * those throw; they just make the screen look broken in a way that reads as
 * missing data.
 */
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
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
  act(() => {
    root.render(
      <FocusProvider>
        <MemoryRouter>
          <EpisodeList episodes={episodes} queue={episodes.map((e) => e.id)} />
        </MemoryRouter>
      </FocusProvider>,
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
   * The synopsis is the reason a season page exists — it is what the poster
   * grid had nowhere to put.
   */
  it("shows the synopsis", () => {
    render([episode({ overview: "Fry is cryogenically frozen." })]);
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
    expect(host.querySelector(".eprow__watched")).not.toBeNull();
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

  // And gets out of the way once there is one, without the row changing shape.
  it("renders the still when one exists", () => {
    render([episode({ artwork: { thumb: "/api/artwork/abc.jpg" } })]);

    expect(host.querySelector(".eprow__still img")).not.toBeNull();
    expect(host.querySelector(".eprow__still--empty")).toBeNull();
  });

  // The row is the play control, so it has to say so to anybody not looking at
  // it.
  it("labels each row as a play action", () => {
    render([episode({ episode: 3, title: "I, Roommate" })]);
    expect(rows()[0].getAttribute("aria-label")).toBe("Play episode 3, I, Roommate");
  });
});
