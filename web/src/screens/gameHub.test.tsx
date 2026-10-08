/*
 * The Game Hub screen (docs/game-hub-plan.md), in each state Chris's notes ask
 * for: installed PC games or none, PC games switched off, a browser that can
 * never start one; a retro library, several, none, and no ROM database.
 *
 * jsdom performs no layout, so this holds what each half says and where each
 * way in leads — not that the two halves sit side by side.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { GameHub } from "./GameHub";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let libraries: unknown[];
let role: string;
let dbInstalled: boolean;
let where = "";

function Where() {
  const loc = useLocation();
  where = loc.pathname + loc.search;
  return null;
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  libraries = [];
  role = "admin";
  dbInstalled = true;
  where = "";
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      const json = (b: unknown) => new Response(JSON.stringify(b), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.includes("/api/auth/status")) return json({ user: { id: "u_1", role }, setup_required: false });
      if (url.includes("/api/libraries")) return json(libraries);
      if (url.includes("/api/retro/database")) return json({ installed: dbInstalled, files: [], platforms: [] });
      return json({});
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  delete window.lancastGames;
  delete window.lancastDesktopState;
});

async function open() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <FocusProvider>
          <MemoryRouter initialEntries={["/game-hub"]}>
            <Routes>
              <Route path="*" element={<GameHub />} />
            </Routes>
            <Where />
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 5; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
}

const half = (label: string) => host.querySelector(`[aria-label="${label}"]`) as HTMLElement;

async function click(el: Element | null) {
  expect(el).not.toBeNull();
  await act(async () => (el as HTMLElement).click());
}

// The desktop app's bindings, with the PC Games list switched on or off.
function desktop(enabled: boolean, games: unknown[] | "not-installed") {
  window.lancastDesktopState = vi.fn(async () => ({ games: enabled })) as never;
  window.lancastGames = vi.fn(async () =>
    games === "not-installed" ? { status: "not-installed" as const } : { status: "ok" as const, games: games as never },
  );
}

const pcGame = (id: string, hidden = false) => ({ id, name: id, size_bytes: 1, last_played: 0, install_path: "",
  has_poster: false, has_header: false, hidden, favourite: false, display: "" });

describe("the PC half", () => {
  it("in a browser, says PC games play in the desktop app and opens nothing", async () => {
    await open();
    expect(half("PC Games").textContent).toContain("desktop app");
    expect(half("PC Games").querySelector("button")).toBeNull();
  });

  it("counts installed games and opens PC Games", async () => {
    desktop(true, [pcGame("a"), pcGame("b"), pcGame("c", true)]);
    await open();
    expect(half("PC Games").textContent).toContain("2 installed games");
    await click(half("PC Games").querySelector("button"));
    expect(where).toBe("/games");
  });

  it("says when no games are installed", async () => {
    desktop(true, "not-installed");
    await open();
    expect(half("PC Games").textContent).toContain("No installed games found");
  });

  it("offers Settings when the list is switched off", async () => {
    desktop(false, []);
    await open();
    expect(half("PC Games").textContent).toContain("Turn on in Settings");
    await click(half("PC Games").querySelector("button"));
    expect(where).toBe("/settings?pane=games");
  });
});

describe("the retro half", () => {
  it("with no retro library, tells an admin how to add one and takes them there", async () => {
    libraries = [{ id: 1, kind: "movie", name: "Movies", item_count: 3 }];
    await open();
    const text = half("Retro Games").textContent ?? "";
    expect(text).toContain("No retro games library yet");
    expect(text).toContain("Retro games");
    expect(text).toContain("ROM database");
    await click(half("Retro Games").querySelector("button"));
    expect(where).toBe("/settings?pane=libraries");
  });

  it("with no retro library, sends a member to whoever runs the server", async () => {
    role = "member";
    libraries = [];
    await open();
    expect(half("Retro Games").textContent).toContain("Ask whoever runs this server");
    expect(half("Retro Games").querySelector("button")).toBeNull();
  });

  it("opens the one retro library", async () => {
    libraries = [{ id: 4, kind: "retro", name: "Retro games", item_count: 88 }];
    await open();
    expect(half("Retro Games").textContent).toContain("88 games in Retro games");
    await click(half("Retro Games").querySelector("button"));
    expect(where).toBe("/library/4");
  });

  it("lists several retro libraries, each its own way in", async () => {
    libraries = [
      { id: 4, kind: "retro", name: "Cartridges", item_count: 80 },
      { id: 9, kind: "retro", name: "Hacks", item_count: 1 },
    ];
    await open();
    const rows = [...half("Retro Games").querySelectorAll(".game-hub__lib")];
    expect(rows.map((r) => r.textContent)).toEqual(["Cartridges80 games", "Hacks1 game"]);
    await click(rows[1]);
    expect(where).toBe("/library/9");
  });

  it("nudges an admin to download a missing ROM database", async () => {
    libraries = [{ id: 4, kind: "retro", name: "Retro games", item_count: 88 }];
    dbInstalled = false;
    await open();
    const nudge = half("Retro Games").querySelector(".game-hub__nudge");
    expect(nudge?.textContent).toContain("ROM database is not installed");
    await click(nudge);
    expect(where).toBe("/settings?pane=retro");
  });

  it("does not nudge when the database is installed", async () => {
    libraries = [{ id: 4, kind: "retro", name: "Retro games", item_count: 88 }];
    await open();
    expect(half("Retro Games").querySelector(".game-hub__nudge")).toBeNull();
  });
});
