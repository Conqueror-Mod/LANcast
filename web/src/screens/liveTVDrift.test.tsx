/*
 * A live channel does not drift behind its edge, and never seeks to fix it.
 *
 * The rule lives in lib/liveEdge.ts and is tested there. This file asserts the
 * half a correct rule says nothing about: that it is connected to the element,
 * and that the element is driven the way this transport can survive.
 *
 * Both halves have failed here already. The first version had a rule that was
 * right and a constant that contradicted the one preroll uses, so every stall
 * gave back part of the cushion it had just waited for. The second seeked a
 * stream the browser cannot range-request, and stranded it: 22 seconds
 * buffered, play head at 0:00, play doing nothing.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LiveTV } from "./LiveTV";
import { PREROLL_SECONDS } from "@/lib/preroll";
import { CATCHUP_RATE, MAX_LAG_SECONDS } from "@/lib/liveEdge";

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
 * currentTime is writable so an attempt to seek would be *visible* rather than
 * throwing — a test that cannot observe the wrong behaviour cannot forbid it.
 */
function stubMedia(
  el: HTMLVideoElement,
  opts: { edge: () => number; at?: number; paused?: boolean },
) {
  const play = vi.fn(() => Promise.resolve());
  const pause = vi.fn();
  let now = opts.at ?? 0;
  let rate = 1;
  Object.defineProperty(el, "play", { value: play, configurable: true });
  Object.defineProperty(el, "pause", { value: pause, configurable: true });
  Object.defineProperty(el, "paused", {
    configurable: true,
    get: () => opts.paused ?? false,
  });
  Object.defineProperty(el, "currentTime", {
    configurable: true,
    get: () => now,
    set: (v: number) => {
      now = v;
    },
  });
  Object.defineProperty(el, "playbackRate", {
    configurable: true,
    get: () => rate,
    set: (v: number) => {
      rate = v;
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

async function timeupdate(el: HTMLVideoElement) {
  await act(async () => {
    el.dispatchEvent(new Event("timeupdate"));
  });
}

describe("a live channel closes its gap by playing faster", () => {
  it("speeds up when the play head has drifted far behind", async () => {
    const { el } = await start({ edge: () => 137, at: 83 }); // the 54s gap
    await tick();
    await timeupdate(el);

    expect(el.playbackRate).toBe(CATCHUP_RATE);
  });

  it("returns to normal once the gap is closed", async () => {
    let head = 83;
    const { el } = await start({ edge: () => 137, at: head });
    await tick();
    await timeupdate(el);
    expect(el.playbackRate).toBe(CATCHUP_RATE);

    head = 135; // caught up
    el.currentTime = head;
    await timeupdate(el);

    expect(el.playbackRate).toBe(1);
  });

  it("leaves ordinary buffering at normal speed", async () => {
    // 8s behind: the head start preroll waits for, not drift.
    const { el } = await start({ edge: () => 100, at: 92 });
    await tick();
    el.currentTime = 92;
    await timeupdate(el);

    expect(el.playbackRate).toBe(1);
  });

  /*
   * The failure that stranded a channel. The live endpoint cannot be
   * range-requested, so moving the play head is the one thing this must never
   * do — a miss leaves the element waiting for bytes nobody will fetch.
   */
  it("never moves the play head", async () => {
    const { el } = await start({ edge: () => 500, at: 10 });
    await tick();
    const before = el.currentTime;
    await timeupdate(el);
    await timeupdate(el);

    expect(el.currentTime).toBe(before);
  });

  it("does not touch a paused player", async () => {
    const { el } = await start({ edge: () => 500, at: 10, paused: true });
    await tick();
    await timeupdate(el);

    expect(el.playbackRate).toBe(1);
    expect(el.currentTime).toBe(10);
  });

  /*
   * A viewer can choose their own speed from the player's own menu. Resetting
   * that on the next tick would make the control look broken, so the rate we
   * did not set is not ours to clear.
   */
  it("leaves a speed the viewer chose alone", async () => {
    const { el } = await start({ edge: () => 30, at: 25 }); // little lag
    await tick();
    el.playbackRate = 1.5;
    await timeupdate(el);

    expect(el.playbackRate).toBe(1.5);
  });

  /*
   * The measured fault: `waiting` fires about once a second on a real channel
   * while the element holds two minutes of media. Pausing on each one kept the
   * player paused 28% of wall time and dragged playback to 0.76x.
   */
  it("ignores a stall it already has the media to ride out", async () => {
    const { el, pause } = await start({ edge: () => 140, at: 8 });
    await tick();
    pause.mockClear();

    await act(async () => {
      el.dispatchEvent(new Event("waiting"));
    });
    await tick();

    expect(pause).not.toHaveBeenCalled();
    expect(host.querySelector(".livetv__buffering")).toBeNull();
  });

  it("still holds when the cushion is genuinely gone", async () => {
    // Derived, not written: this was hard-coded to the value PREROLL_SECONDS
    // happened to have, and went stale the moment the measurement moved it.
    let edge = PREROLL_SECONDS + 2;
    const { el, pause } = await start({ edge: () => edge, at: 0 });
    await tick();
    pause.mockClear();

    edge = 0.2; // the buffer really did run dry
    await act(async () => {
      el.dispatchEvent(new Event("waiting"));
    });
    await tick();

    expect(pause).toHaveBeenCalled();
  });

  /*
   * A speed change a viewer can hear must not be a secret. 10% is subtle
   * enough to be mistaken for the stream being wrong rather than for a
   * correction being applied — which is precisely how it was first reported.
   */
  it("says so on screen while it is running fast", async () => {
    const { el } = await start({ edge: () => 137, at: 83 });
    await tick();
    expect(host.textContent).not.toContain("Catching up");

    await timeupdate(el);
    expect(el.playbackRate).toBe(CATCHUP_RATE);
    expect(host.textContent).toContain("Catching up");
  });

  it("stops saying so once it is back to normal speed", async () => {
    const { el } = await start({ edge: () => 137, at: 83 });
    await tick();
    await timeupdate(el);
    expect(host.textContent).toContain("Catching up");

    el.currentTime = 135; // caught up
    await timeupdate(el);

    expect(el.playbackRate).toBe(1);
    expect(host.textContent).not.toContain("Catching up");
  });

  it("still resumes after a stall", async () => {
    const { play } = await start({ edge: () => PREROLL_SECONDS + 2 });
    await tick();
    expect(play).toHaveBeenCalled();
  });

  it("engages only past the threshold", async () => {
    const { el } = await start({
      edge: () => MAX_LAG_SECONDS + 100,
      at: 100,
    });
    await tick();
    el.currentTime = 100; // lag is 20s exactly
    await timeupdate(el);
    expect(el.playbackRate).toBe(1);
  });
});
