/*
 * The page leaves the screen over a film once nothing is over it (ADR 0111),
 * through the real provider and the real Player, down the native path.
 *
 * The case that matters is a held start. Frame-rate matching keeps a film
 * paused for a few seconds while the TV changes mode, and the controls' hide
 * clock, armed while it was paused, used to run out before playback began, so
 * the controls stayed up and the film stayed composed until the mouse moved.
 *
 * The other half is that a film opens fullscreen with matching on: a
 * maximized window's title bar kept every frame composed.
 *
 * jsdom has no windows, so what is asserted is what the page tells the
 * client; the client's half is internal/webview2 (overlay_wake_windows_test.go)
 * and the TV.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback } from "@/playback/PlaybackProvider";
import { resetNativePlaybackAvailability, mpvBackend } from "@/playback/mpvBackend";
import { resetPrefs, setPrefs } from "@/playback/prefs";
import { Player } from "./Player";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

class NoLayoutResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}
(globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = NoLayoutResizeObserver;

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
let asks: [boolean, string][];
let entered: number;

function Probe() {
  pb = usePlayback();
  return null;
}

beforeEach(() => {
  asks = [];
  entered = 0;
  localStorage.clear();
  resetPrefs();
  resetNativePlaybackAvailability();

  window.lancastMpvAvailable = vi.fn(async () => true);
  window.lancastMpvFeatures = vi.fn(async () => []);
  window.lancastMpvOpen = vi.fn(async () => {});
  window.lancastMpvCommand = vi.fn(async () => {});
  window.lancastMpvStop = vi.fn(async () => {});
  window.lancastMpvLayout = vi.fn(async () => {});
  (window as { lancastEnterFullscreen?: () => Promise<boolean> }).lancastEnterFullscreen = vi.fn(
    async () => {
      entered++;
      return true;
    },
  );
  window.lancastMpvOverlayIdle = vi.fn(async (idle: boolean, keptBy: string) => {
    asks.push([idle, keptBy]);
  });

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
      if (url.includes("/playback")) return json({ decision: { method: "direct", reason: "" } });
      if (url.includes("/stream-ticket")) return json({ ticket: "tk", item_id: 9, expires_at: 0 });
      if (/\/api\/items\/9(\?|$)/.test(url))
        return json({
          id: 9,
          title: "Going in Style",
          kind: "movie",
          container: "mkv",
          duration_ms: 5_760_000,
          progress: { position_ms: 0, watched: false },
          streams: [
            { index: 0, kind: "video", codec: "h264" },
            { index: 1, kind: "audio", codec: "ac3", channels: 6, default: true },
          ],
        });
      if (url.includes("/api/auth")) return json({ user: { role: "admin" }, can_convert: true });
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  delete window.lancastMpvAvailable;
  delete window.lancastMpvFeatures;
  delete window.lancastMpvOpen;
  delete window.lancastMpvCommand;
  delete window.lancastMpvStop;
  delete window.lancastMpvLayout;
  delete window.lancastMpvOverlayIdle;
  delete (window as { lancastEnterFullscreen?: unknown }).lancastEnterFullscreen;
  resetNativePlaybackAvailability();
  resetPrefs();
  localStorage.clear();
});

async function settle(ms = 90) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

const report = (paused: boolean, t: number) =>
  act(async () =>
    mpvBackend().receive({
      events: paused ? ["loadedmetadata"] : ["play", "playing", "timeupdate"],
      current_time: t,
      duration: 5760,
      paused,
      ended: false,
    }),
  );

/** The film opens held, as a matched start does, and the controls are woken. */
async function openHeld() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <FocusProvider>
          <MemoryRouter initialEntries={["/play/9"]}>
            <PlaybackProvider>
              <Probe />
              <Player />
            </PlaybackProvider>
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
  await act(async () => pb.play(9, [9]));
  await settle();
  await report(true, 0);
  // Somebody reaching for the remote while the TV changes mode.
  await act(async () => {
    host.querySelector(".player")?.dispatchEvent(new MouseEvent("mousemove", { bubbles: true }));
  });
}

const idle = () => host.querySelector(".player")?.classList.contains("player--idle") ?? false;

describe("the page leaving the screen over a native film", () => {
  it("still fades the controls when a held start ends after the hide clock", async () => {
    await openHeld();
    // Longer than the hide clock, still held: nothing may hide over a paused film.
    await settle(2700);
    expect(pb.native).toBe(true);
    expect(idle()).toBe(false);
    expect(asks.at(-1)).toEqual([false, "controls"]);

    // The TV has settled and the film starts.
    await report(false, 0.1);
    await settle(2700);

    expect(idle()).toBe(true);
    expect(asks.at(-1)).toEqual([true, ""]);
  }, 10_000);

  it("brings the controls back, and hands on the key, when the client wakes it", async () => {
    await openHeld();
    await report(false, 0.1);
    await settle(2700);
    expect(asks.at(-1)).toEqual([true, ""]);

    const keys: string[] = [];
    const onKey = (e: KeyboardEvent) => keys.push(e.code);
    window.addEventListener("keydown", onKey);
    try {
      await act(async () => window.__lancastNativeWake?.({ key: "i", code: "KeyI", shiftKey: false }));
      await settle(30);
    } finally {
      window.removeEventListener("keydown", onKey);
    }

    expect(keys).toEqual(["KeyI"]);
    expect(idle()).toBe(false);
    // `i` opened the stats panel, so the page stays up for it as well.
    expect(asks.at(-1)?.[0]).toBe(false);
  }, 10_000);

  it("gives the page back to the window on leaving the player", async () => {
    await openHeld();
    await report(false, 0.1);
    await settle(2700);
    act(() => root.unmount());
    root = createRoot(host);
    expect(asks.at(-1)).toEqual([false, "left the player"]);
  }, 10_000);
});

describe("a film opening fullscreen", () => {
  it("asks the window for fullscreen once per film while matching is on", async () => {
    setPrefs({ matchRate: true });
    await openHeld();
    await report(false, 0.1);
    await report(false, 5);
    await settle(30);
    expect(entered).toBe(1);
  });

  it("leaves the window alone with matching off", async () => {
    await openHeld();
    await report(false, 0.1);
    expect(entered).toBe(0);
  });

  it("leaves the window alone when the person turned it off", async () => {
    setPrefs({ matchRate: true, fullscreenFilms: false });
    await openHeld();
    await report(false, 0.1);
    expect(entered).toBe(0);
  });
});
