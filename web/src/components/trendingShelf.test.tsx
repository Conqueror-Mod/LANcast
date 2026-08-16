/*
 * The shelf's title is the claim, and it is the part that can be wrong.
 *
 * `viewers` counts accounts rather than plays, so on a single-account server
 * every entry is 1 and the row is that one person's recent activity. Calling
 * that "Trending" would be a small lie told on the home page every day, which
 * is the kind that survives longest because nobody can be bothered to argue
 * with it.
 *
 * So the server reports how many accounts contributed and the row names itself
 * from that. This asserts the naming, in both directions, plus the rule that an
 * empty result renders nothing at all — a heading over no tiles is the shape of
 * something broken.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { TrendingShelf } from "./TrendingShelf";
import type { Item, Library, Trending } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}

const library: Library = {
  id: 1,
  name: "Films",
  kind: "movie",
  path: "/media/films",
  created_at: 0,
  scanned_at: 0,
  item_count: 40,
};

const item = (id: number, title: string): Item =>
  ({ id, title, kind: "movie" }) as Item;

let container: HTMLDivElement;
let root: Root;

function answer(body: Trending) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
}

async function render() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <FocusProvider>
            <TrendingShelf library={library} />
          </FocusProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  // Let the query resolve and commit. A single microtask is not enough — the
  // fetch promise, react-query's state update and the re-render are three
  // separate turns — so this waits for the shelf to appear rather than
  // guessing how many ticks that takes.
  for (let i = 0; i < 20 && container.textContent === ""; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
}

beforeEach(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  vi.unstubAllGlobals();
});

describe("the trending shelf names itself honestly", () => {
  it("says Recently Played when one account contributed", async () => {
    answer({
      items: [{ item: item(1, "Arrival"), viewers: 1, finishers: 0, last_at: 0 }],
      contributors: 1,
      window_days: 30,
    });
    await render();
    expect(container.textContent).toContain("Recently Played in Films");
    expect(container.textContent).not.toContain("Trending in Films");
  });

  it("says Trending once more than one account has played something", async () => {
    answer({
      items: [{ item: item(1, "Arrival"), viewers: 2, finishers: 1, last_at: 0 }],
      contributors: 2,
      window_days: 30,
    });
    await render();
    expect(container.textContent).toContain("Trending in Films");
  });

  it("renders nothing at all when there is no activity", async () => {
    answer({ items: [], contributors: 0, window_days: 30 });
    await render();
    expect(container.textContent).toBe("");
  });
});
