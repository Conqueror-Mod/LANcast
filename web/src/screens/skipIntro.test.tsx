/*
 * The Skip intro button, wired to a real playhead.
 *
 * The rule is pure and tested next door in lib/skip.test.ts. This is the half a
 * correct rule says nothing about: that it is connected to the item's markers
 * and to the element, that pressing it seeks where it said, and that it goes
 * away again.
 *
 * That seam is where this project's client bugs live. A settings shell whose
 * panes were not wired to its buttons, a context menu whose two items emptied
 * the same shelf — both had correct logic underneath.
 *
 * jsdom performs no layout, so nothing here is about where the button sits.
 * What it proves is when it exists and what it does.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback } from "@/playback/PlaybackProvider";
import { Player } from "./Player";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let seeked: number[];
let pb: ReturnType<typeof usePlayback>;

// The player takes its item from provider state, not the URL, so a harness has
// to ask it to play something the way the app does.
function Probe() {
  pb = usePlayback();
  return null;
}

/** An episode with a detected intro from 87s to 117s. */
function episode() {
  return {
    id: 7,
    title: "The Gang Gets Analyzed",
    kind: "episode",
    duration_ms: 1_320_000,
    progress: { position_ms: 0, watched: false },
    media_streams: [
      { index: 0, kind: "video", codec: "h264" },
      { index: 1, kind: "audio", codec: "aac", language: "eng" },
    ],
    markers: [
      {
        kind: "intro",
        start_ms: 87_000,
        end_ms: 117_000,
        source: "fingerprint",
        confidence: 0.9,
        created_at: 0,
      },
    ],
  };
}

beforeEach(() => {
  seeked = [];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);

  const proto = window.HTMLMediaElement.prototype;
  proto.play = vi.fn(async () => {});
  proto.pause = vi.fn();
  proto.load = vi.fn();
  let now = 0;
  Object.defineProperty(proto, "currentTime", {
    configurable: true,
    get: () => now,
    set: (v: number) => {
      seeked.push(v);
      now = v;
    },
  });
  Object.defineProperty(proto, "duration", { configurable: true, get: () => 1320 });

  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (String(url).includes("/playback")) {
        return json({ decision: { method: "direct", reason: "" } });
      }
      if (/\/api\/items\/\d+(\?|$)/.test(String(url))) return json(episode());
      if (String(url).includes("/api/auth")) {
        return json({ user: { role: "admin" }, can_convert: true });
      }
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  host.remove();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 5));
  });
}

async function render() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter initialEntries={["/play/7"]}>
            <PlaybackProvider>
              <Probe />
              <Player />
            </PlaybackProvider>
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await flush();
  await act(async () => pb.play(7, []));
  await flush();
}

/** Move the playhead and let the element tell the player about it. */
async function at(seconds: number) {
  const el = host.querySelector("video");
  if (!el) return;
  (el as HTMLVideoElement).currentTime = seconds;
  seeked.pop(); // this move is the test's, not the button's
  await act(async () => {
    el.dispatchEvent(new Event("timeupdate"));
  });
  await flush();
}

const skipButton = () =>
  [...host.querySelectorAll("button")].find(
    (b) => b.textContent?.trim() === "Skip intro",
  );

describe("skip intro", () => {
  it("offers nothing before the intro", async () => {
    await render();
    await at(40);
    expect(skipButton()).toBeUndefined();
  });

  it("offers the skip once the intro is playing", async () => {
    await render();
    await at(95);
    expect(skipButton()).toBeDefined();
  });

  /*
   * A button that appears, never a jump that happens.
   *
   * The backlog and ADR 0054 both require this: an automatic skip a few seconds
   * wrong is indistinguishable from a broken file, and the first thing it eats
   * is a cold open. So reaching the intro must move nothing on its own.
   */
  it("does not skip on its own", async () => {
    await render();
    await at(95);
    expect(seeked).toHaveLength(0);
  });

  it("seeks to the end of the intro when pressed, with nothing added", async () => {
    await render();
    await at(95);
    await act(async () => skipButton()!.click());
    await flush();

    expect(seeked).toEqual([117]);
  });

  it("goes away once the intro is behind us", async () => {
    await render();
    await at(95);
    expect(skipButton()).toBeDefined();
    await at(130);
    expect(skipButton()).toBeUndefined();
  });

  // An item with no markers is the ordinary case and must be quiet: most of a
  // library has never been examined.
  it("offers nothing for an item with no markers", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        const json = (v: unknown) =>
          new Response(JSON.stringify(v), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        if (String(url).includes("/playback")) {
          return json({ decision: { method: "direct", reason: "" } });
        }
        if (/\/api\/items\/\d+(\?|$)/.test(String(url))) {
          const e = episode();
          delete (e as { markers?: unknown }).markers;
          return json(e);
        }
        if (String(url).includes("/api/auth")) {
          return json({ user: { role: "admin" }, can_convert: true });
        }
        return json({ items: [], total: 0 });
      }),
    );
    await render();
    await at(95);
    expect(skipButton()).toBeUndefined();
  });
});
