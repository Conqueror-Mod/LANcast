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
import { MemoryRouter, Route, Routes } from "react-router-dom";
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
let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string) => {
      const url = String(input);
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/auth/status"))
        return json({ authenticated: true, configured: true, user: { id: "u1", name: "dana", role: "admin" } });
      if (url.includes("/api/profile/year")) return json({});
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

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={["/profile"]}>
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
