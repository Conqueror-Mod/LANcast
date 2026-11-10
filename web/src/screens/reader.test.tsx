/*
 * The comic reader (ADR 0101): it opens where you left off, turns pages from
 * the keyboard and the screen's sides, keeps your place, and opens a finished
 * comic at the start.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router";
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
/** What /api/profile/reading answers: the account's comic layout and fit. */
let reading: Record<string, unknown>;
/** Which way the comic reads, as the pages answer carries it (ADR 0123). */
let direction: Record<string, unknown>;
let role: string;
let directionPuts: unknown[];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  place = null;
  puts = [];
  reading = { account: true, comic_layout: null, comic_fit: null };
  direction = { direction: "ltr", direction_from: "default", series: "Saga" };
  role = "member";
  directionPuts = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (url.endsWith("/api/items/7/place") && init?.method === "PUT") {
        puts.push(JSON.parse(init.body as string).page);
        return new Response(null, { status: 204 });
      }
      if (url.endsWith("/api/profile/reading")) return json(reading);
      if (url.endsWith("/api/auth/status")) {
        return json({ authenticated: true, user: { id: "u1", name: "dana", role } });
      }
      if (url.endsWith("/api/items/7/series-direction") && init?.method === "PUT") {
        const body = JSON.parse(init.body as string);
        directionPuts.push(body);
        direction = body.direction
          ? { direction: body.direction, direction_from: "series", series: "Saga" }
          : { direction: "ltr", direction_from: "default", series: "Saga" };
        return json(direction);
      }
      if (url.endsWith("/api/items/7/pages")) return json({ pages: 5, place, ...direction });
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

/*
 * Layout from Your account › Reading & listening (settings-3-plan step 4).
 */
describe("the comic reader's layout", () => {
  const all = () => [...host.querySelectorAll("img.reader__page")].map((i) => i.getAttribute("src"));

  it("shows two facing pages, the cover alone, and turns a spread at a time", async () => {
    reading = { account: true, comic_layout: "spread", comic_fit: null };
    await open();
    expect(all()).toEqual(["/api/items/7/pages/0"]);
    await key("ArrowRight");
    expect(all()).toEqual(["/api/items/7/pages/1", "/api/items/7/pages/2"]);
    expect(host.textContent).toContain("2–3 / 5");
    await key("ArrowRight");
    expect(all()).toEqual(["/api/items/7/pages/3", "/api/items/7/pages/4"]);
    // The last spread: forward goes nowhere, and the button says so.
    await key("ArrowRight");
    expect(all()).toEqual(["/api/items/7/pages/3", "/api/items/7/pages/4"]);
    expect(host.querySelector<HTMLButtonElement>('[aria-label="Next page"]')!.disabled).toBe(true);
    await key("ArrowLeft");
    expect(all()).toEqual(["/api/items/7/pages/1", "/api/items/7/pages/2"]);
    // The place kept is the spread's first page.
    await settle(200);
    expect(puts.at(-1)).toBe(1);
  });

  it("shows one page on a screen taller than it is wide, whatever the layout", async () => {
    reading = { account: true, comic_layout: "spread", comic_fit: null };
    vi.stubGlobal("matchMedia", (q: string) => ({
      matches: false,
      media: q,
      addEventListener: () => {},
      removeEventListener: () => {},
    }));
    await open();
    await key("ArrowRight");
    expect(all()).toEqual(["/api/items/7/pages/1"]);
  });

  it("fitted to the width, leaves up and down to scroll the page", async () => {
    reading = { account: true, comic_layout: null, comic_fit: "width" };
    await open();
    expect(host.querySelector(".reader--fit-width")).not.toBeNull();
    await key("ArrowDown");
    expect(shown()).toBe("/api/items/7/pages/0");
    // A page that fits needs no scrolling, so Space turns it.
    await key(" ");
    expect(shown()).toBe("/api/items/7/pages/1");
    await key("ArrowRight");
    expect(shown()).toBe("/api/items/7/pages/2");
  });
});

describe("the comic reader, right to left (ADR 0123)", () => {
  const all = () => [...host.querySelectorAll("img.reader__page")].map((i) => i.getAttribute("src"));

  beforeEach(() => {
    direction = { direction: "rtl", direction_from: "library", series: "Saga" };
  });

  it("goes forward with the left arrow and back with the right", async () => {
    await open();
    expect(host.querySelector(".reader--rtl")).not.toBeNull();
    await key("ArrowLeft");
    await key("ArrowLeft");
    expect(shown()).toBe("/api/items/7/pages/2");
    await key("ArrowRight");
    expect(shown()).toBe("/api/items/7/pages/1");
    // Space has no side: still forward.
    await key(" ");
    expect(shown()).toBe("/api/items/7/pages/2");
  });

  it("goes forward on a tap on the left third, and the left button is Next", async () => {
    await open();
    const stage = host.querySelector(".reader__stage") as HTMLElement;
    Object.defineProperty(stage, "clientWidth", { value: 900 });
    await act(() => {
      stage.dispatchEvent(new MouseEvent("pointerdown", { bubbles: true, clientX: 100, clientY: 100 }));
      stage.dispatchEvent(new MouseEvent("pointerup", { bubbles: true, clientX: 100, clientY: 100 }));
    });
    expect(shown()).toBe("/api/items/7/pages/1");
    const buttons = [...host.querySelectorAll<HTMLButtonElement>(".reader__bar .reader__btn")].slice(-2);
    expect(buttons.map((b) => b.getAttribute("aria-label"))).toEqual(["Next page", "Previous page"]);
    await act(() => buttons[0].click());
    expect(shown()).toBe("/api/items/7/pages/2");
  });

  it("turns a spread with the left arrow, keeping its pages in reading order", async () => {
    reading = { account: true, comic_layout: "spread", comic_fit: null };
    await open();
    await key("ArrowLeft");
    // In reading order; the stage lays them out right to left.
    expect(all()).toEqual(["/api/items/7/pages/1", "/api/items/7/pages/2"]);
  });

  it("offers an admin the series' direction, and a member nothing", async () => {
    await open();
    expect(host.querySelector('[aria-label="Reading direction for Saga"]')).toBeNull();

    act(() => root.unmount());
    root = createRoot(host);
    role = "admin";
    await open();
    const select = host.querySelector<HTMLSelectElement>('[aria-label="Reading direction for Saga"]')!;
    expect(select).not.toBeNull();
    expect(select.value).toBe("");
    expect(select.options[0].textContent).toBe("Library's way (right to left)");

    await act(async () => {
      select.value = "ltr";
      select.dispatchEvent(new Event("change", { bubbles: true }));
    });
    await settle(20);
    expect(directionPuts).toEqual([{ direction: "ltr" }]);
    // The pages answer was read again, and the reader turned round.
    expect(host.querySelector(".reader--rtl")).toBeNull();
    await key("ArrowRight");
    expect(shown()).toBe("/api/items/7/pages/1");
  });

  it("leaves the arrows to the direction dropdown while it has focus", async () => {
    role = "admin";
    await open();
    const select = host.querySelector<HTMLSelectElement>('[aria-label="Reading direction for Saga"]')!;
    await act(() => {
      select.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowLeft", bubbles: true }));
    });
    expect(shown()).toBe("/api/items/7/pages/0");
  });
});
