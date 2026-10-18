/*
 * Now Playing: a track's full screen (docs/now-playing-plan.md, step 1).
 *
 * The real provider and the real Player, in a browser tab. What is asserted is
 * what the column says and does: the track named on the page and not twice,
 * the queue and the lyrics as tabs rather than popovers, a pick from the queue
 * playing without closing anything, and the film's own queue popover left
 * where it was.
 *
 * jsdom performs no layout, so nothing here says the column sits beside the
 * cover, that the two line up, or that the queue follows the playing row. That
 * is checked by looking, at desktop width and at 375px.
 */
import { describe, it, expect, beforeAll, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, useLocation } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback, resetMusicDeckForTests } from "@/playback/PlaybackProvider";
import { resetPrefs } from "@/playback/prefs";
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

// jsdom has no scrolling; the lyrics keep their line in view with it.
beforeAll(() => {
  Element.prototype.scrollIntoView = () => {};
});

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
let path = "";

function Probe() {
  pb = usePlayback();
  path = useLocation().pathname;
  return null;
}

const ALBUM = 50;
const track = (id: number) => ({
  id,
  title: `Song ${id}`,
  kind: "track",
  parent_id: ALBUM,
  artist: "The Band",
  series: "The Record",
  // Tracks carry no year here, so the album's is the one shown.
  year: null,
  duration_ms: 200_000,
  progress: { position_ms: 0, watched: false },
  streams: [{ index: 0, kind: "audio", codec: "mp3", channels: 2 }],
});
const film = (id: number) => ({
  id,
  title: `Film ${id}`,
  kind: "movie",
  container: "mp4",
  duration_ms: 6_000_000,
  progress: { position_ms: 0, watched: false },
  streams: [
    { index: 0, kind: "video", codec: "h264" },
    { index: 1, kind: "audio", codec: "aac", channels: 2, default: true },
  ],
});

beforeEach(() => {
  localStorage.clear();
  resetPrefs();
  resetMusicDeckForTests();
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
      const json = (b: unknown) =>
        new Response(JSON.stringify(b), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.includes("/playback")) return json({ decision: { method: "direct", reason: "" } });
      if (url.includes("/subtitles")) return json({ tracks: [] });
      if (url.includes("/lyrics"))
        return json({
          synced: true,
          lines: [
            { at_ms: 0, text: "First line" },
            { at_ms: 4000, text: "Second line" },
          ],
        });
      const m = url.match(/\/api\/items\/(\d+)(\?|$)/);
      if (m) {
        const id = Number(m[1]);
        if (id === ALBUM) return json({ id: ALBUM, title: "The Record", kind: "album", year: 1994 });
        return json(id >= 90 ? film(id) : track(id));
      }
      if (url.includes("/api/auth")) return json({ user: { role: "admin" }, can_convert: true });
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  resetPrefs();
  localStorage.clear();
});

async function settle(ms = 60) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

async function playing(id: number, queue: number[]) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <FocusProvider>
          <MemoryRouter initialEntries={["/play"]}>
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
  await act(async () => pb.play(id, queue));
  await settle(120);
}

const column = () => host.querySelector<HTMLElement>("section.np");
const tab = (name: string) =>
  [...host.querySelectorAll<HTMLButtonElement>('[role="tab"]')].find((t) => t.textContent === name);
const button = (name: RegExp) =>
  [...host.querySelectorAll("button")].find((b) => name.test(b.getAttribute("aria-label") ?? ""));
const press = async (b: HTMLElement | undefined | null) => {
  await act(async () => b?.click());
  await settle(40);
};

describe("Now Playing on a track", () => {
  it("names the track on the page, with artist, album and the album's year", async () => {
    await playing(1, [1, 2, 3]);
    const np = column();
    expect(np).not.toBeNull();
    expect(np!.querySelector("h1")?.textContent).toBe("Song 1");
    expect(np!.querySelector(".np__meta")?.textContent).toBe("The Band — The Record · 1994");
  });

  it("does not name it a second time in the top bar", async () => {
    await playing(1, [1, 2, 3]);
    expect(host.querySelector(".player__top .player__title")).toBeNull();
  });

  it("opens on the queue, in the order that will play", async () => {
    await playing(1, [1, 2, 3]);
    expect(tab("Queue")?.getAttribute("aria-selected")).toBe("true");
    const rows = [...column()!.querySelectorAll(".queue__row")];
    expect(rows.map((r) => r.querySelector(".queue__title")?.textContent)).toEqual([
      "Song 1",
      "Song 2",
      "Song 3",
    ]);
    expect(rows[0].getAttribute("aria-current")).toBe("true");
  });

  it("plays a pick from the queue and keeps the column", async () => {
    await playing(1, [1, 2, 3]);
    const rows = column()!.querySelectorAll<HTMLButtonElement>(".queue__row");
    await press(rows[2]);
    expect(pb.itemID).toBe(3);
    expect(column()!.querySelector(".queue")).not.toBeNull();
    await settle();
    expect(column()!.querySelector("h1")?.textContent).toBe("Song 3");
  });

  it("shows the lyrics as the other tab, with no close button", async () => {
    await playing(1, [1, 2, 3]);
    await press(tab("Lyrics"));
    expect(tab("Lyrics")?.getAttribute("aria-selected")).toBe("true");
    expect(column()!.querySelector(".queue")).toBeNull();
    const lines = [...column()!.querySelectorAll(".lyrics__line")].map((l) => l.textContent);
    expect(lines).toEqual(["First line", "Second line"]);
    expect(button(/^Close lyrics$/)).toBeUndefined();
  });

  it("offers no queue tab for a single track with nothing behind it", async () => {
    await playing(1, [1]);
    expect(tab("Queue")).toBeUndefined();
    expect(tab("Lyrics")?.getAttribute("aria-selected")).toBe("true");
  });

  it("has dropped the lyrics and queue popovers from the bar", async () => {
    await playing(1, [1, 2, 3]);
    expect(button(/^Lyrics$/)).toBeUndefined();
    expect(button(/^Queue$/)).toBeUndefined();
    // Shuffle and repeat are transport, and stay.
    expect(button(/^Shuffle$/)).toBeDefined();
  });

  it("goes to the album from the meta line, leaving the track playing", async () => {
    await playing(1, [1, 2, 3]);
    await press(column()!.querySelector<HTMLButtonElement>(".np__album"));
    expect(path).toBe(`/item/${ALBUM}`);
    expect(pb.itemID).toBe(1);
  });

  it("marks the chosen tab without gold", async () => {
    await playing(1, [1, 2, 3]);
    const on = tab("Queue")!;
    expect(on.className).toContain("is-on");
    expect(on.className).not.toMatch(/gold/);
  });
});

describe("a film", () => {
  it("has no Now Playing column, and keeps its queue popover", async () => {
    await playing(91, [91, 92]);
    expect(column()).toBeNull();
    expect(button(/^Queue$/)).toBeDefined();
    expect(host.querySelector(".player__top .player__title")?.textContent).toBe("Film 91");
  });
});
