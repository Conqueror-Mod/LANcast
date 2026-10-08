/*
 * Fix match on a game (ADR 0073). The server searches the ROM database for a
 * ROM; the dialog has to say what it is offering — a game, not a "Movie" — and
 * not draw year and popularity meters that a game has no score for, which
 * would sit at zero and read as a poor match. A film's dialog is unchanged.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { FocusProvider } from "@/focus/FocusController";
import { FixMatch } from "./FixMatch";
import type { Item } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let candidates: unknown[];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      new Response(JSON.stringify(candidates), { status: 200, headers: { "Content-Type": "application/json" } }),
    ),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const breakdown = { title: 1, year: 0, popularity: 0, total: 0, year_gap: 0 };

async function open(item: Partial<Item>) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <FixMatch item={{ id: 5, library_id: 1, title: "x", ...item } as Item} onClose={() => {}} />
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
}

describe("Fix match on a game", () => {
  it("labels a ROM candidate a game and shows only its title score", async () => {
    candidates = [
      {
        Provider: "libretro-db", ExternalID: "Fire Emblem (USA, Australia)", Kind: "rom",
        Title: "Fire Emblem (USA, Australia)", Year: 2003, Score: 1, PosterURL: "https://x/box.png",
        Overview: "Strategy", Breakdown: breakdown,
      },
    ];
    await open({ kind: "rom", platform: "gba", title: "Fire Emblem" });
    const cand = host.querySelector(".fixmatch__cand")!;
    expect(cand.querySelector(".fixmatch__kind")?.textContent).toBe("Game");
    const meters = [...cand.querySelectorAll(".fixmatch__meter-label")].map((m) => m.textContent);
    expect(meters).toEqual(["Title"]);
    expect(cand.querySelector("img")?.classList.contains("fixmatch__poster--box")).toBe(true);
  });

  it("leaves a film's candidate as it was", async () => {
    candidates = [
      {
        Provider: "tmdb", ExternalID: "1", Kind: "movie", Title: "Antz", Year: 1998, Score: 0.9,
        PosterURL: "https://x/p.jpg", Overview: "", Breakdown: { ...breakdown, popularity: 0.4 },
      },
    ];
    await open({ kind: "movie", title: "Antz" });
    const cand = host.querySelector(".fixmatch__cand")!;
    expect(cand.querySelector(".fixmatch__kind")?.textContent).toBe("Movie");
    const meters = [...cand.querySelectorAll(".fixmatch__meter-label")].map((m) => m.textContent);
    expect(meters).toEqual(["Title", "Year", "Popularity"]);
    expect(cand.querySelector("img")?.classList.contains("fixmatch__poster--box")).toBe(false);
  });
});
