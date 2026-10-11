/*
 * The phone navigation: a tab bar and two sheets (docs/design.md, Phone).
 *
 * jsdom performs no layout and applies no media queries, so which of the rail
 * and the tab bar is *visible* is the stylesheet's business and is checked by
 * looking at a phone-width screen. What this proves is the part that can be
 * wrong silently: that every place the rail offers is reachable from the tab
 * bar and its sheets, and that the sheets open, close and lead somewhere.
 *
 * Parity is the one worth a test. A place added to the rail tomorrow and not
 * to MobileNav would simply not exist on a phone, and nothing would say so.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, useLocation } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { AppShell } from "./AppShell";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let path = "";

function Probe() {
  path = useLocation().pathname;
  return null;
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/health")) return json({ status: "ok", version: "1", api_version: 1 });
      if (url.includes("/api/activity")) return json({ active: false, tasks: [] });
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      if (url.includes("/api/libraries")) {
        return json([
          { id: 1, name: "Movies", kind: "movie", item_count: 1, media_count: 1 },
          { id: 2, name: "Music", kind: "music", item_count: 1, media_count: 1 },
        ]);
      }
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function render(at = "/") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter initialEntries={[at]}>
            <Probe />
            <AppShell>
              <div />
            </AppShell>
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

async function settle() {
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

const tab = (label: string) =>
  [...host.querySelectorAll<HTMLElement>(".mnav__tab")].find((t) =>
    t.textContent?.includes(label),
  )!;

const sheetRows = () =>
  [...host.querySelectorAll(".mnav__sheet .mnav__row")].map((r) => r.textContent?.trim());

const railTitles = (sel: string) =>
  [...host.querySelectorAll(`${sel} .app-shell__lib`)].map((a) => a.getAttribute("title"));

async function click(el: HTMLElement) {
  await act(async () => {
    el.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
  await settle();
}

describe("the phone tab bar", () => {
  it("offers Home, Libraries, Search, People and More", async () => {
    await render();
    const labels = [...host.querySelectorAll(".mnav__tab")].map((t) =>
      t.textContent?.replace("⋯", "").trim(),
    );
    expect(labels).toEqual(["Home", "Libraries", "Search", "People", "More"]);
  });

  it("lists every library the rail does, Live TV included, in the same order", async () => {
    await render();
    await click(tab("Libraries"));
    const groups = host.querySelectorAll(".app-shell__group");
    const rail = [
      ...railTitles(".app-shell__libs"),
      ...[...groups[0].querySelectorAll(".app-shell__lib")].map((a) => a.getAttribute("title")),
    ];
    expect(sheetRows()).toEqual(rail);
  });

  /*
   * Everything else on the rail, minus People, which has a tab of its own.
   * The account row is labelled with the name, as on the rail.
   */
  it("puts the rest of the rail in More", async () => {
    await render();
    await click(tab("More"));
    const groups = host.querySelectorAll(".app-shell__group");
    const rail = [
      ...[...groups[1].querySelectorAll(".app-shell__lib")].map((a) => a.getAttribute("title")),
      ...railTitles(".app-shell__foot"),
    ]
      .filter((t) => t !== "People")
      .map((t) => (t?.startsWith("Signed in as ") ? t.slice("Signed in as ".length) : t));
    expect(sheetRows()).toEqual(rail);
  });

  it("goes where a row leads, and closes the sheet on the way", async () => {
    await render();
    await click(tab("Libraries"));
    const music = [...host.querySelectorAll<HTMLElement>(".mnav__sheet .mnav__row")].find(
      (r) => r.textContent?.includes("Music"),
    )!;
    await click(music);
    expect(path).toBe("/library/2");
    expect(host.querySelector(".mnav__sheet")).toBeNull();
  });

  // An open sheet owns the keyboard (design.md): Escape closes it and focus
  // goes back to the tab that opened it.
  it("closes on Escape and hands focus back to its tab", async () => {
    await render();
    const libs = tab("Libraries");
    await click(libs);
    expect(host.querySelector(".mnav__sheet")).not.toBeNull();
    await act(async () => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    });
    expect(host.querySelector(".mnav__sheet")).toBeNull();
    expect(document.activeElement).toBe(libs);
  });

  it("marks Libraries as where you are inside a library", async () => {
    await render("/library/1");
    expect(tab("Libraries").classList.contains("is-active")).toBe(true);
    expect(tab("Home").classList.contains("is-active")).toBe(false);
  });
});
