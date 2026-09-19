/*
 * Every film in a row gets told where the picture goes (ADR 0067).
 *
 * Reported as "Randomize all is a bit broken and doesn't play most videos,
 * but the same film played straight from the library plays instantly". Both
 * halves were true and the cause was layout, not playback: stopping one source
 * to open the next hid the client's video window, and the page only re-sent a
 * layout when its *own* layout changed — which one film following another is
 * not. So mpv played, with sound, to a hidden window; going back to the library
 * and picking a film changed the surface, which sent a layout again and made it
 * look like the direct route was the working one.
 *
 * The fix has two halves and this holds the page's: a file opening re-asserts
 * the layout. The client's half is that stopping no longer moves the window at
 * all (cmd/lancast/player_windows.go).
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback, useFullSurface } from "./PlaybackProvider";
import { resetNativePlaybackAvailability } from "./mpvBackend";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
let layouts: string[] = [];
let opened: number[] = [];

function Probe() {
  pb = usePlayback();
  // The player screen claims the full surface; without it the provider is in
  // its docked layout and this test would be about a different rectangle.
  useFullSurface();
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
  window.lancastMpvLayout = vi.fn(async (layout: string) => {
    layouts.push(layout);
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

describe("native video layout across a queue", () => {
  it("tells the client where the picture goes for the second film too", async () => {
    await mount();

    // The shuffled library, played in order — the Randomize all path.
    await act(async () => pb.play(101, [101, 102]));
    await settle();
    await act(async () => {
      clientOpened(101);
    });
    await settle();

    expect(opened).toEqual([101]);
    expect(layouts).toContain("full");
    const afterFirst = layouts.length;

    await act(async () => pb.play(102, [101, 102]));
    await settle();
    await act(async () => {
      clientOpened(102);
    });
    await settle();

    expect(opened).toEqual([101, 102]);
    // The assertion the bug failed: without re-asserting on open, the surface
    // has not changed, nothing is sent, and the film plays to a hidden window.
    expect(layouts.length).toBeGreaterThan(afterFirst);
    expect(layouts.at(-1)).toBe("full");
  });
});
