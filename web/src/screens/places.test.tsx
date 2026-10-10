/*
 * The places screen (ADR 0078).
 *
 * jsdom paints nothing, so these are about wiring and about the three things
 * the screen must keep apart: reading is off, reading found nothing, and
 * reading found places. Each wants a different next step, and a screen that
 * showed one empty page for all three would send somebody looking for a
 * fault that is not there.
 *
 * Place names are fake on purpose; this file must not read as anybody's
 * whereabouts.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { Places } from "./Places";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

type World = {
  enabled: boolean;
  admin?: boolean;
  places?: unknown[];
  elsewhere?: number;
  unlocated?: number;
};

const alpha = {
  id: 9000001,
  name: "Alphaville",
  region: "Somewhere",
  country_code: "ZZ",
  country: "Testland",
  count: 3,
};
const beta = {
  id: 9000002,
  name: "Betaburg",
  region: "Elsewhere",
  country_code: "ZZ",
  country: "Testland",
  count: 1,
};

let host: HTMLDivElement;
let root: Root;
let gets: string[];
let puts: { url: string; body: unknown }[];

function mount(w: World) {
  gets = [];
  puts = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if ((init?.method ?? "GET") !== "GET") {
        puts.push({ url, body: JSON.parse(String(init?.body ?? "null")) });
        return json({ photo_places: true });
      }
      gets.push(url);
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "someone", role: w.admin ? "admin" : "member" },
        });
      }
      if (/\/places\/[^/?]+/.test(url)) {
        return json({
          items: [
            { id: 1, title: "one", kind: "photo", library_id: 5, artwork: {} },
          ],
          total: 1,
        });
      }
      if (url.includes("/places")) {
        return json({
          enabled: w.enabled,
          reading: false,
          places: w.enabled ? (w.places ?? []) : [],
          elsewhere: w.elsewhere ?? 0,
          unlocated: w.unlocated ?? 0,
          unread: w.enabled ? 0 : 40,
        });
      }
      if (url.includes("/api/libraries")) {
        return json([{ id: 5, name: "Pictures", kind: "picture" }]);
      }
      return json({});
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

async function settle() {
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

async function render() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter initialEntries={["/library/5/places"]}>
            <Routes>
              <Route path="/library/:id/places" element={<Places />} />
            </Routes>
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

function buttonNamed(text: string): HTMLButtonElement | undefined {
  return [...host.querySelectorAll("button")].find((b) =>
    b.textContent?.includes(text),
  );
}

describe("the places screen", () => {
  it("offers the switch to an administrator while reading is off", async () => {
    mount({ enabled: false, admin: true });
    await render();
    const on = buttonNamed("Read where photos were taken");
    expect(on).toBeDefined();

    await act(async () => on!.click());
    await settle();
    expect(puts).toHaveLength(1);
    expect(puts[0].url).toContain("/api/settings");
    expect(puts[0].body).toEqual({ photo_places: true });
  });

  // Somebody who cannot turn it on is told who can, not shown a button that
  // answers 403.
  it("tells anybody else who can turn it on", async () => {
    mount({ enabled: false, admin: false });
    await render();
    expect(buttonNamed("Read where photos were taken")).toBeUndefined();
    expect(host.textContent).toContain("An administrator can turn this on");
  });

  it("says a library with no locations has none, rather than offering the switch", async () => {
    mount({ enabled: true, admin: true, unlocated: 40 });
    await render();
    expect(buttonNamed("Read where photos were taken")).toBeUndefined();
    expect(host.textContent).toContain("None of these photographs say where");
  });

  it("lists places with where they are, and opens only the first", async () => {
    mount({ enabled: true, places: [alpha, beta], unlocated: 12 });
    await render();
    expect(host.textContent).toContain("Alphaville");
    expect(host.textContent).toContain("Betaburg");
    // One country in the library, so it is not repeated on every line.
    expect(host.textContent).toContain("Somewhere");
    expect(host.textContent).not.toContain("Testland");
    expect(host.textContent).toContain("12 photos say nothing");

    const opened = gets.filter((u) => /\/places\/\d+/.test(u));
    expect(opened).toEqual([
      expect.stringContaining(`/places/${alpha.id}`),
    ]);
  });

  it("opens a place's photographs when its heading is pressed", async () => {
    mount({ enabled: true, places: [alpha, beta] });
    await render();
    await act(async () => buttonNamed("Betaburg")!.click());
    await settle();
    expect(gets.some((u) => u.includes(`/places/${beta.id}`))).toBe(true);
  });

  // Photographs with a position and no town near it are a bucket, not lost.
  it("gives photographs far from any town a heading of their own", async () => {
    mount({ enabled: true, places: [alpha], elsewhere: 2 });
    await render();
    await act(async () => buttonNamed("Far from any town")!.click());
    await settle();
    expect(gets.some((u) => u.includes("/places/elsewhere"))).toBe(true);
  });
});
