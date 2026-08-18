/*
 * Rebuffering on a live channel.
 *
 * preroll.ts measured what a live source actually does: bytes in bursts, with
 * five-second silences between them. The cushion that absorbs those silences
 * was built once, at start, and never again — so the first drought deep enough
 * to empty it left playback running permanently thin, and every later gap
 * reached the decoder. It reads as judder, not as buffering, because the
 * spinner had already been dismissed.
 *
 * These assert the re-arm: that `waiting` puts the player back into a hold, and
 * that the hold is a real one rather than an instant resume against whatever
 * scrap of buffer triggered it.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LiveTV } from "./LiveTV";
import { PREROLL_SECONDS } from "@/lib/preroll";

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
      if (url.includes("/guide")) return json({ at: 0, channels: {}, programs: [] });
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

/* Lets one test decide how much media the element is holding. jsdom has no
 * media pipeline at all: `buffered` is always empty and play/pause throw, so
 * the element has to be told what to claim. */
function stubMedia(el: HTMLVideoElement, aheadSeconds: () => number) {
  const play = vi.fn(() => Promise.resolve());
  const pause = vi.fn();
  Object.defineProperty(el, "play", { value: play, configurable: true });
  Object.defineProperty(el, "pause", { value: pause, configurable: true });
  Object.defineProperty(el, "currentTime", { value: 0, configurable: true });
  Object.defineProperty(el, "buffered", {
    configurable: true,
    get: () =>
      ({
        length: 1,
        start: () => 0,
        end: () => aheadSeconds(),
      }) as unknown as TimeRanges,
  });
  return { play, pause };
}

// Longer than the effect's 250ms poll, short enough not to slow the suite.
async function tick(ms = 400) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

async function start(ahead: () => number) {
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
  const media = stubMedia(el, ahead);
  return { el, ...media };
}

function buffering() {
  return host.querySelector(".livetv__buffering") !== null;
}

describe("live tv rebuffering", () => {
  it("starts once the cushion is there", async () => {
    const { play } = await start(() => PREROLL_SECONDS + 2);
    await tick();

    expect(play).toHaveBeenCalledTimes(1);
    expect(buffering()).toBe(false);
  });

  /*
   * The assertion this file exists for.
   *
   * Without the re-arm nothing listens to `waiting` at all: the browser resumes
   * itself on the next scrap of data, the spinner never returns, and the
   * channel judders from then on.
   */
  it("holds again when the stream runs dry", async () => {
    let ahead = PREROLL_SECONDS + 2;
    const { el, play, pause } = await start(() => ahead);
    await tick();
    expect(play).toHaveBeenCalledTimes(1);

    // The drought: the buffer is gone and the element says so.
    ahead = 0.2;
    await act(async () => {
      el.dispatchEvent(new Event("waiting"));
    });

    expect(pause).toHaveBeenCalled();
    expect(buffering()).toBe(true);

    // Still thin — resuming here is the bug, not the fix.
    await tick();
    expect(play).toHaveBeenCalledTimes(1);
    expect(buffering()).toBe(true);
  });

  it("resumes once the cushion is rebuilt", async () => {
    let ahead = PREROLL_SECONDS + 2;
    const { el, play } = await start(() => ahead);
    await tick();

    ahead = 0.2;
    await act(async () => {
      el.dispatchEvent(new Event("waiting"));
    });
    await tick();
    expect(play).toHaveBeenCalledTimes(1);

    ahead = PREROLL_SECONDS + 2;
    await tick();
    expect(play).toHaveBeenCalledTimes(2);
    expect(buffering()).toBe(false);
  });

  /*
   * `waiting` fires repeatedly on a stalling channel. Restarting the clock on
   * each one would push the deadline out for ever, which is the failure the
   * deadline exists to prevent.
   */
  it("does not restart the deadline clock on a repeated stall", async () => {
    let ahead = PREROLL_SECONDS + 2;
    const { el, pause } = await start(() => ahead);
    await tick();

    ahead = 0.2;
    for (let i = 0; i < 3; i++) {
      await act(async () => {
        el.dispatchEvent(new Event("waiting"));
      });
    }
    // One hold, not three: the second and third are absorbed rather than
    // starting fresh timers that each own a deadline.
    expect(pause).toHaveBeenCalledTimes(1);
    expect(buffering()).toBe(true);
  });
});
