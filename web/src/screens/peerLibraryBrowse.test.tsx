/*
 * Opening a friend's show, and an album.
 *
 * Their TV and music libraries were walls of shows and artists that could only
 * be "played" — every tile went to the player, and a show has nothing to play.
 * A container now opens as a list of what is inside it; a film or an episode
 * still plays.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { FocusProvider } from "@/focus/FocusController";
import { PeerLibrary } from "./PeerLibrary";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const FP = "BBBB4G6XEG33AUPJU7LGSTT4N74U6D4B";

let host: HTMLDivElement;
let root: Root;
let gets: string[];
let path = "";
let downloads = false;

function Where() {
  path = useLocation().pathname;
  return null;
}

beforeEach(() => {
  gets = [];
  downloads = false;
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      gets.push(url);
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/libraries")) {
        return json({ libraries: [{ id: 4, name: "Their Films", kind: "movie", downloads }] });
      }
      if (url.includes("parent=50")) {
        return json({
          items: [{ id: 501, title: "Pilot", kind: "episode", library_id: 4, artwork: {} }],
          total: 1,
        });
      }
      if (url.includes("kind=collection")) {
        return json({
          items: [{ id: 80, title: "A Saga", kind: "collection", library_id: 4, artwork: {} }],
          total: 1,
        });
      }
      if (url.includes("parent=80")) {
        return json({
          items: [{ id: 801, title: "Part One", kind: "movie", library_id: 4, artwork: {} }],
          total: 1,
        });
      }
      if (url.includes("parent=70")) {
        return json({
          items: [{ id: 701, title: "Track One", kind: "track", library_id: 4, artwork: {} }],
          total: 1,
        });
      }
      return json({
        items: [
          { id: 50, title: "A Show", kind: "show", library_id: 4, artwork: {} },
          { id: 70, title: "A Record", kind: "album", library_id: 4, artwork: {} },
          { id: 60, title: "A Film", kind: "movie", library_id: 4, artwork: {} },
          { id: 80, title: "A Saga", kind: "collection", library_id: 4, artwork: {} },
        ],
        total: 4,
      });
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

async function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <FocusProvider>
          <MemoryRouter initialEntries={[`/peers/${FP}/library/4`]}>
            <Routes>
              <Route path="/peers/:fingerprint/library/:library" element={<PeerLibrary />} />
              <Route
                path="/peers/:fingerprint/library/:library/in/:parent"
                element={<PeerLibrary />}
              />
              <Route
                path="/peers/:fingerprint/library/:library/collections"
                element={<PeerLibrary view="collections" />}
              />
              <Route path="*" element={null} />
            </Routes>
            <Where />
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

const tile = (title: string) =>
  [...host.querySelectorAll<HTMLButtonElement>("button.poster-tile")].find(
    (b) => b.getAttribute("aria-label") === title,
  )!;

describe("a friend's library", () => {
  it("opens a show as its episodes, and an episode plays", async () => {
    await render();
    await act(async () => tile("A Show").click());
    await settle();

    expect(path).toBe(`/peers/${FP}/library/4/in/50`);
    expect(gets.some((u) => u.includes("parent=50"))).toBe(true);
    expect(host.querySelector("h1")?.textContent).toBe("A Show");
    expect(host.textContent).toContain("Pilot");

    await act(async () => tile("Pilot").click());
    expect(path).toBe(`/peers/${FP}/item/501`);
  });

  it("asks for an album in the order the record plays", async () => {
    await render();
    await act(async () => tile("A Record").click());
    await settle();
    expect(gets.some((u) => u.includes("parent=70") && u.includes("sort=track"))).toBe(true);
  });

  // Reported from a real pairing: the host's collections were missing from a
  // friend's shared library. A collection opens as its films, which play.
  it("opens a collection as its films", async () => {
    await render();
    await act(async () => tile("A Saga").click());
    await settle();
    expect(path).toBe(`/peers/${FP}/library/4/in/80`);
    expect(host.querySelector("h1")?.textContent).toBe("A Saga");
    expect(host.textContent).toContain("Part One");

    await act(async () => tile("Part One").click());
    expect(path).toBe(`/peers/${FP}/item/801`);
  });

  // Their collections in one place, as the household's own movie library has.
  it("lists their collections on their own", async () => {
    await render();
    const btn = [...host.querySelectorAll("button")].find((b) => b.textContent === "Collections");
    expect(btn, "no Collections button").toBeTruthy();
    await act(async () => btn!.click());
    await settle();
    expect(path).toBe(`/peers/${FP}/library/4/collections`);
    expect(gets.some((u) => u.includes("kind=collection"))).toBe(true);
    expect(host.textContent).toContain("A Saga");
  });

  // Right-click, as on our own tiles: Download only where their share allows
  // it, and never on a container, which has no file.
  async function menuFor(title: string) {
    await act(async () => {
      tile(title).dispatchEvent(
        new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 5, clientY: 5 }),
      );
    });
    const labels = [...document.querySelectorAll('[role="menuitem"]')].map((b) => b.textContent);
    await act(async () => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    });
    return labels;
  }

  it("offers Download on a right-click only where their share allows it", async () => {
    await render();
    expect(await menuFor("A Film")).toEqual(["Play"]);

    act(() => root.unmount());
    root = createRoot(host);
    downloads = true;
    await render();
    expect(await menuFor("A Film")).toEqual(["Play", "Download"]);
    expect(await menuFor("A Show")).toEqual(["Open"]);
  });

  it("still plays a film straight from the library", async () => {
    await render();
    await act(async () => tile("A Film").click());
    expect(path).toBe(`/peers/${FP}/item/60`);
  });
});
