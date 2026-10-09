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
import { PlaybackProvider, usePlayback } from "@/playback/PlaybackProvider";
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

// A film to dock in the corner while the game runs.
const FILM = {
  id: 77,
  library_id: 1,
  kind: "movie",
  title: "1408",
  duration_ms: 6_000_000,
  progress: { position_ms: 0, watched: false },
  streams: [
    { index: 0, kind: "video", codec: "h264" },
    { index: 1, kind: "audio", codec: "aac" },
  ],
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
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      fetches.push(url);
      const json = (b: unknown) =>
        new Response(JSON.stringify(b), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.includes("/stream-ticket")) return json({ ticket: "tk-secret", item_id: 41, expires_at: 0 });
      if (url.includes("/saves")) return json({ saves });
      if (url.includes("/children")) return json({ items: [] });
      if (url.split("?")[0].endsWith(`/api/items/${FILM.id}`)) return json(FILM);
      if (url.includes(`/api/items/${FILM.id}/playback`)) {
        return json({ decision: { method: "direct", reason: "" } });
      }
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

let pb: ReturnType<typeof usePlayback> | undefined;
function Probe() {
  pb = usePlayback();
  return null;
}

async function render(path: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <Probe />
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

describe("a film in the corner (ADR 0076)", () => {
  it("says a game is on screen while it is, and leaves the film's window alone on the way out", async () => {
    const { isGameOnScreen } = await import("@/playback/retro");
    window.lancastMpvLayout = vi.fn(async () => {});
    await render("/play/41");
    expect(isGameOnScreen()).toBe(true);
    act(() => root.unmount());
    expect(isGameOnScreen()).toBe(false);
    // The video window belongs to whatever film is docked; hiding it here
    // would black out a film somebody is watching in the corner.
    expect(window.lancastMpvLayout).not.toHaveBeenCalled();
    expect(window.lancastRetroStop).toHaveBeenCalled();
    root = createRoot(host);
    delete window.lancastMpvLayout;
  });

  it("leaves the page see-through on the way out when a film still needs it", async () => {
    const { holdNativeVideo, resetNativeVideoHolders, NATIVE_VIDEO_CLASS } = await import("@/playback/mpvBackend");
    holdNativeVideo("film"); // a film docked in the corner, playing natively
    await render("/play/41");
    act(() => root.unmount());
    // Taken off here, the maximised film was a black page over a playing film.
    expect(document.documentElement.classList.contains(NATIVE_VIDEO_CLASS)).toBe(true);
    root = createRoot(host);
    resetNativeVideoHolders();
  });

  it("offers the game's own volume in steps and sends the next one", async () => {
    window.lancastRetroVolume = vi.fn(async () => 0.5);
    window.lancastRetroSetVolume = vi.fn(async () => {});
    await render("/play/41");
    emit({ kind: "started" });
    emit({ kind: "menu" });
    await act(async () => button("Game volume: 50%").click());
    expect(window.lancastRetroSetVolume).toHaveBeenCalledWith(0.25);
    expect(button("Game volume: 25%")).toBeTruthy();
    delete window.lancastRetroVolume;
    delete window.lancastRetroSetVolume;
  });

  it("offers nothing for the corner when nothing is playing", async () => {
    await render("/play/41");
    emit({ kind: "started" });
    emit({ kind: "menu" });
    expect(host.querySelector('[aria-label="In the corner"]')).toBeNull();
  });

  it("pauses and stops what plays in the corner from the menu the pad can reach", async () => {
    const proto = window.HTMLMediaElement.prototype;
    vi.spyOn(proto, "load").mockImplementation(() => {});
    vi.spyOn(proto, "play").mockImplementation(async () => {});
    vi.spyOn(proto, "pause").mockImplementation(() => {});
    await render("/play/41");
    emit({ kind: "started" });
    // A film docked in the corner: the provider has an item.
    await act(async () => pb!.play(FILM.id, [FILM.id]));
    await settle();
    emit({ kind: "menu" });
    const group = host.querySelector('[aria-label="In the corner"]');
    expect(group).not.toBeNull();
    const labels = [...group!.querySelectorAll("button")].map((b) => b.textContent?.trim() ?? "");
    expect(labels.some((l) => l === `Pause ${FILM.title}` || l === `Play ${FILM.title}`)).toBe(true);
    expect(labels).toContain("Stop the film");

    // Where the card sits and how big, from the pad (ADR 0076 stage 2).
    await act(async () => button("Corner: Bottom right").click());
    expect(button("Corner: Bottom left")).toBeTruthy();
    await act(async () => button("Size: Medium").click());
    expect(button("Size: Large")).toBeTruthy();
    const { resetDock } = await import("@/lib/dock");
    localStorage.clear();
    resetDock();

    // Stop goes to the player, and the corner group goes with the film.
    await act(async () => button("Stop the film").click());
    await settle();
    expect(pb!.itemID).toBe(0);
    expect(host.querySelector('[aria-label="In the corner"]')).toBeNull();
  });

  it("goes round from off back to full", async () => {
    const { nextVolume, volumeLabel } = await import("@/playback/retro");
    expect(nextVolume(0)).toBe(1);
    expect(nextVolume(1)).toBe(0.75);
    expect(nextVolume(0.6)).toBe(1); // a value off the steps goes to full
    expect(volumeLabel(0)).toBe("Off");
    expect(volumeLabel(0.75)).toBe("75%");
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

describe("picture options", () => {
  it("curates by the end of the key, in a fixed order, only what the core declares", async () => {
    const { curatedOptions, nextValue } = await import("@/playback/retro");
    const declared = [
      { key: "mupen64plus-aspect", description: "Aspect Ratio", values: ["4:3", "16:9"], value: "4:3" },
      { key: "mupen64plus-cpucore", description: "CPU Core", values: ["a", "b"], value: "a" },
      { key: "mupen64plus-43screensize", description: "4:3 Resolution", values: ["320x240", "640x480"], value: "640x480" },
      { key: "x-EnableNativeResFactor", description: "Native", values: ["0"], value: "0" },
    ];
    const got = curatedOptions(declared).map((o) => o.key);
    expect(got).toEqual(["mupen64plus-43screensize", "mupen64plus-aspect"]);
    expect(nextValue(declared[2])).toBe("320x240");
    expect(curatedOptions(undefined)).toEqual([]);
  });

  it("offers them in the menu and cycles one through the client", async () => {
    window.lancastRetroSetOption = vi.fn(async () => {});
    await render("/play/41");
    emit({ kind: "started" });
    emit({
      kind: "options",
      options: [{ key: "mupen64plus-43screensize", description: "4:3 Resolution", values: ["640x480", "960x720"], value: "640x480" }],
    });
    emit({ kind: "menu" });
    await act(async () => button("4:3 Resolution: 640x480").click());
    expect(window.lancastRetroSetOption).toHaveBeenCalledWith("n64", "mupen64plus-43screensize", "960x720");
    delete window.lancastRetroSetOption;
  });
});
