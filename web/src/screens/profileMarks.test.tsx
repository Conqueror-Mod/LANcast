/*
 * The rest of the profile page: your favourites, your ratings and your tags.
 *
 * The server kept all three and the page showed none of them. GET
 * /api/profile/ratings was documented and specified and nothing ever called
 * it; favourites and tags were visible only one library at a time, as grid
 * filters. Each is private to the account (ADR 0062), so this page is where
 * they belong.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { Profile } from "./Profile";
import { Favourites, TagItems } from "./Marked";

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

let favourites: unknown[];
let ratings: unknown[];
let tags: unknown[];
let gets: string[];
let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  favourites = [item(1, "Heat"), item(2, "Black", "track", { series: "Ten" })];
  ratings = [
    {
      item: item(3, "Alien", "movie", { year: 1979 }),
      rating: { item_id: 3, score: 9, review: "Still the best.", updated_at: 1_700_000_000 },
    },
  ];
  tags = [{ id: 7, name: "Christmas", count: 4 }];
  gets = [];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string) => {
      const url = String(input);
      gets.push(url);
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/auth/status"))
        return json({ authenticated: true, configured: true, user: { id: "u1", name: "chris", role: "admin" } });
      if (url.includes("/api/profile/ratings")) return json({ ratings });
      if (url.includes("/api/tags")) return json({ tags });
      if (url.includes("/api/items") && url.includes("favourite=1"))
        return json({ items: favourites, total: favourites.length });
      if (url.includes("/api/items") && url.includes("tag=7"))
        return json({ items: [item(9, "Elf")], total: 1 });
      if (url.includes("/api/profile/year")) return json({});
      if (url.startsWith("/api/profile"))
        return json({
          user: { name: "chris", admin: true, secured: true },
          stats: { started: 1, finished: 1, watched_ms: 0, first_at: null },
          history: [],
          total: 0,
        });
      return json({});
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function render(path: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={[path]}>
              <Routes>
                <Route path="/profile" element={<Profile />} />
                <Route path="/profile/favourites" element={<Favourites />} />
                <Route path="/profile/tags/:id" element={<TagItems />} />
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

const section = (label: string) =>
  [...host.querySelectorAll("section, .shelf")].find(
    (s) => s.querySelector(".section-label")?.textContent?.trim() === label,
  );

describe("the profile's private sections", () => {
  it("shows a shelf of favourites from every library, with the whole list a click away", async () => {
    await render("/profile");
    const shelf = section("Favourites");
    expect(shelf?.textContent).toContain("Heat");
    expect(shelf?.textContent).toContain("Black");
    // Across libraries: no library_id in the ask.
    const ask = gets.find((u) => u.includes("favourite=1"))!;
    expect(ask).not.toContain("library_id");
    expect(shelf?.querySelector('a[href="/profile/favourites"]')).toBeTruthy();
  });

  it("lists your ratings with the score out of ten and the review", async () => {
    await render("/profile");
    const s = section("Your ratings");
    expect(s?.textContent).toContain("Alien");
    expect(s?.querySelector(".profile__score")?.getAttribute("aria-label")).toBe("9 out of 10");
    expect(s?.textContent).toContain("Still the best.");
    expect(gets.some((u) => u.includes("/api/profile/ratings?limit=10"))).toBe(true);
  });

  it("offers every rating once the first page is full", async () => {
    ratings = Array.from({ length: 10 }, (_, i) => ({
      item: item(100 + i, `Film ${i}`),
      rating: { item_id: 100 + i, score: 7, updated_at: 1 },
    }));
    await render("/profile");
    const more = [...host.querySelectorAll("button")].find((b) => b.textContent === "Show all ratings");
    expect(more).toBeTruthy();
    await act(async () => more!.click());
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
    expect(gets.some((u) => u.includes("/api/profile/ratings?limit=200"))).toBe(true);
  });

  it("lists your tags, each opening what carries it", async () => {
    await render("/profile");
    const link = section("Your tags")?.querySelector<HTMLAnchorElement>("a.profile__tag");
    expect(link?.textContent).toBe("Christmas4");
    expect(link?.getAttribute("href")).toBe("/profile/tags/7");
  });

  // An absent section is also what a broken one looks like.
  it("says how to start each one when there is nothing yet", async () => {
    favourites = [];
    ratings = [];
    tags = [];
    await render("/profile");
    expect(section("Favourites")?.textContent).toContain("Nothing favourited yet");
    expect(section("Your ratings")?.textContent).toContain("Nothing rated yet");
    expect(section("Your tags")?.textContent).toContain("No tags yet");
  });
});

describe("the lists they open", () => {
  it("opens every favourite as a grid", async () => {
    await render("/profile/favourites");
    expect(host.querySelector(".browse__title")?.textContent).toBe("Favourites");
    expect(host.querySelectorAll(".browse__grid > *").length).toBe(2);
  });

  it("opens a tag by its name, across libraries", async () => {
    await render("/profile/tags/7");
    expect(host.querySelector(".browse__title")?.textContent).toBe("Christmas");
    expect(host.textContent).toContain("Elf");
    const ask = gets.find((u) => u.includes("tag=7"))!;
    expect(ask).not.toContain("library_id");
  });
});
