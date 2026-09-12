/*
 * The Games tab's wiring.
 *
 * jsdom performs no layout, so nothing here is about whether the grid looks
 * right — that is what looking at it is for. What these prove is the part a
 * screenshot cannot: that each failure state says its own sentence, that the
 * controls actually reorder and narrow the list, and that a tile's Play button
 * hands the client an app id and nothing else, which is the rule the whole
 * launch path rests on (ADR 0066).
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { Games } from "./Games";
import { DISPLAY_DEFAULT, type GameRow, type GamesResult } from "@/lib/games";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

// Invented ids and titles only.
function game(p: Partial<GameRow> & { id: string; name: string }): GameRow {
  return {
    size_bytes: 0,
    last_played: 0,
    install_path: "",
    has_poster: false,
    has_header: false,
    hidden: false,
    favourite: false,
    // Answered by default, so that the tests about the grid are about the grid.
    // The picker has its own block below, where the answer is deliberately
    // absent.
    display: DISPLAY_DEFAULT,
    ...p,
  };
}

const three: GameRow[] = [
  game({ id: "700010", name: "Zephyr Drift", last_played: 10, size_bytes: 900 }),
  game({ id: "700011", name: "Alpha Protocol", last_played: 0, size_bytes: 100 }),
  game({ id: "700012", name: "Meridian", last_played: 500, size_bytes: 400 }),
];

const bindings = [
  "lancastGames",
  "lancastGameArt",
  "lancastLaunchGame",
  "lancastOpenGameFolder",
  "lancastSetGameFlags",
  "lancastDisplays",
  "lancastSetGameDisplay",
] as const;

// Two screens, one of them the one this window is on — the shape of the choice
// somebody is actually making.
const SCREENS = [
  {
    device: "\\\\.\\DISPLAY1",
    label: "Display 1 — 1920 x 1080 (main)",
    primary: true,
    current: true,
    width: 1920,
    height: 1080,
  },
  {
    device: "\\\\.\\DISPLAY2",
    label: "Display 2 — 1440 x 960",
    primary: false,
    current: false,
    width: 1440,
    height: 960,
  },
];

function stub(result: GamesResult) {
  const games = vi.fn(async () => result);
  const launch = vi.fn(async () => ({ ok: true }));
  const setFlags = vi.fn(async () => ({ ok: true }));
  const displays = vi.fn(async () => ({ ok: true, displays: SCREENS }));
  const setDisplay = vi.fn(async () => ({ ok: true }));
  const w = window as unknown as Record<string, unknown>;
  w.lancastGames = games;
  w.lancastGameArt = vi.fn(async () => ({ ok: true, uri: "" }));
  w.lancastLaunchGame = launch;
  w.lancastOpenGameFolder = vi.fn(async () => ({ ok: true }));
  w.lancastSetGameFlags = setFlags;
  w.lancastDisplays = displays;
  w.lancastSetGameDisplay = setDisplay;
  return { games, launch, setFlags, displays, setDisplay };
}

// A button anywhere on the page, by its visible text.
function byText(text: string): HTMLButtonElement {
  const el = [...host.querySelectorAll("button")].find((b) =>
    b.textContent?.includes(text),
  );
  if (!el) throw new Error(`no button saying ${text}`);
  return el as HTMLButtonElement;
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  const w = window as unknown as Record<string, unknown>;
  for (const b of bindings) delete w[b];
  localStorage.clear();
});

async function render() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={["/games"]}>
          <Games />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await settle();
}

/*
 * Let the binding promise and the query state that follows it come to rest.
 *
 * One flush is not enough and the reason is worth stating, because it looks
 * like a product bug when it bites: a resolved binding still takes several
 * microtask turns to become rendered rows — the promise resolves, the query
 * transitions, React re-renders — and a test that flushes once catches the
 * screen mid-way and reads "Looking…". Every assertion here would then be
 * about a loading state rather than about the grid.
 */
