/*
 * Retro games in the client (ADR 0073).
 *
 * What a ROM must not do here is reach the video player: there is no game
 * player in this client yet, and every control that navigates to /watch would
 * hand a ROM file to a <video> element. So the detail page says where the game
 * will play instead of offering Play, and the grid's menu offers nothing that
 * plays. The Console filter appears only where it can narrow something, and
 * names consoles rather than printing their keys.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { Detail } from "./Detail";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { FilterBar } from "@/components/FilterBar";
import { activePills, FILTER_PARAM_KEYS } from "@/lib/browseFilters";
import { configForKind, kindLabel } from "./libraryConfig";
import { playableKindFor } from "@/api/hooks";
import { orderPlatforms, platformLabel } from "@/lib/platforms";
import type { Facets } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const rom = {
  id: 41,
  library_id: 3,
  kind: "rom",
  title: "Super Mario 64",
  sort_title: "super mario 64",
  year: 1996,
  platform: "n64",
  region: "USA",
  missing: false,
  match_state: "matched",
  provider: "libretro-db",
  external_id: "Super Mario 64 (USA)",
};

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function stubFetch(item: Record<string, unknown>) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/children")) return json({ items: [] });
      if (/\/api\/items\/\d+$/.test(url.split("?")[0])) return json(item);
      if (url.includes("/api/items")) return json({ items: [], total: 0 });
      if (url.includes("/api/libraries")) return json([]);
      return json({});
    }),
  );
}

async function renderDetail(item: Record<string, unknown>) {
  stubFetch(item);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={[`/item/${item.id}`]}>
              <Routes>
                <Route path="/item/:id" element={<Detail />} />
              </Routes>
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

function buttonTexts(): string[] {
  return [...host.querySelectorAll("button")].map((b) => (b.textContent ?? "").trim());
}

describe("a retro game's detail page", () => {
  it("says where it plays instead of offering Play", async () => {
    await renderDetail(rom);
    expect(host.textContent).toContain("Super Mario 64");
    expect(host.textContent).toContain("Plays in the LANcast desktop app");
    const buttons = buttonTexts();
    for (const forbidden of ["Play", "Continue watching", "Play from start", "Add to playlist"]) {
      expect(buttons.some((b) => b === forbidden || b.endsWith(forbidden))).toBe(false);
    }
    // And nothing that marks it watched or rates it as a film.
    expect(buttons.some((b) => /watched/i.test(b))).toBe(false);
  });

  it("leads its meta line with the console and region", async () => {
    await renderDetail(rom);
    const meta = host.querySelector(".detail__meta")?.textContent ?? "";
    expect(meta).toContain("Nintendo 64");
    expect(meta).toContain("USA");
    expect(meta).toContain("1996");
  });

  // The control: a film on the same page still offers Play, so the test above
  // is about ROMs and not about a page that offers nothing to anyone.
  it("still offers Play on a film", async () => {
    await renderDetail({ ...rom, id: 42, kind: "movie", platform: undefined });
    expect(buttonTexts().some((b) => b.endsWith("Play"))).toBe(true);
  });
});

const facetsWith = (platforms: string[]): Facets => ({
  initials: [],
  platforms,
  genres: [],
  decades: [],
  content_ratings: [],
  years: [],
  resolutions: [],
  collections: [],
  max_rating: 0,
  has_watched: false,
  has_in_progress: false,
  has_unmatched: false,
});

async function renderBar(facets: Facets, onToggle = vi.fn()) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <FocusProvider>
            <FilterBar
              libraryID={3}
              facets={facets}
              params={new URLSearchParams()}
              onToggle={onToggle}
              onSet={vi.fn()}
              onClear={vi.fn()}
            />
          </FocusProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  return onToggle;
}

describe("the Console filter", () => {
  it("is not offered for a library of one console", async () => {
    await renderBar(facetsWith(["n64"]));
    expect(buttonTexts().some((b) => b.startsWith("Console"))).toBe(false);
  });

  it("names consoles in display order and toggles by key", async () => {
    const onToggle = await renderBar(facetsWith(["snes", "n64", "gba"]));
    const open = [...host.querySelectorAll("button")].find((b) =>
      (b.textContent ?? "").startsWith("Console"),
    );
    expect(open).toBeTruthy();
    await act(async () => {
      open!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    const chips = buttonTexts().filter((t) =>
      ["SNES", "Nintendo 64", "Game Boy Advance"].includes(t),
    );
    expect(chips).toEqual(["SNES", "Nintendo 64", "Game Boy Advance"]);
    const n64 = [...host.querySelectorAll("button")].find(
      (b) => (b.textContent ?? "").trim() === "Nintendo 64",
    )!;
    await act(async () => {
      n64.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    expect(onToggle).toHaveBeenCalledWith("platform", "n64");
  });

  it("is cleared with the other filters and reads as a name in the pill row", () => {
    expect(FILTER_PARAM_KEYS).toContain("platform");
    const pills = activePills(new URLSearchParams("platform=ps1"), {});
    expect(pills).toEqual([{ key: "platform", value: "ps1", label: "PlayStation" }]);
  });
});

describe("retro library configuration", () => {
  it("is labelled apart from the Games tab", () => {
    expect(kindLabel("retro")).toBe("Retro games");
  });

  it("has no Play all, because nothing in it plays here", () => {
    expect(playableKindFor("retro")).toBeNull();
  });

  it("does not offer a rating sort nobody can fill", () => {
    expect(configForKind("retro").sorts.map((s) => s.value)).toEqual(["title", "year", "added"]);
  });

  it("shows an unknown console as itself rather than nothing", () => {
    expect(platformLabel("dreamcast")).toBe("dreamcast");
    expect(orderPlatforms(["ps1", "dreamcast", "nes"])).toEqual(["nes", "ps1", "dreamcast"]);
  });
});
