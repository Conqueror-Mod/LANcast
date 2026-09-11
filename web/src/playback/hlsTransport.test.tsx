/*
 * Which URL a conversion actually asks for, and what happens when the engine
 * cannot read it.
 *
 * The bug: `All About the Benjamins` — 5.4 Mbps, video copied, audio
 * re-encoded — logged **twelve transcode sessions in eighteen minutes, every
 * one at `start_at=0`**, with no ffmpeg error and nothing reaped. A progressive
 * transcode cannot be range-served, so when Chromium evicts buffered media it
 * can only re-ask from byte zero, and the further into the film the longer that
 * takes. Reported as lagging every few minutes, starting about fifteen minutes
 * in.
 *
 * The unit tests next door cover the choice. These cover the wiring, which is
 * where a correct decision can still reach the element as the wrong URL — and
 * the fallback, which is the part that must work on an engine nobody here has.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback } from "./PlaybackProvider";
import { HLS_VERDICT_KEY } from "./fileTransport";
import { writeDevice } from "@/lib/device";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
let sources: string[] = [];

function Probe() {
  pb = usePlayback();
  return <span data-testid="item">{pb.itemID}</span>;
}

/** A film needing conversion, so the transcode path is the one taken. */
function itemBody(id: number) {
  return {
    id,
    title: "A Film",
    kind: "movie",
    duration_ms: 5_400_000,
    progress: { position_ms: 0, watched: false },
    media_streams: [
      { index: 0, kind: "video", codec: "h264" },
      { index: 1, kind: "audio", codec: "ac3", language: "eng" },
    ],
  };
}

/** What the element claims about playlists. Chromium says "maybe". */
let canPlay = "maybe";

/*
 * What the *server* does when asked for the playlist.
 *
 * This is the distinction the fallback got wrong for a release. The element
 * raises MEDIA_ERR_SRC_NOT_SUPPORTED both when the engine cannot read a
 * playlist and when the server handed it something that is not one — a 503,
 * say, which is what this server returns when ffmpeg has not written
 * index.m3u8 in time. Only the first says anything about the device.
 */
let playlistServed = true;