async function settle(times = 6) {
  for (let i = 0; i < times; i++) {
    await act(async () => {
      // A timer, not just a microtask. React Query batches the notification
      // that turns a resolved query into a re-render through notifyManager,
      // which schedules on a macrotask — so flushing promises alone can spin
      // for ever with the screen still saying "Looking…". That is what made
      // these tests look nondeterministic: the ones that passed happened to
      // dispatch an event that pumped a timer, and the ones that failed
      // asserted straight after rendering.
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
  }
}

function text() {
  return host.textContent ?? "";
}

function tileNames() {
  return [...host.querySelectorAll(".games__name")].map((e) => e.textContent);
}

function buttonFor(name: string, label: string): HTMLButtonElement {
  const tile = [...host.querySelectorAll(".games__tile")].find((t) =>
    t.querySelector(".games__name")?.textContent?.includes(name),
  );
  if (!tile) throw new Error(`no tile for ${name}`);
  const btn = [...tile.querySelectorAll("button")].find(
    (b) => b.textContent === label || b.getAttribute("aria-label") === label,
  );
  if (!btn) throw new Error(`no ${label} button for ${name}`);
  return btn as HTMLButtonElement;
}

async function click(el: HTMLElement) {
  await act(async () => {
    el.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
  // A click here starts a mutation, which then invalidates and refetches the
  // list: three round trips before the grid is right again.
  await settle();
}

describe("the games tab", () => {
  it("says where games live when there are no bindings", async () => {
    // A browser tab on the same server. The page is reachable — somebody can
    // follow a link from the machine where it works — so it answers rather
    // than 404s.
    await render();
    expect(text()).toContain("desktop app");
  });

  it("points at the setting when games are switched off", async () => {
    stub({ status: "disabled" });
    await render();
    expect(text()).toContain("Show my installed games");
  });

  it("says Steam is absent rather than showing an empty grid", async () => {
    // The distinction the Status type exists for: no Steam is a sentence, an
    // empty grid is not.
    stub({ status: "not-installed" });
    await render();
    expect(text()).toContain("No Steam installation");
    expect(text()).not.toContain("Filter by name");
  });

  it("reports a Steam it could not read", async () => {
    stub({ status: "error", error: "the library list is unreadable" });
    await render();
    expect(text()).toContain("the library list is unreadable");
  });

  it("lists installed games by name", async () => {
    stub({ status: "ok", games: three });
    await render();
    expect(tileNames()).toEqual(["Alpha Protocol", "Meridian", "Zephyr Drift"]);
  });

  it("reorders by last played, with the never-played last", async () => {
    stub({ status: "ok", games: three });
    await render();
    const select = host.querySelector("select") as HTMLSelectElement;
    await act(async () => {
      select.value = "played";
      select.dispatchEvent(new Event("change", { bubbles: true }));
    });
    // Meridian was played most recently, Zephyr before it, and Alpha never —
    // so Alpha goes last rather than first.
    expect(tileNames()).toEqual(["Meridian", "Zephyr Drift", "Alpha Protocol"]);
  });

  it("narrows the grid by name", async () => {
    stub({ status: "ok", games: three });
    await render();
    const input = host.querySelector(".games__filter") as HTMLInputElement;
    await act(async () => {
      /*
       * Through the prototype setter, not input.value = "…".
       *
       * React keeps a tracker of the last value it saw on the node. A direct
       * assignment updates the node and the tracker together, so the input
       * event that follows looks like no change at all and onChange never
       * runs — the box shows the text and the grid ignores it, which is a
       * test that fails for a reason that has nothing to do with the screen.
       */
      const setValue = Object.getOwnPropertyDescriptor(
        HTMLInputElement.prototype,
        "value",
      )!.set!;
      setValue.call(input, "merid");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    expect(tileNames()).toEqual(["Meridian"]);
  });

  it("hands the client an app id and nothing else when playing", async () => {
    // The whole launch rule in one assertion: no URI, no path, one id.
    const { launch } = stub({ status: "ok", games: three });
    await render();
    await click(buttonFor("Meridian", "Play"));
    expect(launch).toHaveBeenCalledWith("700012");
  });

  it("hides a game and refetches the list", async () => {
    // A write that changes what a list holds must invalidate that list.
    const { games, setFlags } = stub({ status: "ok", games: three });
    await render();
    expect(games).toHaveBeenCalledTimes(1);
    await click(buttonFor("Meridian", "Hide"));
    expect(setFlags).toHaveBeenCalledWith("700012", true, false);
    expect(games).toHaveBeenCalledTimes(2);
  });

  it("keeps hidden games out until they are asked for", async () => {
    stub({
      status: "ok",
      games: [...three, game({ id: "700013", name: "Put Away", hidden: true })],
    });
    await render();
    expect(tileNames()).not.toContain("Put Away");

    const toggle = [...host.querySelectorAll("button")].find((b) =>
      b.textContent?.startsWith("Show hidden"),
    );
    expect(toggle?.textContent).toContain("(1)");
    await click(toggle as HTMLButtonElement);
    expect(tileNames()).toContain("Put Away");
  });

  it("gives favourites a shelf of their own", async () => {
    stub({
      status: "ok",
      games: [...three, game({ id: "700014", name: "Kept Close", favourite: true })],
    });
    await render();
    expect(text()).toContain("Favourites");
    // Once on the shelf and once in the full list below it.
    expect(tileNames().filter((n) => n === "Kept Close")).toHaveLength(2);
  });

  it("rescans on demand", async () => {
    const { games } = stub({ status: "ok", games: three });
    await render();
    const rescan = [...host.querySelectorAll("button")].find(
      (b) => b.textContent === "Rescan",
    );
    await click(rescan as HTMLButtonElement);
    expect(games).toHaveBeenCalledTimes(2);
  });
});

/*
 * The display picker (ADR 0066 amendment).
 *
 * What matters here is the order and the asking: a game nobody has answered
 * for must not start until somebody has, the answer must be saved before the
 * launch because the client reads it from disk at launch time, and a game that
 * has an answer must never be asked again.
 */
describe("the display picker", () => {
  const unanswered = [game({ id: "700010", name: "Zephyr Drift", display: "" })];

  it("asks before the first launch instead of starting", async () => {
    const { launch } = stub({ status: "ok", games: unanswered });
    await render();
    await click(buttonFor("Zephyr Drift", "Play"));
    expect(text()).toContain("Where should Zephyr Drift open?");
    expect(launch).not.toHaveBeenCalled();
  });

  it("records the screen and then starts the game", async () => {
    const { launch, setDisplay } = stub({ status: "ok", games: unanswered });
    await render();
    await click(buttonFor("Zephyr Drift", "Play"));
    await click(byText("Display 2"));
    // Saved first. The client reads the answer out of games.json when it
    // launches, so a launch that raced the save would open on the old answer.
    // Taken from the fixture rather than written out again: a device name is
    // mostly backslashes, and two copies of it are two chances to escape it
    // differently.
    expect(setDisplay).toHaveBeenCalledWith("700010", SCREENS[1].device);
    expect(launch).toHaveBeenCalledWith("700010");
  });

  it("treats leaving it alone as a real answer", async () => {
    const { launch, setDisplay } = stub({ status: "ok", games: unanswered });
    await render();
    await click(buttonFor("Zephyr Drift", "Play"));
    await click(byText("Wherever it opens"));
    expect(setDisplay).toHaveBeenCalledWith("700010", "default");
    expect(launch).toHaveBeenCalledWith("700010");
  });

  it("marks the screen this window is on", async () => {
    stub({ status: "ok", games: unanswered });
    await render();
    await click(buttonFor("Zephyr Drift", "Play"));
    expect(text()).toContain("LANcast is here");
  });

  it("says what it cannot do before anybody chooses", async () => {
    // The failure is invisible from the inside: the game opens, just not where
    // it was asked to. Naming the two cases is what makes this a limit rather
    // than a bug.
    stub({ status: "ok", games: unanswered });
    await render();
    await click(buttonFor("Zephyr Drift", "Play"));
    expect(text()).toContain("exclusive fullscreen");
    expect(text()).toContain("administrator");
  });

  it("starts nothing when the picker is cancelled", async () => {
    const { launch, setDisplay } = stub({ status: "ok", games: unanswered });
    await render();
    await click(buttonFor("Zephyr Drift", "Play"));
    await click(byText("Cancel"));
    expect(setDisplay).not.toHaveBeenCalled();
    expect(launch).not.toHaveBeenCalled();
    expect(text()).not.toContain("Where should");
  });

  it("does not ask again once a game has an answer", async () => {
    const answered = [
      game({ id: "700011", name: "Answered", display: SCREENS[1].device }),
    ];
    const { launch } = stub({ status: "ok", games: answered });
    await render();
    await click(buttonFor("Answered", "Play"));
    expect(text()).not.toContain("Where should");
    expect(launch).toHaveBeenCalledWith("700011");
  });
});
