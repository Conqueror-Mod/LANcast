/*
 * The comic reader (ADR 0101): it opens where you left off, turns pages from
 * the keyboard and the screen's sides, keeps your place, and opens a finished
 * comic at the start.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { Reader } from "./Reader";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let place: { page: number; pages: number; updated_at: number } | null;
let puts: number[];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  place = null;
  puts = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (url.endsWith("/api/items/7/place") && init?.method === "PUT") {
        puts.push(JSON.parse(init.body as string).page);
        return new Response(null, { status: 204 });
      }
      if (url.endsWith("/api/items/7/pages")) return json({ pages: 5, place });
      if (url.endsWith("/api/items/8/pages")) {
        return json({ error: { code: "unavailable", message: "cannot be opened" } }, 503);
      }
      if (url.endsWith("/api/items/7")) return json({ id: 7, kind: "comic", title: "Saga 001" });
      return json({});
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function settle(ms = 5) {
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, ms));
    });
  }
}

async function open(path = "/read/7") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={["/item/7", path]} initialIndex={1}>
          <Routes>
            <Route path="/read/:id" element={<Reader />} />
            <Route path="/item/:id" element={<div className="detail-stub">details</div>} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await settle();
}

const shown = () => host.querySelector("img.reader__page")?.getAttribute("src");
const key = (k: string) =>
  act(() => {
    window.dispatchEvent(new KeyboardEvent("keydown", { key: k }));
  });

describe("the comic reader", () => {
  it("opens at the start of a comic never opened, and turns with the keys", async () => {
    await open();
    expect(shown()).toBe("/api/items/7/pages/0");
    expect(host.textContent).toContain("1 / 5");
    await key("ArrowRight");
    await key(" ");
    expect(shown()).toBe("/api/items/7/pages/2");
    await key("ArrowLeft");
    expect(shown()).toBe("/api/items/7/pages/1");
    await key("End");
    expect(shown()).toBe("/api/items/7/pages/4");
    await key("ArrowRight");
    expect(shown()).toBe("/api/items/7/pages/4");
    await key("Home");
    await key("ArrowLeft");
    expect(shown()).toBe("/api/items/7/pages/0");
  });

  it("opens where you left off, and keeps the new place once the page settles", async () => {
    place = { page: 2, pages: 5, updated_at: 1 };
    await open();
    expect(shown()).toBe("/api/items/7/pages/2");
    await key("ArrowRight");
    await key("ArrowRight");
    expect(puts).toEqual([]);
    await settle(200);
    expect(puts).toEqual([4]);
  });

  it("opens a finished comic at the start", async () => {
    place = { page: 4, pages: 5, updated_at: 1 };
    await open();
    expect(shown()).toBe("/api/items/7/pages/0");
  });

  it("starts over when asked to, wherever the place is", async () => {
    place = { page: 3, pages: 5, updated_at: 1 };
    await open("/read/7?start=1");
    expect(shown()).toBe("/api/items/7/pages/0");
  });

  it("turns back on a tap on the left third and forward on the right", async () => {
    await open();
    const stage = host.querySelector(".reader__stage") as HTMLElement;
    Object.defineProperty(stage, "clientWidth", { value: 900 });
    const tap = (x: number) =>
      act(() => {
        stage.dispatchEvent(new MouseEvent("pointerdown", { bubbles: true, clientX: x, clientY: 100 }));
        stage.dispatchEvent(new MouseEvent("pointerup", { bubbles: true, clientX: x, clientY: 100 }));
      });
    await tap(800);
    await tap(800);
    expect(shown()).toBe("/api/items/7/pages/2");
    await tap(100);
    expect(shown()).toBe("/api/items/7/pages/1");
    // The middle shows and hides the bar.
    await tap(450);
    expect(host.querySelector(".reader--bare")).toBeTruthy();
  });

  it("goes back on Escape, keeping the place on the way out", async () => {
    await open();
    await key("ArrowRight");
    await key("Escape");
    await settle();
    expect(host.querySelector(".detail-stub")).toBeTruthy();
    expect(puts).toEqual([1]);
  });

  // A PDF page that is text, not a picture (ADR 0102), fails to load as an
  // image; it is said in words in its place, and the next page still shows.
  it("says so in place when a page cannot be shown", async () => {
    await open();
    await key("ArrowRight");
    act(() => {
      host.querySelector("img.reader__page")!.dispatchEvent(new Event("error"));
    });
    expect(host.querySelector("img.reader__page")).toBeNull();
    expect(host.querySelector(".reader__missing")?.textContent).toContain("Page 2 cannot be shown");
    await key("ArrowRight");
    expect(shown()).toBe("/api/items/7/pages/2");
    expect(host.querySelector(".reader__missing")).toBeNull();
  });

  it("says so when the comic cannot be opened", async () => {
    await act(async () => {
      root.render(
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
          <MemoryRouter initialEntries={["/read/8"]}>
            <Routes>
              <Route path="/read/:id" element={<Reader />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      );
    });
    await settle();
    expect(host.querySelector('[role="alert"]')?.textContent).toContain("cannot be opened");
  });
});
