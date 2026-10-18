/*
 * Rate what you've watched: stars under each tile, a click is the rating, and
 * the shelf asks again so the title leaves. The refetch is the part that is
 * easy to lose: the rating would save and the tile would stay, which reads as
 * the click not working.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { Shelf } from "./Shelf";
import { QuickRate } from "./QuickRate";
import { useToRate } from "@/api/hooks";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let pending: { id: number; title: string; kind: string }[];
let puts: { url: string; body: unknown }[];

function RateShelf() {
  const { data } = useToRate();
  return (
    <Shelf
      title="Rate what you've watched"
      items={(data?.items ?? []) as never}
      itemFooter={(item) => <QuickRate item={item} />}
    />
  );
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  pending = [
    { id: 5, title: "Saw VI", kind: "movie" },
    { id: 9, title: "A Show", kind: "show" },
  ];
  puts = [];
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.includes("/rating") && init?.method === "PUT") {
        const body = JSON.parse(init.body as string);
        puts.push({ url, body });
        const id = Number(url.match(/items\/(\d+)/)![1]);
        pending = pending.filter((p) => p.id !== id);
        return new Response(null, { status: 204 });
      }
      if (url.includes("/api/profile/ratings/pending")) return json({ items: pending });
      return json({});
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function settle() {
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

describe("Rate what you've watched", () => {
  it("rates from the shelf and the title leaves", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    await act(async () => {
      root.render(
        <QueryClientProvider client={client}>
          <MemoryRouter>
            <FocusProvider>
              <RateShelf />
            </FocusProvider>
          </MemoryRouter>
        </QueryClientProvider>,
      );
    });
    await settle();
    expect(host.textContent).toContain("Rate what you've watched");
    const group = host.querySelector('[aria-label="Rate Saw VI"]')!;
    expect(group).toBeTruthy();

    const fourStars = group.querySelector('[aria-label="4 stars"]') as HTMLButtonElement;
    await act(async () => fourStars.click());
    await settle();

    expect(puts).toEqual([{ url: "/api/items/5/rating", body: { score: 8 } }]);
    expect(host.querySelector('[aria-label="Rate Saw VI"]')).toBeNull();
    expect(host.querySelector('[aria-label="Rate A Show"]')).toBeTruthy();
  });

  it("is no shelf at all when there is nothing to rate", async () => {
    pending = [];
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    await act(async () => {
      root.render(
        <QueryClientProvider client={client}>
          <MemoryRouter>
            <FocusProvider>
              <RateShelf />
            </FocusProvider>
          </MemoryRouter>
        </QueryClientProvider>,
      );
    });
    await settle();
    expect(host.textContent).not.toContain("Rate what you've watched");
  });
});
