/*
 * The docked card's corner and size (ADR 0076 stage 2): the grip and size
 * button on the strip, the page attributes both halves of the card read, and
 * the native picture following the card.
 *
 * jsdom performs no layout, so "the card is in the top-left" is asserted as
 * the attribute the CSS keys on and the rectangle sent to the client, not as
 * pixels on a screen.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback } from "./PlaybackProvider";
import { MiniPlayer } from "@/components/MiniPlayer";
import { resetNativePlaybackAvailability } from "./mpvBackend";
import { getDock, resetDock } from "@/lib/dock";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
let layouts: { layout: string; x: number }[] = [];

function Probe() {
  pb = usePlayback();
  return null;
}

beforeEach(() => {
  localStorage.clear();
  resetDock();
  layouts = [];
  resetNativePlaybackAvailability();
  window.lancastMpvAvailable = vi.fn(async () => true);
  window.lancastMpvOpen = vi.fn(async () => {});
  window.lancastMpvCommand = vi.fn(async () => {});
  window.lancastMpvStop = vi.fn(async () => {});
  window.lancastMpvLayout = vi.fn(async (layout: string, x: number) => {
    layouts.push({ layout, x });
  });
  // The docked box: on the left when the page says the corner is a left one,
  // so a corner change is visible in the rectangle sent to the client.
  vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(() => {
    const c = document.documentElement.getAttribute("data-dock-corner") ?? "br";
    const left = c === "bl" || c === "tl" ? 78 : 1600;
    return { left, top: 700, width: 300, height: 169, right: left + 300, bottom: 869, x: left, y: 700, toJSON: () => ({}) } as DOMRect;
  });
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
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
      const json = (b: unknown) => new Response(JSON.stringify(b), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.includes("/stream-ticket")) return json({ ticket: "tk", item_id: 7, expires_at: 0 });
      if (/\/api\/items\/7(\?|$)/.test(url)) {
        return json({ id: 7, title: "1408", kind: "movie", duration_ms: 6_000_000, progress: { position_ms: 0, watched: false },
          streams: [{ index: 0, kind: "video", codec: "hevc" }] });
      }
      if (url.includes("/playback")) return json({ decision: { method: "direct", reason: "" } });
      if (url.includes("/api/auth")) return json({ user: { role: "admin" }, can_convert: true });
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  delete window.lancastMpvAvailable;
  delete window.lancastMpvOpen;
  delete window.lancastMpvCommand;
  delete window.lancastMpvStop;
  delete window.lancastMpvLayout;
  resetNativePlaybackAvailability();
  localStorage.clear();
  resetDock();
});

async function settle(ms = 60) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

async function dockAFilm() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter>
              <Probe />
              <MiniPlayer />
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
  await act(async () => pb.play(7, [7]));
  await settle();
  await act(async () => {
    window.__lancastMpvEvent?.({ events: ["loadedmetadata"], current_time: 0, duration: 6000, paused: false, ended: false });
  });
  await settle();
}

const grip = () => host.querySelector(".mini__grip") as HTMLButtonElement;
const sizeButton = () => host.querySelector(".mini__size") as HTMLButtonElement;
const html = () => document.documentElement;

function pointer(el: Element, type: string, x: number, y: number) {
  el.dispatchEvent(new MouseEvent(type, { bubbles: true, clientX: x, clientY: y, button: 0 }));
}

describe("the docked card's corner and size", () => {
  it("puts its corner and size on the page, where both halves read them", async () => {
    await dockAFilm();
    expect(html().getAttribute("data-dock-corner")).toBe("br");
    expect(html().style.getPropertyValue("--mini-w")).toBe("300px");
  });

  it("goes round the corners from the grip, and through the sizes from its button", async () => {
    await dockAFilm();
    await act(async () => grip().click());
    expect(getDock().corner).toBe("bl");
    expect(html().getAttribute("data-dock-corner")).toBe("bl");
    await act(async () => sizeButton().click());
    expect(getDock().size).toBe("l");
    expect(html().style.getPropertyValue("--mini-w")).toBe("420px");
    expect(sizeButton().textContent).toBe("L");
  });

  it("follows a drag and settles in the corner it was let go in, without also going round", async () => {
    await dockAFilm();
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 1920 });
    Object.defineProperty(window, "innerHeight", { configurable: true, value: 1080 });
    const g = grip();
    await act(async () => pointer(g, "pointerdown", 1700, 1000));
    await act(async () => pointer(g, "pointermove", 1702, 1001)); // a wobble, not a drag
    expect(getDock().dx).toBe(0);
    await act(async () => pointer(g, "pointermove", 300, 200));
    expect(getDock().dx).toBe(-1400);
    expect(html().hasAttribute("data-dock-dragging")).toBe(true);
    await act(async () => {
      pointer(g, "pointerup", 300, 200);
      g.click(); // the click a drag ends in
    });
    expect(getDock()).toMatchObject({ corner: "tl", dx: 0, dy: 0 });
    expect(html().hasAttribute("data-dock-dragging")).toBe(false);
  });

  it("tells the desktop client where the picture went when the corner changes", async () => {
    await dockAFilm();
    const before = layouts.at(-1);
    expect(before?.layout).toBe("mini");
    expect(before?.x).toBe(1600);
    await act(async () => grip().click()); // to bottom left
    await settle(40);
    // The same size, so no resize observer fires: only the dock subscription
    // can have sent this.
    expect(layouts.at(-1)?.x).toBe(78);
  });
});
