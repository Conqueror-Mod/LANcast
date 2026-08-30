/*
 * A direct play that drops a fifth of its frames.
 *
 * Measured on the reporting install: two HEVC Main 10 films dropped 19.8% and
 * 19.9% of their frames, while an H.264 film from the same folder dropped none.
 * Reported as *heavy frame lag*.
 *
 * Nothing could have predicted it — `canPlayType` says "probably" for HEVC Main
 * 10 and `mediaCapabilities.decodingInfo()` returned smooth **and**
 * power-efficient for the exact shape of the failing file. So it is caught by
 * watching the counters, and this is the half the pure rule cannot prove: that
 * the player acts on the verdict, withdraws the right claim, and does not send
 * the viewer back to the beginning to do it.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback } from "./PlaybackProvider";
import { clearDenials, deniedCapabilities } from "./capabilities";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
let sources: string[] = [];

/** The element's decode counters, driven by the test. */
let counters = { total: 0, dropped: 0 };

function Probe() {
  pb = usePlayback();
  return <span data-testid="item">{pb.itemID}</span>;
}

/** An HEVC Main 10 film the server is happy to direct-play. */
function itemBody(id: number) {
  return {
    id,
    title: "I Still Know",
    kind: "movie",
    duration_ms: 6_000_000,
    progress: { position_ms: 0, watched: false },
    // `streams`, not `media_streams`: capabilitiesNeededBy reads Item.streams,
    // and a fixture using the wrong name silently denies nothing at all.
    streams: [
      { index: 0, kind: "video", codec: "hevc", profile: "Main 10" },
      { index: 1, kind: "audio", codec: "aac", language: "eng" },
    ],
  };
}

beforeEach(() => {
  sources = [];
  counters = { total: 0, dropped: 0 };
  localStorage.clear();
  clearDenials();

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
  Object.defineProperty(proto, "readyState", {
    configurable: true,
    get: () => 4,
  });
  Object.defineProperty(proto, "paused", { configurable: true, get: () => false });
  Object.defineProperty(proto, "currentTime", {
    configurable: true,
    get: () => 1800, // half an hour in, which is where this fires in real life
    set: () => {},
  });
  // On HTMLVideoElement rather than HTMLMediaElement, which is where it really
  // lives — the elements under test are videos, so the prototype they inherit
  // from is the media one and the cast is what lets it be planted there.
  (proto as unknown as HTMLVideoElement).getVideoPlaybackQuality = () =>
    ({
      totalVideoFrames: counters.total,
      droppedVideoFrames: counters.dropped,
      corruptedVideoFrames: 0,
      creationTime: 0,
    }) as VideoPlaybackQuality;
  proto.load = vi.fn();
  proto.play = vi.fn(async () => {});
  proto.pause = vi.fn();
  proto.canPlayType = vi.fn(() => "probably") as unknown as typeof proto.canPlayType;

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
      // The server direct-plays it, because the client claims hevc10.
      if (url.includes("/playback")) {
        return json({ decision: { method: "direct", reason: "" } });
      }
      const item = url.match(/\/api\/items\/(\d+)(\?|$)/);
      if (item) return json(itemBody(Number(item[1])));
      if (url.includes("/api/auth")) {
        return json({ user: { role: "admin" }, can_convert: true });
      }
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  clearDenials();
  localStorage.clear();
});

async function settle(ms = 60) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
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
          <PlaybackProvider>
            <MemoryRouter>
              <Probe />
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle(20);
}

const streams = () => sources.filter((s) => s.includes("/api/stream"));

async function start() {
  await render();
  await act(async () => {
    pb.play(1, [1]);
  });
  await settle(120);
  /*
   * The sampler only runs while the provider believes playback is under way,
   * and that belief comes from the element's `play` event — which a mocked
   * play() never fires. Dispatching it is what makes this a test of the
   * sampler rather than of the mock.
   */
  const v = host.querySelector("video");
  if (v) {
    await act(async () => {
      v.dispatchEvent(new Event("play"));
    });
  }
  await settle(30);
}

/**
 * Advance the counters and the clock together, in the shape real playback has.
 *
 * `dropRate` of 0.2 is what was measured on the failing files; 0 is the H.264
 * control from the same folder.
 */
async function playFor(seconds: number, dropRate: number) {
  const fps = 24;
  const step = 2; // matches the sampler's interval
  for (let t = 0; t < seconds; t += step) {
    const frames = fps * step;
    counters.total += frames;
    counters.dropped += Math.round(frames * dropRate);
    await act(async () => {
      vi.advanceTimersByTime(step * 1000);
      await Promise.resolve();
    });
  }
  await settle(30);
}

describe("a direct play that cannot keep up", () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("starts out direct, because the server said so", async () => {
    await start();
    expect(streams()[0]).not.toContain("/transcode");
    expect(streams()[0]).not.toContain("/hls/");
  });

  // The reported fault: a fifth of the frames gone, sustained.
  it("converts instead once the frames keep being dropped", async () => {
    await start();
    await playFor(20, 0.2);
    const last = streams()[streams().length - 1];
    expect(last).toMatch(/\/transcode|\/hls\//);
  });

  /*
   * And resumes where the viewer is. Firing half an hour in and restarting at
   * the beginning would be a worse bug than the frame lag it fixes.
   */
  it("resumes where the viewer was, not where they started", async () => {
    await start();
    await playFor(20, 0.2);
    const last = streams()[streams().length - 1];
    expect(last).toContain("t=1800");
  });

  /*
   * The claim that caused it is withdrawn, so the *next* film does not have to
   * be watched badly for twenty seconds to reach the same answer. Narrow: only
   * what this file needed, because a file cannot be ruined by a claim it never
   * used.
   */
  it("withdraws the codec claim that made it direct-play", async () => {
    await start();
    await playFor(20, 0.2);
    const names = deniedCapabilities().map((d) => d.name);
    expect(names).toContain("hevc10");
    expect(names).not.toContain("ac3"); // never needed by this file
  });

  // The control, from the same shelf on the same machine.
  it("leaves a file that plays cleanly alone", async () => {
    await start();
    await playFor(30, 0);
    expect(streams()).toHaveLength(1);
    expect(deniedCapabilities()).toHaveLength(0);
  });

  /*
   * And it only fires once. Without the latch the sampler would keep meeting a
   * bad ratio while the replacement source loads, and re-request the transcode
   * on every tick.
   */
  it("does not keep re-requesting once it has switched", async () => {
    await start();
    await playFor(20, 0.2);
    const afterSwitch = streams().length;
    await playFor(20, 0.2);
    expect(streams()).toHaveLength(afterSwitch);
  });
});
