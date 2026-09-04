/*
 * The "On this day" shelf.
 *
 * Two properties, and the second is the one worth a test.
 *
 * It shows what the server sent. And on the days it sends nothing — which is
 * most days — the shelf is *absent* rather than an empty heading. A heading
 * over no tiles is the shape of something broken, and this one would be broken
 * on screen far more often than it was whole.
 *
 * The date is deliberately not asserted here. The server decides what day it
 * is, and a client test that computed one to check against would be doing the
 * exact thing this feature exists to keep out of the client: `toISOString()` is
 * UTC, so through a US evening it resolves to tomorrow. Which photographs
 * belong to today is proved in internal/store/memories_test.go, against the
 * clock the query actually uses.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { Home } from "./Home";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const memory = {
  id: 5512,
  title: "A Holiday Years Ago",
  kind: "photo",
  library_id: 3,
  artwork: {},
};

let host: HTMLDivElement;
let root: Root;
let asked: string[];

function mount(memories: unknown[]) {
  asked = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (method !== "GET") return new Response(null, { status: 204 });
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      if (url.includes("/api/memories")) {
        asked.push(url);
        return json({ items: memories, on: "09-04" });
      }
      if (url.includes("/api/libraries")) return json([]);
      if (url.includes("/api/continue")) return json({ items: [] });
      return json({ items: [], total: 0 });
    }),
  );
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

async function render() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={["/"]}>
              <Home />
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

describe("On this day", () => {
  it("shows what the server sent", async () => {
    mount([memory]);
    await render();
    expect(host.textContent).toContain("On this day");
    expect(host.textContent).toContain("A Holiday Years Ago");
  });

  it("is absent entirely on a day with nothing in it", async () => {
    mount([]);
    await render();
    expect(
      host.textContent,
      "an empty shelf is a heading over nothing, which reads as broken rather " +
        "than as a quiet day",
    ).not.toContain("On this day");
  });

  it("asks the server rather than working the date out itself", async () => {
    mount([memory]);
    await render();
    // No date in the request: the day is the server's to decide, and a client
    // that put one in the query string would be back to computing calendar
    // dates in UTC.
    expect(asked.length).toBeGreaterThan(0);
    expect(asked.some((u) => /\d{2}-\d{2}/.test(u))).toBe(false);
  });
});
