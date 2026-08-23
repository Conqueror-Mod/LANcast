/*
 * A live channel stays near its live edge.
 *
 * The rule lives in lib/liveEdge.ts and is tested there. This file asserts the
 * half that a correct rule says nothing about: that it is actually connected to
 * the element. This suite exists because of a settings shell whose panes were
 * not wired to its buttons, and the same failure is available here — a perfect
 * catch-up rule that no event ever calls is indistinguishable, from the sofa,
 * from not having written it.
 *
 * The fault being fixed, measured in the running app while the server was
 * measured at exactly 1.00x at the same moment:
 *
 *	0:48 / 1:14      play head 26s behind the incoming data
 *	1:23 / 2:17      54s behind, ~30s later
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LiveTV } from "./LiveTV";
import { PREROLL_SECONDS } from "@/lib/preroll";
import { TARGET_LAG_SECONDS } from "@/lib/liveEdge";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const channels = [
  {
    id: 1,
    source_id: 1,
    name: "Channel One",
    logo_url: null,
    group: "UK",
    position: 0,
    tvg_id: "one.example",
  },
];

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/guide"))
        return json({ at: 0, channels: {}, programs: [] });
      if (url.includes("/api/channels")) return json({ channels });
      return json({});
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

async function tick(ms = 400) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

/*
 * jsdom has no media pipeline: `buffered` is always empty and play/pause throw.
 * Unlike the rebuffer harness this makes `currentTime` writable, because
 * whether the player moves it is the entire subject here.
 */
function stubMedia(
  el: HTMLVideoElement,
  opts: { edge: () => number; at?: number; paused?: boolean },
) {
  const play = vi.fn(() => Promise.resolve());
  const pause = vi.fn();
  let now = opts.at ?? 0;
  Object.defineProperty(el, "play", { value: play, configurable: true });
  Object.defineProperty(el, "pause", { value: pause, configurable: true });
  Object.defineProperty(el, "paused", {
    configurable: true,
    get: () => opts.paused ?? false,
  });
  Object.defineProperty(el, "seeking", { configurable: true, get: () => false });
  Object.defineProperty(el, "currentTime", {
    configurable: true,
    get: () => now,
    set: (v: number) => {
      now = v;
    },
  });
  Object.defineProperty(el, "buffered", {
    configurable: true,
    get: () =>
      ({
        length: 1,
        start: () => 0,
        end: () => opts.edge(),
      }) as unknown as TimeRanges,
  });
  return { play, pause };
}

async function start(opts: {
  edge: () => number;
  at?: number;
  paused?: boolean;
}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <LiveTV />
      </QueryClientProvider>,
    );
  });
  await flush();

  const tile = host.querySelector<HTMLButtonElement>(".livetv__channel")!;
  await act(async () => {
    tile.click();
  });
  await flush();

  const el = host.querySelector("video")!;
  const media = stubMedia(el, opts);
  return { el, ...media };
}

describe("a live channel does not drift behind its edge", () => {
  /*
   * The accumulating half. Every drought used to resume wherever it stopped,
   * so on a provider that stalls several times a minute the play head walked
   * away from live and never came back.
   */
  it("resumes near the edge after a stall, not where it stopped", async () => {
    const { el, play } = await start({ edge: () => PREROLL_SECONDS + 60 });
    await tick();

    expect(play).toHaveBeenCalled();
    expect(el.currentTime).toBe(PREROLL_SECONDS + 60 - TARGET_LAG_SECONDS);
  });

  it("keeps a cushion rather than jumping to the very end", async () => {
    const edge = 100;
    const { el } = await start({ edge: () => edge });
    await tick();

    // Landing at the edge is the tempting answer and it guarantees the next
    // drought stalls at once, turning one jump into a permanent cycle.
    expect(el.currentTime).toBeLessThan(edge);
    expect(edge - el.currentTime).toBe(TARGET_LAG_SECONDS);
  });

  /*
   * The standing correction, for drift that arrives without a stall — a burst
   * outrunning playback, a throttled background tab, a machine that slept.
   */
  it("pulls the play head forward when it has drifted far behind", async () => {
    const { el } = await start({ edge: () => 137, at: 83 });
    await tick();
    // Put it back where the app was, then let a timeupdate arrive.
    el.currentTime = 83;

    await act(async () => {
      el.dispatchEvent(new Event("timeupdate"));
    });

    expect(el.currentTime).toBe(137 - TARGET_LAG_SECONDS);
  });

  it("leaves ordinary buffering alone", async () => {
    const { el } = await start({ edge: () => 100, at: 92 });
    await tick();
    el.currentTime = 92; // 8s behind: the measured drought, not drift

    await act(async () => {
      el.dispatchEvent(new Event("timeupdate"));
    });

    expect(el.currentTime).toBe(92);
  });

  /*
   * A paused element is somebody's decision. Yanking it forward would override
   * that — and the correction still happens on the next tick after they press
   * play, which is the right moment for a live channel to return to live.
   */
  it("does not yank a paused player forward", async () => {
    const { el } = await start({ edge: () => 200, at: 10, paused: true });
    await tick();
    el.currentTime = 10;

    await act(async () => {
      el.dispatchEvent(new Event("timeupdate"));
    });

    expect(el.currentTime).toBe(10);
  });
});
