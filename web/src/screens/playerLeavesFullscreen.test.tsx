/*
 * Leaving the player leaves fullscreen.
 *
 * Reported: a film in fullscreen, then back out to Home, and the window had no
 * minimize, maximize or close, and no way out of fullscreen short of starting
 * another film. In the desktop window fullscreen is the window itself —
 * borderless, the size of the monitor — and only the player screen has a
 * control for it. Escape already left fullscreen first; the player's close and
 * dock buttons, and any other navigation, did not.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route, useNavigate } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback } from "@/playback/PlaybackProvider";
import { Player } from "./Player";
import { WINDOW_HIDDEN } from "@/components/TrayBridge";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
let go: ReturnType<typeof useNavigate>;
/** The window's fullscreen, as the desktop binding keeps it. */
let windowFullscreen = false;

function Probe() {
  pb = usePlayback();
  go = useNavigate();
  return null;
}

const film = {
  id: 700,
  title: "A Film",
  kind: "movie",
  library_id: 1,
  duration_ms: 6_000_000,
  container: "matroska",
  artwork: {},
  streams: [{ index: 0, kind: "video", codec: "h264" }],
};

beforeEach(() => {
  windowFullscreen = false;
  const proto = window.HTMLMediaElement.prototype;
  proto.load = vi.fn();
  proto.play = vi.fn(async () => {});
  proto.pause = vi.fn();
  vi.stubGlobal(
    "lancastToggleFullscreen",
    vi.fn(async () => {
      windowFullscreen = !windowFullscreen;
      return windowFullscreen;
    }),
  );
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
      if (url.includes("/progress")) return new Response(null, { status: 204 });
      if (url.includes("/playback")) return json({ decision: { method: "direct", reason: "" } });
      if (/\/api\/items\/700(\?|$)/.test(url)) return json(film);
      if (url.includes("/api/auth")) return json({ user: { role: "admin" }, can_convert: true });
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function settle(ms = 60) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

describe("leaving the player", () => {
  it("takes the window out of fullscreen on the way to Home", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    await act(async () => {
      root.render(
        <QueryClientProvider client={qc}>
          <MemoryRouter initialEntries={["/watch/700"]}>
            <FocusProvider>
              <PlaybackProvider>
                <Probe />
                <Routes>
                  <Route path="/watch/:id" element={<Player />} />
                  <Route path="/" element={<div id="home" />} />
                </Routes>
              </PlaybackProvider>
            </FocusProvider>
          </MemoryRouter>
        </QueryClientProvider>,
      );
    });
    await settle();

    await act(async () => pb.toggleFullscreen());
    await settle();
    expect(windowFullscreen).toBe(true);

    // Not Escape: a plain navigation, as the close and dock buttons make.
    await act(async () => go("/"));
    await settle();

    expect(host.querySelector("#home")).not.toBeNull();
    expect(windowFullscreen, "the window was left borderless over Home").toBe(false);
  });

  /*
   * ✕ during a film stops it, and the player screen goes with it. Reported
   * after the close fix: reopening from the tray showed this screen black
   * with 0:00 / 0:00, because the film had stopped and the route had not.
   */
  it("leaves the player when the window is hidden during a film", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    await act(async () => {
      root.render(
        <QueryClientProvider client={qc}>
          <MemoryRouter initialEntries={["/", "/watch/700"]} initialIndex={1}>
            <FocusProvider>
              <PlaybackProvider>
                <Probe />
                <Routes>
                  <Route path="/watch/:id" element={<Player />} />
                  <Route path="/" element={<div id="home" />} />
                </Routes>
              </PlaybackProvider>
            </FocusProvider>
          </MemoryRouter>
        </QueryClientProvider>,
      );
    });
    await settle();
    expect(pb.itemID).toBe(700);

    await act(async () => {
      window.dispatchEvent(new Event(WINDOW_HIDDEN));
    });
    await settle();

    expect(host.querySelector("#home"), "still on the player screen").not.toBeNull();
    expect(pb.itemID, "the film is still loaded").toBe(0);
  });
});
