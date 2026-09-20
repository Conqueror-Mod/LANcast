/*
 * The desktop asks before it plays (ADR 0067 phase 4).
 *
 * Native playback opens the file itself, so for most of a library the server
 * has nothing to say. It still has one thing: a **quality ceiling** set by
 * whoever runs the server is policy about what may leave it, not a statement
 * about what a client can decode. While the desktop skipped the question
 * entirely, that setting sat on the settings screen doing nothing for the one
 * client most likely to be pointed outside the house.
 *
 * So it asks under `?profile=native`, and when the answer is anything but
 * direct play the item goes to the browser player — the engine that reads the
 * server's conversions.
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

// jsdom performs no layout and has no ResizeObserver; the provider uses one to
// keep the native video window in step with the page.
class NoLayoutResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}
(globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver =
  NoLayoutResizeObserver;

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
let opened: number[] = [];
let asked: string[] = [];
/** What the server says about the next item asked about. */
let decision = { method: "direct", reason: "" };
let elementSources: string[] = [];

function Probe() {
  pb = usePlayback();
  return <span>{pb.itemID}</span>;
}

function itemBody(id: number) {
  return {
    id,
    title: "Dogma",
    kind: "movie",
    duration_ms: 7_741_000,
    progress: { position_ms: 0, watched: false },
    streams: [
      { index: 0, kind: "video", codec: "hevc" },
      { index: 1, kind: "audio", codec: "truehd", language: "eng" },
    ],
  };
}

beforeEach(() => {
  opened = [];
  asked = [];
  elementSources = [];
  decision = { method: "direct", reason: "" };
  localStorage.clear();
  resetNativePlaybackAvailability();

  window.lancastMpvAvailable = vi.fn(async () => true);
  window.lancastMpvOpen = vi.fn(async (id: number) => {
    opened.push(id);
  });
  window.lancastMpvCommand = vi.fn(async () => {});
  window.lancastMpvStop = vi.fn(async () => {});
  window.lancastMpvLayout = vi.fn(async () => {});

  const proto = window.HTMLMediaElement.prototype;
  Object.defineProperty(proto, "src", {
    configurable: true,
    set(v: string) {
      elementSources.push(v);
      this.setAttribute("src", v);
    },
    get() {
      return this.getAttribute("src") ?? "";
    },
  });
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
      if (url.includes("/playback")) {
        asked.push(url);
        return json({ decision });
      }
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

async function settle(ms = 90) {
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

describe("native playback and the server's answer", () => {
  it("asks as a native client and plays the file itself when told to", async () => {
    await mount();
    await act(async () => pb.play(7021, [7021]));
    await settle();

    expect(asked.some((u) => u.includes("profile=native"))).toBe(true);
    expect(opened).toEqual([7021]);
  });

  it("hands the item to the browser player when the server says convert", async () => {
    decision = { method: "transcode", reason: "video is 2160p, above the 1080p limit" };
    await mount();
    await act(async () => pb.play(7021, [7021]));
    await settle();

    expect(asked.some((u) => u.includes("profile=native"))).toBe(true);
    // Nothing was opened natively, and the element was given a source.
    expect(opened).toEqual([]);
    expect(elementSources.length).toBeGreaterThan(0);
  });
});
