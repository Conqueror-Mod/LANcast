/*
 * Playing a retro game, from the page's side (ADR 0073, stage 2).
 *
 * The desktop client is stubbed at its bindings — the only surface the page
 * has — so these prove the wiring: the ticket goes to the client and nowhere
 * else, Escape opens a menu that pauses, the menu's buttons send the commands
 * they say, an error before the game starts is shown rather than swallowed,
 * and a detail page offers Continue only where there is somewhere to continue
 * from. jsdom performs no layout, so nothing here says the menu is visible
 * over the picture; that needs the real window.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { RetroPlay } from "./RetroPlay";
import { Detail } from "./Detail";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const game = {
  id: 41,
  library_id: 3,
  kind: "rom",
  title: "Super Mario 64",
  sort_title: "super mario 64",
  platform: "n64",
  region: "USA",
  missing: false,
};

let host: HTMLDivElement;
let root: Root;
let saves: { slot: string; updated_at: number; size_bytes: number }[];
let fetches: string[];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  saves = [];
  fetches = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      fetches.push(url);
      const json = (b: unknown) =>
        new Response(JSON.stringify(b), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.includes("/stream-ticket")) return json({ ticket: "tk-secret", item_id: 41, expires_at: 0 });
      if (url.includes("/saves")) return json({ saves });
      if (url.includes("/children")) return json({ items: [] });
      if (/\/api\/items\/\d+$/.test(url.split("?")[0])) return json(game);
      return json({ items: [], total: 0 });
    }),
  );
  window.lancastRetroOpen = vi.fn(async () => {});
  window.lancastRetroCommand = vi.fn(async () => {});
  window.lancastRetroStop = vi.fn(async () => {});
  window.lancastRetroAvailable = vi.fn(async () => ({ available: true, core: "mGBA" }));
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  delete window.lancastRetroOpen;
  delete window.lancastRetroCommand;
  delete window.lancastRetroStop;
  delete window.lancastRetroAvailable;
  delete window.__lancastRetroEvent;
});

async function settle() {
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
}

async function render(path: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={[path]}>
              <Routes>
                <Route path="/play/:id" element={<RetroPlay />} />
                <Route path="/item/:id" element={<Detail />} />
              </Routes>
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

function emit(e: Record<string, unknown>) {
  act(() => window.__lancastRetroEvent?.(e as never));
}

function buttons(): HTMLButtonElement[] {
  return [...host.querySelectorAll("button")] as HTMLButtonElement[];
}

function button(label: string): HTMLButtonElement {
  const b = buttons().find((x) => x.textContent?.trim() === label);
  if (!b) throw new Error(`no ${label}; have ${buttons().map((x) => x.textContent).join(", ")}`);
  return b;
}

describe("the game screen", () => {
  it("hands the client the game, its console and a ticket — never a URL", async () => {
    await render("/play/41?resume=1");
    expect(window.lancastRetroOpen).toHaveBeenCalledWith(41, "tk-secret", "n64", true);
    expect(fetches.some((u) => u.includes("tk-secret"))).toBe(false);
  });

  it("shows loading until the game starts", async () => {
    await render("/play/41");
    emit({ kind: "loading", done: 50, total: 200 });
    expect(host.textContent).toContain("25%");
    emit({ kind: "started" });
    expect(host.textContent).not.toContain("Fetching");
  });

  it("opens a menu that pauses on Escape, and its buttons say what they do", async () => {
    await render("/play/41");
    emit({ kind: "started" });
    await act(async () => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    });
    expect(window.lancastRetroCommand).toHaveBeenCalledWith("pause", "");
    expect(host.querySelector('[role="dialog"]')).not.toBeNull();

    await act(async () => button("Save to Slot 2").click());
    expect(window.lancastRetroCommand).toHaveBeenCalledWith("save-state", "state-2");

    await act(async () => button("Resume").click());
    expect(window.lancastRetroCommand).toHaveBeenCalledWith("resume", "");
    expect(host.querySelector('[role="dialog"]')).toBeNull();
  });

  it("offers to load only the slots that hold a save", async () => {
    saves = [{ slot: "state-3", updated_at: 1, size_bytes: 4 }];
    await render("/play/41");
    emit({ kind: "started" });
    emit({ kind: "menu" });
    const labels = buttons().map((b) => b.textContent?.trim());
    expect(labels).toContain("Load Slot 3");
    expect(labels).not.toContain("Load Slot 1");
  });

  it("shows an error that arrives before the game starts, with a way back", async () => {
    await render("/play/41");
    emit({ kind: "error", text: "No mGBA core is installed." });
    expect(host.querySelector('[role="alert"]')?.textContent).toContain("No mGBA core is installed.");
    expect(button("Back")).toBeTruthy();
  });

  it("stops the game when the screen goes", async () => {
    await render("/play/41");
    act(() => root.unmount());
    expect(window.lancastRetroStop).toHaveBeenCalled();
    root = createRoot(host);
  });
});

describe("a game's detail page", () => {
  it("offers Play where the console can run here", async () => {
    await render("/item/41");
    const labels = buttons().map((b) => b.textContent?.trim() ?? "");
    expect(labels.some((l) => l.endsWith("Play"))).toBe(true);
    expect(labels.some((l) => l.endsWith("Continue"))).toBe(false);
  });

  it("offers Continue and Start over when a game was left mid-way", async () => {
    saves = [{ slot: "auto", updated_at: 1, size_bytes: 4 }];
    await render("/item/41");
    const labels = buttons().map((b) => b.textContent?.trim() ?? "");
    expect(labels.some((l) => l.endsWith("Continue"))).toBe(true);
    expect(labels).toContain("Start over");
  });

  it("says why not where the console cannot run", async () => {
    window.lancastRetroAvailable = vi.fn(async () => ({
      available: false,
      reason: "Games for this console play in a later release.",
    }));
    await render("/item/41");
    expect(host.textContent).toContain("Games for this console play in a later release.");
    expect(buttons().some((b) => b.textContent?.trim().endsWith("Play"))).toBe(false);
  });
});
