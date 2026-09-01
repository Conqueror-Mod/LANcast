/*
 * Resuming a show from the home page.
 *
 * Reported: pressing Resume on a half-watched series played that one episode
 * and stopped — with nothing to distinguish it from a show that had actually
 * ended. The button navigated to /watch/{id} with no queue at all.
 *
 * jsdom cannot see the player, so what is asserted is what the hero *hands*
 * it: the episodes from this one onward, in order.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route, useLocation } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { HomeHero } from "./HomeHero";
import type { Item } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const episode = {
  id: 52,
  library_id: 3,
  kind: "episode",
  title: "The One With The Bug",
  parent_id: 9,
  season: 1,
  episode: 3,
  artwork: {},
} as Item;

const film = {
  id: 70,
  library_id: 1,
  kind: "movie",
  title: "A Film",
  parent_id: null,
  artwork: {},
} as Item;

let host: HTMLDivElement;
let root: Root;
let landed: { path: string; state: unknown } | null;

function Landing() {
  const loc = useLocation();
  landed = { path: loc.pathname, state: loc.state };
  return null;
}

function mount(episodes: number[]) {
  landed = null;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/episodes")) {
        return json({
          episodes: episodes.map((id) => ({
            id,
            kind: "episode",
            title: `E${id}`,
            library_id: 3,
            artwork: {},
          })),
        });
      }
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      return json({});
    }),
  );
}

beforeEach(() => {
  /*
   * jsdom implements no matchMedia, and the hero reads it to honour
   * prefers-reduced-motion. Stubbed rather than worked around in the component:
   * the missing API is the test environment's gap, not the product's.
   */
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    onchange: null,
    dispatchEvent: () => false,
  }));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function render(item: Item) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter initialEntries={["/"]}>
            <Routes>
              <Route path="/" element={<HomeHero item={item} resuming />} />
              <Route path="/watch/:id" element={<Landing />} />
            </Routes>
          </MemoryRouter>
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

async function pressResume() {
  const btn = [...host.querySelectorAll("button")].find(
    (b) => b.textContent?.includes("Resume") || b.textContent?.includes("Play"),
  );
  expect(btn, "no Resume button").toBeTruthy();
  await act(async () => {
    btn!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
  for (let i = 0; i < 5; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

describe("resuming from the home page", () => {
  // The reported fault.
  it("queues the rest of the show, not just the episode", async () => {
    mount([50, 51, 52, 53, 54]);
    await render(episode);
    await pressResume();

    expect(landed?.path).toBe("/watch/52");
    const queue = (landed?.state as { queue?: number[] })?.queue;
    expect(queue, "no queue was handed over — the show would end here").toBeTruthy();
    expect(queue).toEqual([52, 53, 54]);
  });

  /*
   * From here onward, not the whole show.
   *
   * Continue means "carry on", so queueing the earlier episodes would make the
   * previous button walk back through ones already finished — which is what
   * Play from the top is for.
   */
  it("does not queue episodes already behind the viewer", async () => {
    mount([50, 51, 52, 53, 54]);
    await render(episode);
    await pressResume();
    const queue = (landed?.state as { queue?: number[] })?.queue ?? [];
    expect(queue).not.toContain(50);
    expect(queue).not.toContain(51);
  });

  // A film has no episodes to fetch and must not be made to wait for a lookup
  // that cannot help it.
  it("plays a film without a queue", async () => {
    mount([]);
    await render(film);
    await pressResume();
    expect(landed?.path).toBe("/watch/70");
    expect((landed?.state as { queue?: number[] })?.queue).toBeUndefined();
  });

  /*
   * A failed lookup still plays something.
   *
   * One episode is a worse outcome than a whole show and a far better one than
   * a button that does nothing.
   */
  it("still plays when the episode list cannot be fetched", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("nope", { status: 500 })),
    );
    await render(episode);
    await pressResume();
    expect(landed?.path).toBe("/watch/52");
  });
});
