/*
 * Which transport a live channel actually gets wired to.
 *
 * The setting is only worth having if the screen obeys it, and "obeys it" here
 * means two things that are easy to get half-right: the element is fed from the
 * right place, and the compensation code stops running when something else is
 * doing that job. A version that switched the source but left `preroll` holding
 * the element would pass any test about the URL and stutter in real life.
 *
 * jsdom has no MediaSource and performs no media, so nothing here proves a
 * channel plays. What it proves is the wiring — which is exactly the class of
 * fault this suite was built for, after a settings shell whose panes were not
 * connected to its buttons.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LiveTV } from "./LiveTV";
import { LIVE_TRANSPORT_KEY, type LiveTransport } from "@/lib/liveTransport";
import { writeDevice } from "@/lib/device";

/*
 * hls.js is mocked, not loaded.
 *
 * The real vendored bundle is 618 KB and needs a MediaSource jsdom does not
 * have, so loading it here would test the environment rather than the screen.
 * What these tests are about is what the screen does with the attachment once
 * it has one.
 */
vi.mock("hls.js", () => {
  class FakeHls {
    static Events = { ERROR: "hlsError", MANIFEST_PARSED: "hlsManifestParsed" };
    handlers = new Map<string, () => void>();
    on(event: string, fn: () => void) {
      this.handlers.set(event, fn);
      fakeHlsInstances.push(this);
    }
    loadSource() {}
    /*
     * Deliberately does nothing to the element.
     *
     * The real one does not either, not synchronously: the MediaSource reaches
     * the element in a later task, which is exactly why a `play()` on the line
     * after it rejects. A mock that pretended otherwise would let the wrong fix
     * pass, which is what happened.
     */
    attachMedia() {}
    detachMedia() {}
    destroy() {}
    /** Stand in for the playlist arriving. */
    emitManifestParsed() {
      this.handlers.get("hlsManifestParsed")?.();
    }
  }
  return { default: FakeHls };
});

type FakeHlsInstance = { emitManifestParsed: () => void };
const fakeHlsInstances: FakeHlsInstance[] = [];

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const channels = [
  {
    id: 7,
    source_id: 1,
    name: "Channel Seven",
    logo_url: null,
    group: "UK",
    position: 0,
    tvg_id: "seven.example",
  },
];

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  fakeHlsInstances.length = 0;
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  /*
   * Reset through writeDevice rather than localStorage.
   *
   * device.ts keeps a module-level cache so that getSnapshot returns a stable
   * reference, and that cache outlives a test. Clearing localStorage alone
   * leaves the first value any test read in place for every test after it —
   * which showed up here as the setting appearing to be ignored when it was in
   * fact never re-read.
   */
  writeDevice<LiveTransport>(LIVE_TRANSPORT_KEY, "progressive");
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
  writeDevice<LiveTransport>(LIVE_TRANSPORT_KEY, "progressive");
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

async function playFirstChannel() {
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
  return host.querySelector("video")!;
}

