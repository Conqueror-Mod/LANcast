/*
 * The watch history page (ADR 0074): every finished viewing, grouped by month,
 * with the two downloads.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { WatchHistory } from "./WatchHistory";

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
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      new Response(JSON.stringify({ viewings, total: viewings.length }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ),
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
          <MemoryRouter>
            <WatchHistory />
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
});
