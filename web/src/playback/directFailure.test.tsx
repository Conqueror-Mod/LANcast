/*
 * Which failed direct plays withdraw a codec claim (R14-2).
 *
 * Every error on a direct source used to withdraw the file's claims for a
 * fortnight. A network drop mid-film — the server restarting for an update,
 * the laptop leaving Wi-Fi — withdrew `hevc`, and every HEVC title on the
 * device was re-encoded for two weeks. So did a missing optimized copy, whose
 * failure has nothing to do with the original's codecs. Only a decode or
 * unsupported-format error is about the codec; the rest still convert, so the
 * film is rescued, and withdraw nothing.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
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

/** Whether the server answers with the optimized copy. */
let optimized = false;
/** The code of the error the element is about to raise. */
let errorCode = 0;

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
  optimized = false;
  errorCode = 0;
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
    get: () => 1800, // half an hour in
    set: () => {},
  });
  Object.defineProperty(proto, "error", {
    configurable: true,
    get: () => (errorCode ? { code: errorCode, message: "" } : null),
  });
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
        return json({
          decision: { method: "direct", reason: "" },
          ...(optimized ? { version: "optimized" } : {}),
        });
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

/** A conversion, by either path. */
const CONVERTED = /\/transcode|\/hls\//;

const streams = () => sources.filter((s) => s.includes("/api/stream"));

async function failWith(code: number) {
  await render();
  await act(async () => {
    pb.play(1, [1]);
  });
  await settle(120);
  const v = host.querySelector("video")!;
  // Playing, half an hour in: the element has said where it is.
  await act(async () => {
    v.dispatchEvent(new Event("loadedmetadata"));
    v.dispatchEvent(new Event("timeupdate"));
  });
  await settle(30);
  errorCode = code;
  await act(async () => {
    v.dispatchEvent(new Event("error"));
  });
  await settle(30);
}

describe("a direct play that fails", () => {
  it("withdraws nothing over a network error, and converts from where it was", async () => {
    await failWith(2); // MEDIA_ERR_NETWORK
    expect(deniedCapabilities()).toEqual([]);
    const last = streams()[streams().length - 1];
    expect(last).toMatch(CONVERTED);
    expect(last).toContain("t=1800");
  });

  it("withdraws the file's own claims over a decode error", async () => {
    await failWith(3); // MEDIA_ERR_DECODE
    const names = deniedCapabilities().map((d) => d.name);
    expect(names).toContain("hevc10");
    expect(names).not.toContain("ac3");
    expect(streams()[streams().length - 1]).toMatch(CONVERTED);
  });

  it("withdraws the file's own claims when the format is refused", async () => {
    await failWith(4); // MEDIA_ERR_SRC_NOT_SUPPORTED
    expect(deniedCapabilities().map((d) => d.name)).toContain("hevc10");
  });

  it("withdraws nothing when it was the optimized copy that failed", async () => {
    optimized = true;
    await failWith(4); // the copy route's 503 arrives as "not supported"
    expect(streams()[0]).toContain("version=optimized");
    expect(deniedCapabilities()).toEqual([]);
    const last = streams()[streams().length - 1];
    expect(last).toMatch(CONVERTED);
    expect(last).not.toContain("version=optimized");
  });
});
