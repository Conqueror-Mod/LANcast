/*
 * The runtime shown while Randomize all plays one film after another.
 *
 * Reported: a 1:30:00 film showing a total of 6:00, or 4:12, with the
 * progress bar measured against it, and the wrong length carried from film to
 * film for as long as the run went on. Hard to reproduce, because it needs an
 * order: a converted film leaves the few minutes it had produced as the
 * duration, and the next film's player reports itself open before it knows its
 * length — so nothing replaced the stale number, and it outranked the probe.
 *
 * The probe now leads, the duration is cleared at every new source, and a late
 * length arrives as durationchange.
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
/** What the media element says the current source's length is. */
let elementDuration = NaN;
/** Each item's probed runtime, or null for one the server never measured. */
const probed: Record<number, number | null> = {};

function Probe() {
  pb = usePlayback();
  return <span>{pb.itemID}</span>;
}

function itemBody(id: number) {
  return {
    id,
    title: `Film ${id}`,
    kind: "movie",
    duration_ms: probed[id] ?? null,
    progress: { position_ms: 0, watched: false },
    streams: [
      { index: 0, kind: "video", codec: "h264" },
      { index: 1, kind: "audio", codec: "aac", language: "eng" },
    ],
  };
}

beforeEach(() => {
  elementDuration = NaN;
  for (const k of Object.keys(probed)) delete probed[Number(k)];
  localStorage.clear();

  const proto = window.HTMLMediaElement.prototype;
  proto.load = vi.fn();
  proto.play = vi.fn(async () => {});
  proto.pause = vi.fn();
  Object.defineProperty(proto, "duration", {
    configurable: true,
    get: () => elementDuration,
  });

  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);

  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (init?.method === "PUT") return new Response(null, { status: 204 });
      if (url.includes("/playback")) return json({ decision: { method: "direct", reason: "" } });
      const item = url.match(/\/api\/items\/(\d+)(\?|$)/);
      if (item) return json(itemBody(Number(item[1])));
      if (url.includes("/api/auth")) return json({ user: { role: "admin" }, can_convert: true });
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

async function settle(ms = 80) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

async function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <FocusProvider>
            <PlaybackProvider>
              <Probe />
            </PlaybackProvider>
          </FocusProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await settle();
}


async function reportOpen(duration: number) {
  elementDuration = duration;
  const video = host.querySelector("video")!;
  await act(async () => {
    video.dispatchEvent(new Event("loadedmetadata"));
  });
  await settle();
}

describe("the total runtime across a run of films", () => {
  it("shows the server's measured runtime over the player's", async () => {
    probed[201] = 5_400_000; // 1:30:00
    await mount();
    await act(async () => pb.play(201, [201]));
    await settle();
    // What a stream reports when it knows only part of the film.
    await reportOpen(360);
    expect(pb.totalDuration).toBe(5400);
  });

  it("does not carry one film's length onto the next", async () => {
    await mount();
    // The first film was never probed, so its player's short length is all
    // there is, and it is shown.
    await act(async () => pb.play(301, [301, 302]));
    await settle();
    await reportOpen(252);
    expect(pb.totalDuration).toBe(252);

    // The next one opens before its length is known...
    await act(async () => pb.playFromQueue(302));
    await settle();
    expect(pb.itemID).toBe(302);
    await reportOpen(NaN);
    expect(pb.totalDuration, "the previous film's 4:12 carried over").not.toBe(252);

    // ...and learns it a moment later.
    elementDuration = 7200;
    await act(async () => {
      host.querySelector("video")!.dispatchEvent(new Event("durationchange"));
    });
    await settle();
    expect(pb.totalDuration).toBe(7200);
  });
});
