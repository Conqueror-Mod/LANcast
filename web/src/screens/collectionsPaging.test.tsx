/*
 * The collections page pages.
 *
 * It did not, and nothing said so. `useInfiniteItems` was called for its first
 * page and `fetchNextPage` was never wired, so the page rendered 120 tiles and
 * stopped — on a real library with 170 collections, every one after roughly "H"
 * was unreachable, and the count read "120" because it was `items.length`.
 *
 * A silent truncation is the worst shape a listing bug takes: the page looks
 * complete. So both halves are asserted here — that a second page is requested,
 * and that the header reports the server's total rather than what happens to be
 * loaded.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { Collections } from "./Collections";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const TOTAL = 170;
const PAGE = 120;

function pageOf(offset: number) {
  const items = [];
  for (let i = offset; i < Math.min(offset + PAGE, TOTAL); i++) {
    items.push({
      id: 1000 + i,
      library_id: 3,
      kind: "collection",
      title: `Collection ${String(i).padStart(3, "0")}`,
      year: null,
      child_count: 2,
    });
  }
  return { items, total: TOTAL };
}

let host: HTMLDivElement;
let root: Root;
let requests: string[];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  requests = [];

  // jsdom has no IntersectionObserver. The real hook deliberately does not rely
  // on one — observer callbacks are suppressed in a throttled tab — so a stub
  // that never fires leaves the scroll and immediate paths, which are the ones
  // that must work.
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      observe() {}
      disconnect() {}
      unobserve() {}
    },
  );

  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      requests.push(url);
      const body = url.includes("/api/libraries")
        ? [{ id: 3, name: "Movies", kind: "movie" }]
        : pageOf(Number(new URL(url, "http://x").searchParams.get("offset") ?? 0));
      return new Response(JSON.stringify(body), {
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

async function render() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={["/library/3/collections"]}>
          <FocusProvider>
            <Routes>
              <Route
                path="/library/:id/collections"
                element={<Collections />}
              />
              <Route path="/item/:id" element={<div id="detail" />} />
            </Routes>
          </FocusProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await flush();
}

async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

function itemRequests() {
  return requests.filter((u) => u.includes("/api/items"));
}

describe("collections paging", () => {
  it("asks for a second page rather than stopping at the first", async () => {
    await render();
    // The sentinel starts inside the viewport in jsdom (zero-height layout), so
    // the hook's immediate check fires without a scroll — which is exactly the
    // case it exists for: a first page too short to fill the screen.
    await flush();

    const offsets = itemRequests().map(
      (u) => new URL(u, "http://x").searchParams.get("offset") ?? "0",
    );
    expect(offsets).toContain("120");
  });

  it("reports the server's total, not the number loaded", async () => {
    await render();
    const count = host.querySelector(".browse__count")?.textContent ?? "";
    // Either "120 of 170" while the rest loads, or "170" once it has — never a
    // bare "120", which is the paging bug wearing a number.
    expect(count).toMatch(/170/);
  });

  it("asks the server only for collections", async () => {
    await render();
    expect(itemRequests()[0]).toContain("kind=collection");
  });
});
