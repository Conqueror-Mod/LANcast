/*
 * The book bar's sleep menu is wired to the player's timer: choosing a time
 * sets it and shows what is left, and choosing Off clears it.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { BookProvider, resetBookAudioForTests, useBookPlayer, type BookPlayerState } from "./BookPlayer";
import { BookBar } from "./BookBar";

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
  volume = 1;
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
}

const info = {
  duration_ms: 30 * MIN,
  ready: true,
  parts: [{ id: 11, title: "Part 1", start_ms: 0, duration_ms: 30 * MIN }],
  chapters: [
    { title: "One", start_ms: 0, end_ms: 10 * MIN },
    { title: "Two", start_ms: 10 * MIN, end_ms: 30 * MIN },
  ],
  position_ms: 0,
  watched: false,
};

let host: HTMLDivElement;
let root: Root;
let player: BookPlayerState | null = null;

function Probe() {
  player = useBookPlayer();
  return null;
}

beforeEach(async () => {
  resetBookAudioForTests(new FakeAudio() as unknown as HTMLAudioElement);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method && init.method !== "GET") return new Response(null, { status: 204 });
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
        <MemoryRouter>
          <BookProvider>
            <Probe />
            <BookBar />
          </BookProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await act(async () => {
    await player!.play(BOOK, 2 * MIN);
  });
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  resetBookAudioForTests();
});

function choose(value: string) {
  const select = host.querySelector<HTMLSelectElement>('select[aria-label="Sleep timer"]')!;
  act(() => {
    select.value = value;
    select.dispatchEvent(new Event("change", { bubbles: true }));
  });
}

describe("the book bar's sleep menu", () => {
  it("names itself Sleep while nothing is set", () => {
    const select = host.querySelector<HTMLSelectElement>('select[aria-label="Sleep timer"]');
    expect(select).toBeTruthy();
    expect(select!.selectedOptions[0].textContent).toBe("Sleep");
    expect(host.querySelector(".book-bar__sleep-left")).toBeNull();
  });

  it("sets the end of the chapter, shows what is left, and clears on Off", () => {
    choose("chapter");
    expect(player!.sleep).toBe("chapter");
    // Two minutes into a ten-minute chapter at 1x.
    expect(host.querySelector(".book-bar__sleep-left")!.textContent).toBe("8:00");

    choose("30");
    expect(player!.sleep).toBe(30);
    expect(host.querySelector(".book-bar__sleep-left")!.textContent).toBe("30:00");

    choose("off");
    expect(player!.sleep).toBeNull();
    expect(host.querySelector(".book-bar__sleep-left")).toBeNull();
  });
});