beforeEach(() => {
  sources = [];
  canPlay = "maybe";
  playlistServed = true;
  localStorage.clear();
  /*
   * readDevice keeps a module-level cache that outlives clearing localStorage,
   * so the verdict has to be written back rather than merely erased — without
   * this, a test that ends with "refused" silently decides the next one, which
   * then never takes the playlist path it is about.
   */
  writeDevice(HLS_VERDICT_KEY, "unknown");

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
  proto.load = vi.fn();
  proto.play = vi.fn(async () => {});
  proto.pause = vi.fn();
  proto.canPlayType = vi.fn(() => canPlay) as unknown as typeof proto.canPlayType;

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
      if (url.includes("/hls/index.m3u8")) {
        return playlistServed
          ? new Response("#EXTM3U", {
              status: 200,
              headers: { "Content-Type": "application/vnd.apple.mpegurl" },
            })
          : new Response(JSON.stringify({ error: { code: "unavailable" } }), {
              status: 503,
              headers: { "Content-Type": "application/json" },
            });
      }
      if (url.includes("/playback")) {
        return json({
          decision: {
            method: "transcode",
            reason: "audio codec ac3 is not supported",
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
const media = () => host.querySelector("video") as HTMLVideoElement | null;

/** Fire the element's error event with a given MediaError code. */
async function failWith(code: number) {
  const v = media();
  if (!v) throw new Error("no media element");
  Object.defineProperty(v, "error", {
    configurable: true,
    value: { code },
  });
  await act(async () => {
    v.dispatchEvent(new Event("error"));
  });
  await settle();
}

async function start() {
  await render();
  await act(async () => {
    pb.play(1, [1]);
  });
  await settle();
}

describe("delivering a conversion", () => {
  it("asks for a playlist rather than one endless response", async () => {
    await start();
    expect(streams()[0]).toContain("/hls/index.m3u8");
  });

  /*
   * The fallback, which is the part that has to work on an engine nobody here
   * has. canPlayType answers "maybe" whether or not playback will work, so this
   * is discovered by trying — and the cost of finding out must be one reload,
   * not a dead player.
   */
  it("falls back to the endless response when the engine refuses the playlist", async () => {
    await start();
    expect(streams()[0]).toContain("/hls/index.m3u8");

    await failWith(4); // MEDIA_ERR_SRC_NOT_SUPPORTED

    const after = streams();
    expect(after.length).toBeGreaterThan(1);
    expect(after[after.length - 1]).toContain("/transcode");
    expect(after[after.length - 1]).not.toContain("/hls/");
  });

  it("remembers the refusal so the next film does not pay for it again", async () => {
    await start();
    await failWith(4);
    await settle();
    expect(localStorage.getItem(HLS_VERDICT_KEY)).toContain("refused");
  });

  /*
   * The failure that cost a release, and the reason the verdict is no longer
   * written from the error code alone.
   *
   * The server returns 503 when ffmpeg has not produced index.m3u8 within
   * thirty seconds. The element reports that as *unsupported source* — the
   * same code as an engine that cannot read a playlist — so one slow start was
   * recorded as "this device cannot play HLS" and every later film fell back to
   * the progressive path, which is the path segments exist to replace.
   *
   * On the server this happened for every film, because the playlist type made
   * ffmpeg defer the playlist to the end of the encode. Even with that fixed, a
   * single slow start must not retire the path for ever.
   */
  it("does not blame the engine when the server did not serve a playlist", async () => {
    playlistServed = false;
    await start();
    await failWith(4);
    await settle();
    expect(localStorage.getItem(HLS_VERDICT_KEY)).not.toContain("refused");
  });

  // The fallback still happens either way: it is right for this playback
  // whichever half failed, and the viewer must not wait on the question.
  it("falls back for this film even when no verdict is recorded", async () => {
    playlistServed = false;
    await start();
    await failWith(4);
    const after = streams();
    expect(after[after.length - 1]).toContain("/transcode");
  });

  /*
   * A decode error is about this file and a network error about this moment.
   * Treating either as a verdict on the engine would retire the better path for
   * every film, permanently, over one transient fault.
   */
  it("does not blame the playlist for a decode error", async () => {
    await start();
    await failWith(3); // MEDIA_ERR_DECODE
    expect(localStorage.getItem(HLS_VERDICT_KEY)).not.toContain("refused");
  });

  /*
   * `playing` is not proof that playlists work.
   *
   * On the desktop client the element fires `playing` on a playlist and then
   * fails a few seconds later, on every film. Recording "playable" at `playing`
   * wiped the refusal count each time: the stored record read `refusals: 1`
   * after twenty failures, never settled, and every start, seek and audio change
   * paid eight to eleven seconds retrying a path that had never once worked.
   */
  it("settles on the fallback when playlists play briefly and then fail", async () => {
    await render();
    for (const id of [1, 2, 3]) {
      await act(async () => {
        pb.play(id, [id]);
      });
      await settle();
      expect(streams()[streams().length - 1], `film ${id} should try the playlist`).toContain(
        "/hls/index.m3u8",
      );
      const v = media()!;
      await act(async () => {
        v.dispatchEvent(new Event("playing"));
      });
      await failWith(4);
      await settle();
    }

    const record = JSON.parse(localStorage.getItem(HLS_VERDICT_KEY) ?? "{}");
    expect(
      record,
      "each brief `playing` reset the count, so three failures never settled",
    ).toMatchObject({ verdict: "refused", refusals: 3 });

    await act(async () => {
      pb.play(4, [4]);
    });
    await settle();
    expect(streams()[streams().length - 1]).toContain("/transcode");
    expect(streams()[streams().length - 1]).not.toContain("/hls/");
  });

  // The other half: a device where playlists really work is still recognised,
  // once enough has actually played.
  it("remembers playlists as playable once enough has really played", async () => {
    await start();
    const v = media()!;
    Object.defineProperty(v, "currentTime", { configurable: true, writable: true, value: 0 });
    await act(async () => {
      v.dispatchEvent(new Event("playing"));
    });
    expect(localStorage.getItem(HLS_VERDICT_KEY) ?? "").not.toContain("playable");

    (v as unknown as { currentTime: number }).currentTime = 31;
    await act(async () => {
      v.dispatchEvent(new Event("timeupdate"));
    });
    await settle();
    expect(localStorage.getItem(HLS_VERDICT_KEY)).toContain("playable");
  });

  // An engine that says it has no idea what a playlist is, is believed — there
  // is no reason to spend a visibly failed load discovering that.
  it("does not try a playlist on an engine that rules it out", async () => {
    canPlay = "";
    await start();
    expect(streams()[0]).toContain("/transcode");
  });
});
