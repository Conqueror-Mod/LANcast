/*
 * The book player (ADR 0116 step 2) against a stand-in for the audio element.
 *
 * jsdom plays nothing, so the element is replaced by an object that records
 * what it is told and raises the events a real one would. What this proves is
 * the wiring the ADR's trap rests on: a book resumes a few seconds back on its
 * own clock, its next part follows within the same book, and every place is
 * written against the book and never a part.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BookProvider, resetBookAudioForTests, useBookPlayer, type BookPlayerState } from "./BookPlayer";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const MIN = 60_000;
const BOOK = 7;

class FakeAudio extends EventTarget {
  src = "";
  currentTime = 0;
  paused = true;
  playbackRate = 1;
  preload = "";
  play() {
    this.paused = false;
    this.dispatchEvent(new Event("play"));
    return Promise.resolve();
  }
  pause() {
    this.paused = true;
    this.dispatchEvent(new Event("pause"));
  }
  load() {}
  removeAttribute(name: string) {
    if (name === "src") this.src = "";
  }
  /** What the browser does once the new source's header is read. */
  ready() {
    this.dispatchEvent(new Event("loadedmetadata"));
  }
  /** Playing on to a point in the part. */
  at(sec: number) {
    this.currentTime = sec;
    this.dispatchEvent(new Event("timeupdate"));
  }
}

let audio: FakeAudio;
let host: HTMLDivElement;
let root: Root;
let player: BookPlayerState | null = null;
let writes: { url: string; method: string; body: unknown }[] = [];

function Probe() {
  player = useBookPlayer();
  return null;
}

const info = {
  duration_ms: 30 * MIN,
  ready: true,
  parts: [
    { id: 11, title: "Part 1", start_ms: 0, duration_ms: 10 * MIN },
    { id: 12, title: "Part 2", start_ms: 10 * MIN, duration_ms: 20 * MIN },
  ],
  chapters: [
    { title: "Part 1", start_ms: 0, end_ms: 10 * MIN },
    { title: "Part 2", start_ms: 10 * MIN, end_ms: 30 * MIN },
  ],
  position_ms: 15 * MIN,
  watched: false,
};

beforeEach(async () => {
  audio = new FakeAudio();
  resetBookAudioForTests(audio as unknown as HTMLAudioElement);
  writes = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method && init.method !== "GET") {
        writes.push({ url, method: init.method, body: init.body ? JSON.parse(String(init.body)) : null });
        return new Response(null, { status: 204 });
      }
      const body = url.endsWith(`/api/items/${BOOK}/book`) ? info : { id: BOOK, kind: "book", title: "Warped" };
      return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
    }),
  );
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <BookProvider>
          <Probe />
        </BookProvider>
      </QueryClientProvider>,
    );
  });
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  resetBookAudioForTests();
});

describe("playing a book", () => {
  it("resumes a few seconds back, in the part the place falls in", async () => {
    await act(async () => {
      await player!.play(BOOK);
    });
    // Fifteen minutes in is five minutes into the second part, less five seconds.
    expect(audio.src).toBe("/api/stream/12");
    act(() => audio.ready());
    expect(audio.currentTime).toBe(5 * 60 - 5);
    expect(audio.paused).toBe(false);
    expect(player!.speed).toBe(1);
  });

  it("follows one part with the next within the same book", async () => {
    await act(async () => {
      await player!.play(BOOK, 0);
    });
    expect(audio.src).toBe("/api/stream/11");
    act(() => audio.ready());
    act(() => audio.dispatchEvent(new Event("ended")));
    expect(audio.src).toBe("/api/stream/12");
    act(() => audio.ready());
    act(() => audio.at(60));
    // One clock: a minute into part two is eleven minutes into the book.
    expect(player!.timeMs).toBe(11 * MIN);
    expect(player!.bookID).toBe(BOOK);
  });

  it("writes the place against the book and never a part", async () => {
    await act(async () => {
      await player!.play(BOOK);
    });
    act(() => audio.ready());
    act(() => audio.at(6 * 60));
    act(() => audio.pause());
    const progress = writes.filter((w) => w.url.endsWith("/progress"));
    expect(progress.length).toBeGreaterThan(0);
    for (const w of progress) expect(w.url).toBe(`/api/items/${BOOK}/progress`);
    expect(progress.at(-1)!.body).toEqual({ position_ms: 16 * MIN, watched: false });
  });

  it("finishes the book, once, at the end of its last part", async () => {
    await act(async () => {
      await player!.play(BOOK, 25 * MIN);
    });
    act(() => audio.ready());
    act(() => audio.dispatchEvent(new Event("ended")));
    const finished = writes.filter((w) => (w.body as { watched?: boolean } | null)?.watched);
    expect(finished).toEqual([
      { url: `/api/items/${BOOK}/progress`, method: "PUT", body: { position_ms: 30 * MIN, watched: true } },
    ]);
  });

  it("keeps a speed per book, on the element and on the server", async () => {
    await act(async () => {
      await player!.play(BOOK, 0);
    });
    act(() => player!.setSpeed(1.5));
    expect(audio.playbackRate).toBe(1.5);
    expect(writes.find((w) => w.url.endsWith("/speed"))).toEqual({
      url: `/api/items/${BOOK}/speed`,
      method: "PUT",
      body: { speed: 1.5 },
    });
  });

  it("skips back 15 and forward 30 on the book's clock, across parts", async () => {
    await act(async () => {
      await player!.play(BOOK, 10 * MIN + 5_000);
    });
    act(() => audio.ready());
    act(() => audio.at(5));
    act(() => player!.skipBack());
    // Five seconds into part two, back fifteen: ten seconds before its end in part one.
    expect(audio.src).toBe("/api/stream/11");
    act(() => audio.ready());
    expect(audio.currentTime).toBe(10 * 60 - 10);
  });
});
