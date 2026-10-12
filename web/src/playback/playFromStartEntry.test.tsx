/*
 * Play from start, through the screen that receives it.
 *
 * Reported: on a film's detail page, Play from start resumed the film instead.
 * The pieces were each tested — the button sends `fromStart` (continueOrPlay),
 * and entrySeconds answers 0 for it (resumePoint) — but never the chain
 * between them: the Player screen reading the flag and the provider honouring
 * it when the source reports. This drives that chain with the real screen and
 * the real provider.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback } from "./PlaybackProvider";
import { Player } from "@/screens/Player";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const SAVED_S = 2633;
const film = {
  id: 700,
  title: "A Film",
  kind: "movie",
  library_id: 1,
  duration_ms: 6_000_000,
  container: "matroska",
  artwork: {},
  progress: { position_ms: SAVED_S * 1000, watched: false },
  // An account with a language preference: the server names the track.
  track_choice: { audio_index: 1 },
  streams: [
    { index: 0, kind: "video", codec: "h264" },
    { index: 1, kind: "audio", codec: "aac", language: "eng" },
  ],
};

let host: HTMLDivElement;
let root: Root;
/** Every position the player asked the element to move to. */
let seeks: number[] = [];
let pb: ReturnType<typeof usePlayback>;
let now = 0;

function Probe() {
  pb = usePlayback();
  return null;
}

beforeEach(() => {
  seeks = [];
  now = 0;
  const proto = window.HTMLMediaElement.prototype;
  proto.load = vi.fn();
  proto.play = vi.fn(async () => {});
  proto.pause = vi.fn();
  Object.defineProperty(proto, "duration", { configurable: true, get: () => 6000 });
  Object.defineProperty(proto, "currentTime", {
    configurable: true,
    get: () => now,
    set: (v: number) => {
      seeks.push(v);
    },
  });
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
      if (url.includes("/progress")) return new Response(null, { status: 204 });
      if (url.includes("/playback")) return json({ decision: { method: "direct", reason: "" } });
      if (/\/api\/items\/700(\?|$)/.test(url)) return json(film);
      if (url.includes("/api/auth")) return json({ user: { role: "admin" }, can_convert: true });
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function settle(ms = 80) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

async function enter(state: unknown) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={[{ pathname: "/watch/700", state }]}>
          <FocusProvider>
            <PlaybackProvider>
              <Routes>
                <Route path="/watch/:id" element={<><Player /><Probe /></>} />
              </Routes>
            </PlaybackProvider>
          </FocusProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await settle();
  const v = document.querySelector("video");
  if (!v) throw new Error("no media element");
  await act(async () => {
    v.dispatchEvent(new Event("loadedmetadata"));
  });
  await settle();
}

describe("entering the player", () => {
  it("resumes a part-watched film at its saved place", async () => {
    await enter(null);
    expect(seeks).toContain(SAVED_S);
  });

  it("starts at the top when the detail page asked for Play from start", async () => {
    await enter({ fromStart: true });
    expect(seeks).not.toContain(SAVED_S);
  });

  it("lets go once it has played, so a track change keeps its place", async () => {
    await enter({ fromStart: true });
    const v = document.querySelector("video")!;
    now = 300;
    await act(async () => {
      v.dispatchEvent(new Event("timeupdate"));
    });
    seeks = [];
    // A different audio track reloads the source.
    await act(async () => pb.selectAudio(0));
    await settle();
    now = 0;
    await act(async () => {
      v.dispatchEvent(new Event("loadedmetadata"));
    });
    await settle();
    expect(seeks).toContain(300);
    expect(seeks).not.toContain(SAVED_S);
  });
});
