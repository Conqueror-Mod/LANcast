/*
 * The docked picture follows the window onto a monitor with another scale.
 *
 * Reported: drag the app onto the other screen with a film in the mini player
 * and the picture travels with it at its own offset, off to the side of the
 * window. The docked box is sent to the client in device pixels, and moving to
 * a monitor at a different scale changes the device pixels of the same CSS box
 * without anything else on the page firing — so the old rectangle stood.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback } from "./PlaybackProvider";
import { resetNativePlaybackAvailability } from "./mpvBackend";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
let layouts: { layout: string; x: number; y: number; w: number; h: number }[] = [];
/** The listeners a resolution query registered, so a test can fire them. */
let ratioListeners: (() => void)[] = [];
let opened: number[] = [];

function Probe() {
  pb = usePlayback();
  // No full-surface claim: the provider is docked, which is the layout whose
  // rectangle depends on the monitor's scale.
  return <span>{pb.itemID}</span>;
}

function itemBody(id: number) {
  return {
    id,
    title: `Film ${id}`,
    kind: "movie",
    duration_ms: 6_000_000,
    progress: { position_ms: 0, watched: false },
    streams: [
      { index: 0, kind: "video", codec: "hevc" },
      { index: 1, kind: "audio", codec: "truehd", language: "eng" },
    ],
  };
}

/** What the client reports once a file is open. */
function clientOpened(id: number) {
  window.__lancastMpvEvent?.({
    events: ["loadedmetadata", "loadeddata"],
    current_time: 0,
    duration: 6000,
    paused: true,
    ended: false,
  });
  return id;
}

beforeEach(() => {
  layouts = [];
  opened = [];
  localStorage.clear();
  resetNativePlaybackAvailability();

  window.lancastMpvAvailable = vi.fn(async () => true);
  window.lancastMpvOpen = vi.fn(async (id: number) => {
    opened.push(id);
  });
  window.lancastMpvCommand = vi.fn(async () => {});
  window.lancastMpvStop = vi.fn(async () => {});
  window.lancastMpvLayout = vi.fn(
    async (layout: string, x: number, y: number, w: number, h: number) => {
      layouts.push({ layout, x, y, w, h });
    },
  );
  ratioListeners = [];
  Object.defineProperty(window, "devicePixelRatio", { configurable: true, value: 1 });
  vi.stubGlobal(
    "matchMedia",
    vi.fn(() => ({
      matches: true,
      addEventListener: (_: string, fn: () => void) => ratioListeners.push(fn),
      removeEventListener: (_: string, fn: () => void) => {
        ratioListeners = ratioListeners.filter((f) => f !== fn);
      },
    })),
  );
  // jsdom has no layout and no ResizeObserver; the docked box is a fixed one.
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
  vi.spyOn(Element.prototype, "getBoundingClientRect").mockReturnValue({
    left: 100, top: 50, width: 320, height: 180, right: 420, bottom: 230, x: 100, y: 50,
    toJSON: () => ({}),
  } as DOMRect);

  const proto = window.HTMLMediaElement.prototype;
  proto.load = vi.fn();
  proto.play = vi.fn(async () => {});
  proto.pause = vi.fn();

  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);

  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/stream-ticket")) return json({ ticket: "tk", item_id: 1, expires_at: 0 });
      const item = url.match(/\/api\/items\/(\d+)(\?|$)/);
      if (item) return json(itemBody(Number(item[1])));
      if (url.includes("/api/auth")) return json({ user: { role: "admin" }, can_convert: true });
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  delete window.lancastMpvAvailable;
  delete window.lancastMpvOpen;
  delete window.lancastMpvCommand;
  delete window.lancastMpvStop;
  delete window.lancastMpvLayout;
  resetNativePlaybackAvailability();
  localStorage.clear();
});

async function settle(ms = 80) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

async function mount() {
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
}


describe("the docked picture across monitors", () => {
  it("is placed again, at the new scale, when the pixel ratio changes", async () => {
    await mount();
    await act(async () => pb.play(101, [101]));
    await settle();
    await act(async () => {
      clientOpened(101);
    });
    await settle();

    const docked = layouts.filter((l) => l.layout === "mini");
    expect(docked.length, "the docked picture was never placed").toBeGreaterThan(0);
    const before = docked.at(-1)!;
    expect(before.w).toBe(320);

    // Onto a 150% monitor: same CSS box, half again as many device pixels.
    Object.defineProperty(window, "devicePixelRatio", { configurable: true, value: 1.5 });
    expect(ratioListeners.length, "nothing listens for the ratio").toBeGreaterThan(0);
    await act(async () => {
      for (const fn of [...ratioListeners]) fn();
    });
    await settle();

    const after = layouts.at(-1)!;
    expect(after.layout).toBe("mini");
    expect(after.w).toBe(480);
    expect(after.x).toBe(150);
  });
});
