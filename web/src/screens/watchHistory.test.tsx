/*
 * The watch history (ADR 0074): every finished viewing, grouped by month, with
 * the two downloads. Since Profile 2.0 step 5 it is History's "Every time",
 * and its old address redirects there.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { ViewingLog, WatchHistory } from "./WatchHistory";
import type { HistoryKind } from "@/api/hooks";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

// Noon on the 15th, so no timezone moves a row into another month.
const at = (y: number, m: number) => Math.floor(new Date(y, m, 15, 12).getTime() / 1000);
const row = (id: number, extra: object) => ({
  id,
  estimated: false,
  item_id: id,
  kind: "movie",
  title: "Heat",
  year: 1995,
  series: null,
  season: null,
  episode: null,
  imdb_id: null,
  show_year: null,
  show_imdb_id: null,
  finished_at: at(2026, 8),
  ...extra,
});

let viewings: unknown[];
let urls: string[];
let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  viewings = [
    row(3, { title: "Heat" }),
    row(2, { title: "Heat", finished_at: at(2026, 7) }),
    row(1, {
      kind: "episode",
      title: "Space Pilot 3000",
      series: "Futurama",
      season: 1,
      episode: 1,
      item_id: null,
      estimated: true,
      finished_at: at(2026, 7),
    }),
  ];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  urls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string) => {
      urls.push(String(input));
      return new Response(JSON.stringify({ viewings, total: viewings.length }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function render(kind: HistoryKind = "") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter>
            <ViewingLog kind={kind} />
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await act(async () => {
    await new Promise((r) => setTimeout(r, 5));
  });
}

describe("watch history", () => {
  it("lists a film once per viewing, grouped by month", async () => {
    await render();
    const months = [...host.querySelectorAll(".profile__section")];
    expect(months).toHaveLength(2);
    expect(months[0].querySelectorAll(".profile__row")).toHaveLength(1);
    expect(months[1].querySelectorAll(".profile__row")).toHaveLength(2);
    expect(host.textContent?.match(/Heat/g)).toHaveLength(2);
  });

  it("says when a row outlived its item or has an approximate date", async () => {
    await render();
    const ep = [...host.querySelectorAll(".profile__row")].find((r) =>
      r.textContent?.includes("Space Pilot 3000"),
    )!;
    expect(ep.tagName).toBe("DIV");
    expect(ep.textContent).toContain("removed");
    expect(ep.textContent).toContain("approximate");
    expect(ep.textContent).toContain("Futurama · S01E01");
  });

  it("says a finished book or comic is one, so it does not read as a film", async () => {
    viewings = [
      row(5, { kind: "book", title: "Dune", year: 1965 }),
      row(4, { kind: "comic", title: "Saga 01", year: null }),
    ];
    await render();
    const rows = [...host.querySelectorAll(".profile__row")].map((r) => r.textContent ?? "");
    expect(rows.find((t) => t.includes("Dune"))).toContain("Audiobook · 1965");
    expect(rows.find((t) => t.includes("Saga 01"))).toContain("Comic");
  });

  it("offers both downloads", async () => {
    await render();
    const hrefs = [...host.querySelectorAll<HTMLAnchorElement>(".profile__exportlinks a")].map((a) =>
      a.getAttribute("href"),
    );
    expect(hrefs).toEqual([
      "/api/profile/viewings/export?format=csv",
      "/api/profile/viewings/export?format=trakt",
    ]);
  });

  it("explains an empty history and offers no download", async () => {
    viewings = [];
    await render();
    expect(host.textContent).toContain("Nothing here yet");
    expect(host.querySelector(".profile__exportlinks")).toBeNull();
  });

  it("asks the server for one kind when filtered", async () => {
    await render("comics");
    expect(urls.some((u) => u.includes("kind=comics"))).toBe(true);
  });

  it("explains that songs are not logged instead of an empty list", async () => {
    await render("music");
    expect(host.textContent).toContain("Songs are not kept in the log");
    expect(host.querySelector(".profile__row")).toBeNull();
  });

  it("sends the old address to History's Every time", async () => {
    let at = "";
    function Where() {
      at = useLocation().pathname + useLocation().search;
      return null;
    }
    const client = new QueryClient();
    await act(async () => {
      root.render(
        <QueryClientProvider client={client}>
          <MemoryRouter initialEntries={["/profile/viewings"]}>
            <Routes>
              <Route path="/profile/viewings" element={<WatchHistory />} />
              <Route path="/profile" element={<Where />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      );
    });
    expect(at).toBe("/profile?tab=history&every=1");
  });
});
