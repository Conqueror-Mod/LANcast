/*
 * Night mode for a film in a browser tab.
 *
 * The film plays through the same element as music, so night mode runs on the
 * same Web Audio graph. What decides whether it can is what reaches the
 * element: a converted soundtrack arrives as stereo whatever the file carries,
 * a directly played one with the file's own channels, and the graph must not
 * fold surround to stereo without saying so. Driven through the real provider,
 * with the engine's routing call observed rather than performed (jsdom has no
 * Web Audio).
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";

const applied: { night: boolean }[] = [];
vi.mock("./elementEngine", async (orig) => {
  const real = await orig<typeof import("./elementEngine")>();
  return {
    ...real,
    elementFXSupported: () => true,
    applyElementFX: (_el: unknown, fx: { night: boolean }) => {
      applied.push(fx);
      return undefined;
    },
  };
});

const { PlaybackProvider, usePlayback } = await import("./PlaybackProvider");

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
let decision: Record<string, unknown>;

function Probe() {
  pb = usePlayback();
  return null;
}

beforeEach(() => {
  applied.length = 0;
  const proto = window.HTMLMediaElement.prototype;
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
      if (url.includes("/playback")) return json({ decision });
      const item = url.match(/\/api\/items\/(\d+)(\?|$)/);
      if (item) {
        return json({
          id: Number(item[1]),
          title: "A Film",
          kind: "movie",
          duration_ms: 6_000_000,
          // A 5.1 soundtrack, the ordinary case for a film.
          streams: [
            { index: 0, kind: "video", codec: "hevc" },
            { index: 1, kind: "audio", codec: "eac3", channels: 6, default: true },
          ],
        });
      }
      if (url.includes("/api/auth")) return json({ user: { role: "admin" } });
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  localStorage.clear();
});

async function settle(ms = 40) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

async function playWithNight() {
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
  await act(async () => {
    pb.setPrefs({ nightVideo: true });
    pb.play(7, [7]);
  });
  await settle(80);
}

describe("night mode on a film in a browser tab", () => {
  it("engages on a converted soundtrack, which arrives as stereo", async () => {
    decision = { method: "transcode", reason: "hevc", video_action: "encode", audio_action: "encode" };
    await playWithNight();
    expect(pb.filmChannels).toBe(2);
    expect(applied.at(-1)?.night, "night mode was not applied to a converted film").toBe(true);
  });

  it("does not fold a directly played 5.1 soundtrack to stereo", async () => {
    decision = { method: "direct", reason: "" };
    await playWithNight();
    expect(pb.filmChannels).toBe(6);
    expect(applied.at(-1)?.night, "a 5.1 film was routed through the stereo graph").toBe(false);
  });

  it("does not engage when the audio is copied, even if the picture is converted", async () => {
    decision = { method: "transcode", reason: "hevc", video_action: "encode", audio_action: "copy" };
    await playWithNight();
    expect(pb.filmChannels).toBe(6);
    expect(applied.at(-1)?.night).toBe(false);
  });
});
