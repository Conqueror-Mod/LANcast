/*
 * A docked film floats above a game (ADR 0076).
 *
 * While a game screen is up, the page stays in the client's overlay over the
 * game, and a docked film's picture must go *above* the page ("pip") rather
 * than above the main window ("mini"), where it would be under the overlay and
 * out of sight. The provider sends which; the game screen coming and going is
 * one of the things that must make it say so again, or the film is left on the
 * wrong side of the page for as long as nothing else moves.
 *
 * jsdom performs no layout, so the docked box is stubbed: this proves which
 * layout is sent and when, not where anything lands on the screen.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback } from "./PlaybackProvider";
import { resetNativePlaybackAvailability } from "./mpvBackend";
import { setGameOnScreen } from "./retro";
import { nativeLayout } from "./nativeLayout";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
let layouts: string[] = [];

function Probe() {
  pb = usePlayback();
  return null;
}

beforeEach(() => {
  layouts = [];
  localStorage.clear();
  resetNativePlaybackAvailability();
  setGameOnScreen(false);
  window.lancastMpvAvailable = vi.fn(async () => true);
  window.lancastMpvOpen = vi.fn(async () => {});
  window.lancastMpvCommand = vi.fn(async () => {});
  window.lancastMpvStop = vi.fn(async () => {});
  window.lancastMpvLayout = vi.fn(async (layout: string) => {
    layouts.push(layout);
  });
  vi.spyOn(Element.prototype, "getBoundingClientRect").mockReturnValue({
    left: 900,
    top: 500,
    width: 300,
    height: 169,
    right: 1200,
    bottom: 669,
    x: 900,
    y: 500,
    toJSON: () => ({}),
  } as DOMRect);
  const proto = window.HTMLMediaElement.prototype;
  proto.load = vi.fn();
  proto.play = vi.fn(async () => {});
  proto.pause = vi.fn();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  // jsdom has none; the docked layout watches its box with one.
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.includes("/stream-ticket")) return json({ ticket: "tk", item_id: 7, expires_at: 0 });
      if (/\/api\/items\/\d+(\?|$)/.test(url)) {
        return json({
          id: 7,
          title: "A film",
          kind: "movie",
          duration_ms: 6_000_000,
          progress: { position_ms: 0, watched: false },
          streams: [
            { index: 0, kind: "video", codec: "hevc" },
            { index: 1, kind: "audio", codec: "aac" },
          ],
        });
      }
      if (url.includes("/api/auth")) return json({ user: { role: "admin" }, can_convert: true });
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  setGameOnScreen(false);
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  delete window.lancastMpvAvailable;
  delete window.lancastMpvOpen;
  delete window.lancastMpvCommand;
  delete window.lancastMpvStop;
  delete window.lancastMpvLayout;
  resetNativePlaybackAvailability();
});

async function settle(ms = 80) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

describe("a docked film and a game", () => {
  it("is docked above the page over a game, and back above the window after", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    await act(async () => {
      root.render(
        <QueryClientProvider client={qc}>
          <MemoryRouter>
            <FocusProvider>
              <PlaybackProvider>
                <Probe />
              </PlaybackProvider>
            </FocusProvider>
          </MemoryRouter>
        </QueryClientProvider>,
      );
    });
    await settle();
    // No player screen claims the surface, so the film plays docked.
    await act(async () => pb.play(7, [7]));
    await settle();
    await act(async () => {
      window.__lancastMpvEvent?.({ events: ["loadedmetadata"], current_time: 0, duration: 6000, paused: false, ended: false });
    });
    await settle();
    expect(layouts.at(-1)).toBe("mini");

    // A game starts: the same film, the same box, now above the page.
    await act(async () => setGameOnScreen(true));
    await settle();
    expect(layouts.at(-1)).toBe("pip");

    // The game ends: back where it was.
    await act(async () => setGameOnScreen(false));
    await settle();
    expect(layouts.at(-1)).toBe("mini");
  });
});

describe("nativeLayout over a game", () => {
  const box = { left: 10, top: 10, width: 300, height: 169 };
  it("docks as pip over a game and mini elsewhere, the same rectangle either way", () => {
    const over = nativeLayout("mini", true, box, 1.5, true);
    const plain = nativeLayout("mini", true, box, 1.5, false);
    expect(over.layout).toBe("pip");
    expect(plain.layout).toBe("mini");
    expect({ ...over, layout: "" }).toEqual({ ...plain, layout: "" });
  });
  it("leaves full and hidden alone", () => {
    expect(nativeLayout("full", true, null, 1, true).layout).toBe("full");
    expect(nativeLayout("idle", true, box, 1, true).layout).toBe("hidden");
    expect(nativeLayout("mini", false, box, 1, true).layout).toBe("hidden");
  });
});
