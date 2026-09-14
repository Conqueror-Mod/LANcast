/*
 * Your year, on the profile page.
 *
 * jsdom performs no layout, so nothing here proves the chart looks like a
 * chart. What it can prove is the part that actually matters on a page like
 * this: that the qualifiers are on screen rather than in a comment, that a
 * quiet month is still drawn, and that a year holding one title does not show
 * that title twice as though the two ends of the year were different films.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { YearInReview } from "./YearInReview";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let asked: string[];

const arrival = { id: 41, title: "Arrival", artwork: { poster: "abc" } };
const dune = { id: 88, title: "Dune", artwork: { poster: "def" } };

function months(filled: Record<number, number> = {}) {
  return Array.from({ length: 12 }, (_, i) => ({
    month: i + 1,
    titles: filled[i + 1] ?? 0,
  }));
}

function stub(over: Record<string, unknown> = {}) {
  asked = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      asked.push(String(url));
      const body = {
        year: 2025,
        titles: 12,
        finished: 9,
        abandoned: 3,
        watched_ms: 43_200_000,
        libraries: 2,
        months: months({ 3: 4, 11: 8 }),
        kinds: [
          { kind: "movie", titles: 9 },
          { kind: "episode", titles: 3 },
        ],
        first: arrival,
        last: dune,
        years: [2025, 2024],
        partial: false,
        ...over,
      };
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
}

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <YearInReview />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await settle();
}

// React Query notifies on a macrotask, so a resolved promise is not enough.
async function settle() {
  for (let i = 0; i < 5; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
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

describe("your year", () => {
  it("says what it does not claim, on screen", async () => {
    /*
     * The load-bearing assertion. A page like this is believed, and every
     * number on it is smaller than a marketing version would want — so the
     * reason has to be readable rather than buried in a tooltip or a comment.
     */
    stub();
    await render();
    expect(text()).toContain("one viewing of each counted");
    expect(text()).toContain("Nothing is sent anywhere");
    expect(text()).toMatch(/last.*played/i);
  });

  it("draws every month, including the quiet ones", async () => {
    // A chart with the empty months missing is a chart that lies about the
    // shape of the year, which is the one thing it is really for.
    stub();
    await render();
    expect(host.querySelectorAll(".year__month")).toHaveLength(12);
    expect(text()).toContain("Jan");
    expect(text()).toContain("Dec");
  });

  it("scales the bars against the busiest month", async () => {
    stub();
    await render();
    const bars = [...host.querySelectorAll<HTMLElement>(".year__bar")];
    // November is the busiest at 8, March is 4, and the rest are empty.
    expect(bars[10].style.height).toBe("100%");
    expect(bars[2].style.height).toBe("50%");
    expect(bars[0].style.height).toBe("0%");
  });

  it("shows one title once, not as both ends of its year", async () => {
    // A small year is not a bug, but a film listed as both the first and last
    // thing watched reads as one.
    stub({ titles: 1, finished: 1, abandoned: 0, first: arrival, last: arrival });
    await render();
    const ends = host.querySelectorAll(".year__end");
    expect(ends).toHaveLength(1);
  });

  it("says a running year is not finished", async () => {
    // "Your 2025" and "your 2025 so far" are a summary and a claim.
    stub({ partial: true });
    await render();
    expect(text()).toContain("2025 so far");
  });

  it("offers no picker when there is nothing to pick", async () => {
    // One year of history is not a picker, it is a label with an arrow on it.
    stub({ years: [2025] });
    await render();
    expect(host.querySelector('select[aria-label="Year"]')).toBeNull();
  });

  it("asks the server for the year that was chosen", async () => {
    stub();
    await render();
    // The first request names no year: the default is the server's, since it
    // owns the calendar the history is bucketed on.
    expect(asked[0]).toBe("/api/profile/year");

    const pick = host.querySelector<HTMLSelectElement>('select[aria-label="Year"]')!;
    await act(async () => {
      pick.value = "2024";
      pick.dispatchEvent(new Event("change", { bubbles: true }));
    });
    await settle();
    expect(asked).toContain("/api/profile/year?year=2024");
  });

  it("is honest about a year with nothing in it", async () => {
    stub({ titles: 0, finished: 0, abandoned: 0, watched_ms: 0, months: months(), first: undefined, last: undefined });
    await render();
    expect(text()).toContain("Nothing played in 2025");
    expect(host.querySelector(".year__chart")).toBeNull();
  });
});
