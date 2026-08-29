/*
 * How many streams one playback asks the server for.
 *
 * The server log has repeatedly shown two `transcode started` lines for one
 * item, milliseconds apart, both at `start_at=0`. That is not free: each line
 * is a real ffmpeg, and the second one supersedes the first, so the work the
 * first did is thrown away and the viewer waits for a process that started
 * late.
 *
 * `supersede` means this can never *leak* — which is why it was ruled out as a
 * server fault, correctly. It is a client question: how many times does the
 * source effect assign `v.src` for a single play?
 *
 * Instrumented rather than argued. Every previous pass at this was a reading of
 * the effect's dependency array, and readings disagreed; a count does not.
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

/** Every value assigned to a media element's src, in order. */
let sources: string[] = [];

function Probe() {
  pb = usePlayback();
  return <span data-testid="item">{pb.itemID}</span>;
}

/** One film that has to be converted, so the transcode path is the one taken. */
function itemBody(id: number) {
  return {
    id,
    title: "A Film",
    kind: "movie",
    duration_ms: 5_400_000,
    progress: { position_ms: 0, watched: false },
    media_streams: [
      { index: 0, kind: "video", codec: "hevc" },
      { index: 1, kind: "audio", codec: "ac3", language: "eng" },
      { index: 2, kind: "audio", codec: "aac", language: "fra" },
    ],
  };
}

beforeEach(() => {
  sources = [];

  /*
   * jsdom implements `src` as a plain reflected attribute and has neither
   * `load` nor `play`. Replacing the setter is what makes the count possible at
   * all — there is no network in jsdom, so a request is only ever visible as an
   * assignment.
   */
  const proto = window.HTMLMediaElement.prototype;
  const original = Object.getOwnPropertyDescriptor(
    window.HTMLElement.prototype,
    "src",
  );
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
  void original;
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

      if (url.includes("/playback")) {
        return json({
          decision: {
            method: "transcode",
            reason: "video codec hevc is not supported",
          },
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
});

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
  await settle();
}

async function settle(ms = 20) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

/** Only the stream requests; the provider assigns nothing else to src. */
function streams() {
  return sources.filter((s) => s.includes("/stream") || s.includes("/api/items"));
}

describe("how many streams one playback asks for", () => {
  it("asks for one stream when a film starts", async () => {
    await render();
    await act(async () => {
      pb.play(1, [1]);
    });
    await settle(60);

    /*
     * The assertion the log has been failing in the field.
     *
     * Two here means two ffmpeg processes for one press of play, the first
     * superseded before it produced anything useful — invisible except as a
     * slower start and a doubled line in the log.
     */
    expect(streams()).toHaveLength(1);
  });

  it("asks for one more when the audio track is changed, not two", async () => {
    await render();
    await act(async () => {
      pb.play(1, [1]);
    });
    await settle(60);
    const before = streams().length;

    await act(async () => {
      pb.selectAudio(2);
    });
    await settle(60);

    // A track change is a new request by design — the server decides delivery,
    // so the client cannot switch tracks by itself. Exactly one, though.
    expect(streams().length - before).toBe(1);
  });

  /*
   * Moving between items is where a stale audio index could double it: the
   * reset to the file's own default and the source effect are separate effects
   * over the same change, and if the source effect runs once on the old index
   * and again on null, that is two ffmpegs for one film.
   */
  it("asks for one stream when moving to another film after picking a track", async () => {
    await render();
    await act(async () => {
      pb.play(1, [1, 2]);
    });
    await settle(60);
    await act(async () => {
      pb.selectAudio(2);
    });
    await settle(60);

    const before = streams().length;
    await act(async () => {
      pb.play(2, [1, 2]);
    });
    await settle(80);

    expect(streams().length - before).toBe(1);
  });
});
