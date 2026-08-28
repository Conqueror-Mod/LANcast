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