describe("live tv transport", () => {
  it("uses the progressive endpoint by default", async () => {
    const el = await playFirstChannel();
    expect(el.getAttribute("src")).toBe("/api/channels/7/live");
  });

  /*
   * jsdom reports no MediaSource, which is the "cannot do it" case rather than
   * an incidental limitation — so with the setting on it must fall back rather
   * than leave the element with nothing.
   *
   * This is the assertion that would have caught handing a device a black
   * rectangle because a preference asked for something it does not have.
   */
  it("falls back to progressive when the device has no MediaSource, even with the setting on", async () => {
    writeDevice<LiveTransport>(LIVE_TRANSPORT_KEY, "mse");
    const el = await playFirstChannel();
    expect(el.getAttribute("src")).toBe("/api/channels/7/live");
  });

  it("hands a native-HLS browser the playlist instead of a pipeline to it", async () => {
    writeDevice<LiveTransport>(LIVE_TRANSPORT_KEY, "mse");
    // Safari's tell. canPlayType answering for the playlist type is what
    // separates "plays HLS itself" from "needs MSE built for it".
    vi.spyOn(HTMLMediaElement.prototype, "canPlayType").mockReturnValue(
      "probably",
    );
    const el = await playFirstChannel();
    expect(el.getAttribute("src")).toBe("/api/channels/7/hls/index.m3u8");
  });

  /*
   * The half of preroll's job that was not a guess.
   *
   * The element carries no `autoPlay` and nothing plays it on `canplay`, both
   * deliberately — what starts a channel is the preroll effect, after it has
   * waited for a head start. That effect does not run on the MSE path, and
   * when this path was built nothing took over the *pressing play* part of it.
   * A channel attached cleanly and sat at 0:00 until somebody clicked, which no
   * assertion in this file could see, because every one of them was about the
   * source.
   *
   * Found by watching a real channel, which is what the ADR gated step 6 on.
   */
  it("does not press play before there is anything to play", async () => {
    /*
     * The assertion the first fix would have failed, and the reason it shipped
     * to a real browser and changed nothing.
     *
     * `attachMedia` returns before the MediaSource reaches the element, so a
     * `play()` on the next line runs against an element with no source at all.
     * It rejects, the rejection is swallowed, and the channel sits at 0:00
     * looking exactly like a channel that is off the air.
     *
     * A test that only asked *whether* play was called passed that version.
     * This one asks *when*.
     */
    writeDevice<LiveTransport>(LIVE_TRANSPORT_KEY, "mse");
    vi.stubGlobal("MediaSource", {
      isTypeSupported: () => true,
    } as unknown as typeof MediaSource);
    vi.spyOn(HTMLMediaElement.prototype, "canPlayType").mockReturnValue("");
    const play = vi
      .spyOn(HTMLMediaElement.prototype, "play")
      .mockResolvedValue(undefined);

    await playFirstChannel();
    await flush();
    await flush();

    expect(play).not.toHaveBeenCalled();
  });

  it("presses play on the MSE path, because nothing else does", async () => {
    writeDevice<LiveTransport>(LIVE_TRANSPORT_KEY, "mse");
    vi.stubGlobal("MediaSource", {
      isTypeSupported: () => true,
    } as unknown as typeof MediaSource);
    vi.spyOn(HTMLMediaElement.prototype, "canPlayType").mockReturnValue("");
    const play = vi
      .spyOn(HTMLMediaElement.prototype, "play")
      .mockResolvedValue(undefined);

    await playFirstChannel();
    await flush();
    await flush();

    /*
     * The manifest alone must not be enough.
     *
     * This was the second wrong fix: the playlist is loaded independently of
     * the element, so MANIFEST_PARSED can fire while the element still has
     * nothing.
     */
    await act(async () => {
      for (const h of fakeHlsInstances) h.emitManifestParsed();
    });
    await flush();
    expect(play).not.toHaveBeenCalled();

    // The element having media is the only thing that settles it.
    const el = host.querySelector("video")!;
    await act(async () => {
      el.dispatchEvent(new Event("loadedmetadata"));
    });
    await flush();

    expect(play).toHaveBeenCalled();
  });

  /*
   * The path this client actually takes, and the one every earlier fix missed.
   *
   * Chromium answers `maybe` for `application/vnd.apple.mpegurl`, so `livePath`
   * reads it as a native-HLS browser and hands the element the playlist. Three
   * fixes went into the MSE effect reasoning about hls.js, and none of them ran
   * — the element reached readyState 4 with ten seconds buffered and sat at
   * 0:00, because preroll starts a channel and preroll is progressive-only.
   *
   * This is the assertion that would have caught it on the first afternoon.
   */
  it("presses play on the native-HLS path too, which preroll does not cover", async () => {
    writeDevice<LiveTransport>(LIVE_TRANSPORT_KEY, "mse");
    vi.spyOn(HTMLMediaElement.prototype, "canPlayType").mockReturnValue(
      "maybe",
    );
    const play = vi
      .spyOn(HTMLMediaElement.prototype, "play")
      .mockResolvedValue(undefined);

    const el = await playFirstChannel();
    // Confirm the premise rather than assuming it: this is the branch where
    // the element is handed the playlist itself.
    expect(el.getAttribute("src")).toBe("/api/channels/7/hls/index.m3u8");

    await act(async () => {
      el.dispatchEvent(new Event("loadedmetadata"));
    });
    await flush();

    expect(play).toHaveBeenCalled();
  });

  /*
   * Metadata that arrived before the effect did.
   *
   * A listener for an event that has been and gone never fires, and on a fast
   * local server that is the ordinary case rather than the rare one.
   */
  it("plays a channel whose metadata arrived before the effect ran", async () => {
    writeDevice<LiveTransport>(LIVE_TRANSPORT_KEY, "mse");
    vi.spyOn(HTMLMediaElement.prototype, "canPlayType").mockReturnValue(
      "maybe",
    );
    vi.spyOn(HTMLMediaElement.prototype, "readyState", "get").mockReturnValue(4);
    const play = vi
      .spyOn(HTMLMediaElement.prototype, "play")
      .mockResolvedValue(undefined);

    await playFirstChannel();
    await flush();

    expect(play).toHaveBeenCalled();
  });

  it("leaves the element without a src on the MSE path, so nothing fetches the channel twice", async () => {
    writeDevice<LiveTransport>(LIVE_TRANSPORT_KEY, "mse");
    // Enough of a MediaSource for the capability check to say yes. The library
    // is never reached: the dynamic import is not awaited by this assertion,
    // and what is being tested is that the element was not given a src.
    vi.stubGlobal("MediaSource", {
      isTypeSupported: () => true,
    } as unknown as typeof MediaSource);
    vi.spyOn(HTMLMediaElement.prototype, "canPlayType").mockReturnValue("");

    const el = await playFirstChannel();
    expect(el.getAttribute("src")).toBeNull();
  });
});
