/*
 * The overview's totals by kind and Most played (profile-2-plan §4c, step 4).
 *
 * "Time watched" counted songs. These pin that a server which splits its totals
 * gets a card per group, that one which does not still gets the old cards, and
 * that Most played keeps films and songs in separate lists.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { Profile } from "./Profile";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const item = (id: number, title: string, kind = "movie", extra: object = {}) => ({
  id,
  title,
  kind,
  library_id: 1,
  artwork: {},
  ...extra,
});

let profile: Record<string, unknown>;
let kinds: Record<string, unknown> | null;
let urls: string[];
let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  kinds = null;
  urls = [];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string) => {
      const url = String(input);
      urls.push(url);
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/auth/status"))
        return json({ authenticated: true, configured: true, user: { id: "u1", name: "dana", role: "admin" } });
      if (url.includes("/api/profile/year")) return json({});
      if (url.includes("/api/profile/kinds"))
        return kinds ? json(kinds) : new Response("", { status: 404 });
      if (url.includes("/api/profile/viewings")) return json({ viewings: [], total: 0 });
      if (url.startsWith("/api/profile?") || url === "/api/profile") return json(profile);
      return json({});
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function render(path = "/profile") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={[path]}>
              <Routes>
                <Route path="/profile" element={<Profile />} />
              </Routes>
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

const labels = () =>
  [...host.querySelectorAll(".profile__statlabel")].map((e) => e.textContent);

const base = {
  user: { name: "dana", admin: true, secured: true },
  history: [],
  has_more: false,
};

describe("profile totals by kind", () => {
  it("gives watching and listening their own cards", async () => {
    profile = {
      ...base,
      stats: {
        started: 3, finished: 3, viewings: 5, watched_ms: 8_000_000, first_at: 1,
        by_kind: [
          { kind: "movie", started: 1, finished: 1, viewings: 1, watched_ms: 7_200_000 },
          { kind: "track", started: 2, finished: 2, viewings: 4, watched_ms: 800_000 },
        ],
      },
      most_played: [],
    };
    await render();
    expect(labels()).toEqual(["Watched", "Listened"]);
    expect(host.textContent).not.toContain("Time watched");
    expect(host.textContent).toContain("1 film finished");
    expect(host.textContent).toContain("2 songs played");
  });

  it("keeps the old cards for a server that does not split", async () => {
    profile = {
      ...base,
      stats: { started: 4, finished: 2, viewings: 2, watched_ms: 60_000, first_at: 1 },
    };
    await render();
    expect(labels()).toEqual(["Started", "Finished", "Time watched"]);
    expect(host.querySelector(".profile__mostplayed")).toBeNull();
  });

  it("lists Most played in groups, films before songs", async () => {
    profile = {
      ...base,
      stats: { started: 0, finished: 0, viewings: 0, watched_ms: 0, first_at: null, by_kind: [] },
      most_played: [
        { item: item(1, "Heat"), plays: 3 },
        { item: item(2, "Black", "track", { series: "Ten" }), plays: 9 },
      ],
    };
    await render();
    const groups = [...host.querySelectorAll(".profile__grouplabel")].map((e) => e.textContent);
    expect(groups).toEqual(["Films & shows", "Music"]);
    const rows = [...host.querySelectorAll(".profile__mostplayed .profile__row")];
    expect(rows.map((r) => r.getAttribute("href"))).toEqual(["/item/1", "/item/2"]);
    expect(rows[1].textContent).toContain("9 plays");
    expect(rows[1].textContent).toContain("Ten");
  });
});

const click = async (label: string) => {
  const b = [...host.querySelectorAll<HTMLButtonElement>(".chip")].find((c) => c.textContent === label)!;
  await act(async () => {
    b.click();
    await new Promise((r) => setTimeout(r, 10));
  });
};

describe("one History (profile-2-plan §4e)", () => {
  beforeEach(() => {
    profile = {
      ...base,
      stats: { started: 0, finished: 0, viewings: 0, watched_ms: 0, first_at: null, by_kind: [] },
      most_played: [],
    };
  });

  it("asks for one kind when a filter is pressed", async () => {
    await render("/profile?tab=history");
    await click("Video");
    expect(urls.some((u) => u.startsWith("/api/profile?") && u.includes("kind=video"))).toBe(true);
    const on = [...host.querySelectorAll(".chip.is-on")].map((c) => c.textContent);
    expect(on).toEqual(["Video", "Each title once"]);
  });

  it("switches to the log, keeping the filter", async () => {
    await render("/profile?tab=history&kind=books");
    await click("Every time");
    expect(urls.some((u) => u.includes("/api/profile/viewings") && u.includes("kind=books"))).toBe(true);
    expect(host.textContent).toContain("each time you finished it");
  });
});

describe("books, comics and games on Overview (profile-2-plan §4d)", () => {
  beforeEach(() => {
    profile = {
      ...base,
      stats: { started: 0, finished: 0, viewings: 0, watched_ms: 0, first_at: null, by_kind: [] },
      most_played: [],
    };
  });

  it("shows only the kinds that have something, with where you are", async () => {
    kinds = {
      books: { in_progress: [], finished: [] },
      comics: {
        in_progress: [
          { item: item(7, "Saga 01", "comic"), touched_at: 1, page: { page: 3, pages: 20, updated_at: 1 } },
        ],
        finished: [{ item: item(8, "Saga 02", "comic"), touched_at: 1 }],
      },
      games: [{ item: item(9, "Tetris", "rom"), saved_at: 1_700_000_000 }],
    };
    await render();
    const labels = [...host.querySelectorAll(".profile__kind .section-label")].map((e) => e.textContent);
    expect(labels).toEqual(["Comics", "Games"]);
    const rows = [...host.querySelectorAll(".profile__kind .profile__row")].map((r) => r.textContent ?? "");
    expect(rows[0]).toContain("Page 4 of 20");
    expect(rows[1]).toContain("Read");
    expect(rows[2]).toContain("Saved");
  });

  it("shows nothing on a server without the route", async () => {
    kinds = null;
    await render();
    expect(host.querySelector(".profile__kind")).toBeNull();
  });
});
