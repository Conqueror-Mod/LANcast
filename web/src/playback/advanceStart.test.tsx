/*
 * The next item starts from its own place, not the previous item's.
 *
 * Seen on a real season: an episode resumed in its credits at 1301s, and every
 * episode after it started at 1301s too — each played fifty seconds of credits,
 * was marked watched, and advanced, until one shorter than 1301s could not be
 * started at all. Four episodes were marked watched that nobody had seen.
 *
 * The mechanism is a gap. When the queue advances, the provider's item id moves
 * on at once, but the element keeps playing the old stream until the next
 * item's details have loaded and its source is assigned. A `timeupdate` from
 * the old stream in that gap was recorded as the live position of the *new*
 * item, and the source effect trusted it as "already playing this".
 *
 * Driven through the real provider, because the fault lives in the wiring
 * between a ref, an event handler's closure and an async fetch.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback } from "./PlaybackProvider";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
let sources: string[] = [];
/** Held back so the gap between advancing and the next item loading is real. */
let releaseItem2: (() => void) | null = null;

function Probe() {
  pb = usePlayback();
  return <span data-testid="item">{pb.itemID}</span>;
}

// Episode one is part-watched and resumes in its credits; episode two is fresh.
function itemBody(id: number) {
  return {
    id,
    title: `Episode ${id}`,
    kind: "episode",
    duration_ms: 1_353_000,
    progress:
      id === 1
        ? { position_ms: 1_301_000, watched: false }
        : { position_ms: 0, watched: false },
    media_streams: [
      { index: 0, kind: "video", codec: "hevc" },
      { index: 1, kind: "audio", codec: "aac" },
    ],
  };
}

beforeEach(() => {
  sources = [];
  releaseItem2 = null;
  const proto = window.HTMLMediaElement.prototype;
  Object.defineProperty(proto, "src", {
    configurable: true,
    get() {
      return this.getAttribute("src") ?? "";
    },
    set(v: string) {
      sources.push(v);
      this.setAttribute("src", v);
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
        return json({ decision: { method: "transcode", reason: "hevc" } });
      }
      const item = url.match(/\/api\/items\/(\d+)(\?|$)/);
      if (item) {
        const id = Number(item[1]);
        if (id === 2) {
          // The next episode's details arrive only when the test says so.
          await new Promise<void>((r) => (releaseItem2 = r));
        }
        return json(itemBody(id));
      }
      if (url.includes("/api/auth")) {
        return json({ user: { role: "admin" }, can_convert: true });
      }
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  releaseItem2?.();
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function settle(ms = 30) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter>
              <Probe />
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

function streamsFor(id: number) {
  return sources.filter((s) => s.includes(`/api/stream/${id}`));
}

describe("advancing to the next item", () => {
  it("starts the next item from its own place, not the last one's", async () => {
    await render();
    await act(async () => {
      pb.play(1, [1, 2]);
    });
    await settle(60);
    // Episode one resumed in its credits, as it did in the field.
    expect(streamsFor(1).at(-1)).toContain("t=1301");

    await act(async () => {
      pb.playNext();
    });
    await settle();

    // The gap: the old stream is still playing and reports its position while
    // episode two's details have not arrived.
    const v = document.querySelector("video")!;
    Object.defineProperty(v, "currentTime", { configurable: true, value: 50 });
    await act(async () => {
      v.dispatchEvent(new Event("timeupdate"));
    });

    await act(async () => {
      releaseItem2?.();
    });
    await settle(60);

    const next = streamsFor(2).at(-1) ?? "";
    expect(next, "episode two was never requested").not.toBe("");
    expect(
      next,
      "episode two was started at episode one's position — the credits of every " +
        "episode after it, each marked watched",
    ).not.toMatch(/[?&]t=1[23]\d\d\b/);
    expect(next).not.toMatch(/[?&]t=/);
  });
});
