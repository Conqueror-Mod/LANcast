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
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
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

function Where() {
  path = useLocation().pathname;
  return null;
}

beforeEach(() => {
  gets = [];
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
        return json({ libraries: [{ id: 4, name: "Their TV", kind: "show" }] });
      }
      if (url.includes("parent=50")) {
        return json({
          items: [{ id: 501, title: "Pilot", kind: "episode", library_id: 4, artwork: {} }],
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
        ],
        total: 3,
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

  it("still plays a film straight from the library", async () => {
    await render();
    await act(async () => tile("A Film").click());
    expect(path).toBe(`/peers/${FP}/item/60`);
  });
});
