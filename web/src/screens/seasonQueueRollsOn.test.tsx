import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  MemoryRouter,
  Routes,
  Route,
  useLocation,
  useParams,
} from "react-router";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { Detail } from "./Detail";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

/*
 * A season page's episode hands over the rest of the show, not the season.
 *
 * It handed over the season alone, so a finale played and the night stopped:
 * no Up Next card and nothing for auto play to roll on to. Ash vs Evil Dead's
 * first finale was found that way (2026-11-22). Asserts the queue, as
 * continueShowQueue does, because the destination was always right.
 */

const season = {
  id: 910,
  title: "Season 1",
  kind: "season",
  library_id: 1,
  parent_id: 900,
  child_count: 2,
  artwork: {},
};

function ep(id: number, s: number, n: number) {
  return {
    id,
    title: `Episode ${n}`,
    kind: "episode",
    library_id: 1,
    series: "Ash",
    season: s,
    episode: n,
    duration_ms: 1_800_000,
    artwork: {},
  };
}

const seasonOne = [ep(101, 1, 1), ep(102, 1, 2)];
const whole = [...seasonOne, ep(201, 2, 1), ep(202, 2, 2)];

let host: HTMLDivElement;
let root: Root;
let landed: { path: string; state: unknown } | null;
let showList: unknown[];

function mount() {
  landed = null;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/items/900/episodes")) return json({ episodes: showList });
      if (url.includes("parent_id=")) return json({ items: seasonOne, total: seasonOne.length });
      if (url.includes("/api/items/910")) return json(season);
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "dana", role: "admin" },
        });
      }
      return json({ items: [], total: 0 });
    }),
  );
}

function FakePlayer() {
  const loc = useLocation();
  const params = useParams();
  landed = { path: params.id as string, state: loc.state };
  return <div>player</div>;
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  showList = whole;
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={["/item/910"]}>
              <Routes>
                <Route path="/item/:id" element={<Detail />} />
                <Route path="/watch/:id" element={<FakePlayer />} />
              </Routes>
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

async function pressEpisode(n: number) {
  const row = [...host.querySelectorAll<HTMLButtonElement>(".eprow__play")].find((b) =>
    b.getAttribute("aria-label")?.startsWith(`Play episode ${n},`),
  );
  expect(row).toBeDefined();
  await act(async () => row!.click());
}

describe("a season page's queue", () => {
  it("runs on from the finale into the next season", async () => {
    mount();
    await render();
    await pressEpisode(2);
    expect(landed?.path).toBe("102");
    expect((landed?.state as { queue: number[] }).queue).toEqual([101, 102, 201, 202]);
  });

  it("is the season alone when the show's list leaves out an episode on the page", async () => {
    showList = [seasonOne[0], whole[2], whole[3]];
    mount();
    await render();
    await pressEpisode(2);
    expect((landed?.state as { queue: number[] }).queue).toEqual([101, 102]);
  });
});
