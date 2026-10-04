/*
 * Up Next, wired to a real playhead and a real queue.
 *
 * The card's job is small and every part of it is a promise: it appears when
 * the credits do and only when something follows, it names what follows, it
 * waits for a person or for its count, and either way forward is the ordinary
 * end of the episode rather than a jump of its own. jsdom performs no layout,
 * so nothing here is about where the card sits.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback } from "@/playback/PlaybackProvider";
import { Player } from "./Player";
import { UP_NEXT_SECONDS } from "@/components/UpNextCard";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let seeked: number[];
let pb: ReturnType<typeof usePlayback>;
let kind = "episode";

function Probe() {
  pb = usePlayback();
  return null;
}

/** Episode 7: 1,320 s, gated credits from 1,240 s. Episode 8 follows it. */
function item(id: number) {
  const base = {
    kind,
    duration_ms: 1_320_000,
    progress: { position_ms: 0, watched: false },
    media_streams: [
      { index: 0, kind: "video", codec: "h264" },
      { index: 1, kind: "audio", codec: "aac", language: "eng" },
    ],
  };
  const markers = [
    { kind: "credits", start_ms: 1_240_000, source: "blackdetect-gated", confidence: 0.9, created_at: 0 },
  ];
  const titles: Record<number, [string, number]> = {
    7: ["The Gang Gets Racist", 1],
    8: ["The Gang Exploits a Miracle", 2],
    9: ["Mac Bangs Dennis's Mom", 5],
    10: ["Charlie Wants an Abortion", 3],
    11: ["Underage Drinking", 4],
  };
  const [title, episode] = titles[id] ?? titles[7];
  return { ...base, id, title, season: 1, episode, markers };
}

beforeEach(() => {
  // From the start, not from the moment a test wants the count: the card
  // schedules its first second when it appears, and a timer made before the
  // clock was faked is never reached by advancing it.
  vi.useFakeTimers({ shouldAdvanceTime: true });
  seeked = [];
  kind = "episode";
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  const proto = window.HTMLMediaElement.prototype;
  proto.play = vi.fn(async () => {});
  proto.pause = vi.fn();
  proto.load = vi.fn();
  let now = 0;
  Object.defineProperty(proto, "currentTime", {
    configurable: true,
    get: () => now,
    set: (v: number) => {
      seeked.push(v);
      now = v;
    },
  });
  Object.defineProperty(proto, "duration", { configurable: true, get: () => 1320 });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
      const u = String(url);
      if (u.includes("/playback")) return json({ decision: { method: "direct", reason: "" } });
      const m = u.match(/\/api\/items\/(\d+)(\?|$)/);
      if (m) return json(item(Number(m[1])));
      if (u.includes("/api/auth")) return json({ user: { role: "admin" }, can_convert: true });
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(async () => {
  // Playback preferences are device-local and cached for the whole module:
  // one test turning auto play off would turn it off for every test after it,
  // and the paused test would pass by never counting at all.
  await act(async () => pb?.setPrefs({ autoPlay: true }));
  vi.useRealTimers();
  host.remove();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 5));
  });
}

async function render(queue: number[]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter initialEntries={["/play/7"]}>
            <PlaybackProvider>
              <Probe />
              <Player />
            </PlaybackProvider>
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await flush();
  await act(async () => pb.play(7, queue));
  await flush();
  await act(async () => {
    host.querySelector("video")?.dispatchEvent(new Event("loadedmetadata"));
    // "play" is what tells the provider it is playing; the countdown waits on it.
    host.querySelector("video")?.dispatchEvent(new Event("play"));
    host.querySelector("video")?.dispatchEvent(new Event("playing"));
  });
  await flush();
}

async function at(seconds: number) {
  const el = host.querySelector("video");
  if (!el) return;
  (el as HTMLVideoElement).currentTime = seconds;
  seeked.pop(); // the test's move, not the card's
  await act(async () => {
    el.dispatchEvent(new Event("timeupdate"));
  });
  await flush();
}

/** Let the countdown run, a second at a time, the way the card schedules it. */
async function wait(seconds: number) {
  for (let i = 0; i < seconds; i++) {
    await act(async () => {
      vi.advanceTimersByTime(1000);
    });
  }
}

const card = () => host.querySelector(".player__upnext");
const button = (label: string) =>
  [...host.querySelectorAll("button")].find((b) => b.textContent?.trim() === label);

describe("up next", () => {
  it("appears when the credits begin, naming what follows", async () => {
    await render([7, 8]);
    await at(1200);
    expect(card()).toBeNull();
    await at(1250);
    expect(card()?.textContent).toContain("S01E02");
    expect(card()?.textContent).toContain("The Gang Exploits a Miracle");
    expect(card()?.textContent).toContain(`Playing in ${UP_NEXT_SECONDS}`);
  });

  // Two controls doing one job in one corner is one too many.
  it("takes the place of Skip credits", async () => {
    await render([7, 8]);
    await at(1250);
    expect(button("Skip credits")).toBeUndefined();
  });

  it("rolls on when the count runs out, and not before", async () => {
    await render([7, 8]);
    await at(1250);
    await wait(UP_NEXT_SECONDS - 1);
    expect(seeked).toHaveLength(0);
    await wait(1);
    // To three seconds before the end: the episode ends by playing, which is
    // where watched, auto play and the still-watching run are decided.
    expect(seeked).toEqual([1317]);
  });

  it("plays now when asked", async () => {
    await render([7, 8]);
    await at(1250);
    await act(async () => button("Play now")!.click());
    await flush();
    expect(seeked).toEqual([1317]);
  });

  it("goes away when cancelled, hands back Skip credits, and does not roll on", async () => {
    await render([7, 8]);
    await at(1250);
    await act(async () => button("Cancel")!.click());
    await flush();
    expect(card()).toBeNull();
    expect(button("Skip credits")).toBeDefined();
    await wait(UP_NEXT_SECONDS + 2);
    expect(seeked).toHaveLength(0);
  });

  // Auto play off means the end of an item does not roll on by itself. The
  // card still offers the next one; it does not count.
  it("does not count with auto play off", async () => {
    await render([7, 8]);
    await act(async () => pb.setPrefs({ autoPlay: false }));
    await at(1250);
    expect(card()).not.toBeNull();
    expect(card()?.textContent).not.toContain("Playing in");
    await wait(UP_NEXT_SECONDS + 2);
    expect(seeked).toHaveLength(0);
  });

  it("holds the count while paused, and finishes it on resuming", async () => {
    await render([7, 8]);
    await at(1250);
    const video = () => host.querySelector("video")!;
    await act(async () => {
      video().dispatchEvent(new Event("pause"));
    });
    await flush();
    await wait(UP_NEXT_SECONDS + 2);
    expect(seeked).toHaveLength(0);
    // Not a count that never runs: resumed, it finishes.
    await act(async () => {
      video().dispatchEvent(new Event("play"));
      video().dispatchEvent(new Event("playing"));
    });
    await flush();
    await wait(UP_NEXT_SECONDS + 1);
    expect(seeked).toEqual([1317]);
  });

  /*
   * Something queued by hand plays before the queue resumes, so it is what the
   * card names. A card naming the queue's next episode while the lane plays
   * another would be a picture of an order that is not going to happen.
   */
  it("names the hand-queued item, which is what will play", async () => {
    await render([7, 8]);
    await act(async () => pb.playNextUp(9));
    await at(1250);
    expect(card()?.textContent).toContain("S01E05");
    expect(card()?.textContent).not.toContain("S01E02");
  });

  // The last episode has nothing to be up next, and keeps Skip credits.
  it("does not appear with nothing to follow", async () => {
    await render([7]);
    await at(1250);
    expect(card()).toBeNull();
    expect(button("Skip credits")).toBeDefined();
  });

  it("does not appear on a film", async () => {
    kind = "movie";
    await render([7, 8]);
    await at(1250);
    expect(card()).toBeNull();
  });

  /*
   * The countdown running out is the machine moving on, and the still-watching
   * prompt counts exactly that. If the card's roll-on were recorded as a person,
   * a season left playing would never be asked about — the bug a careless
   * reuse of seekTo would have shipped.
   */
  async function endViaCard(press: boolean) {
    await at(1250);
    if (press) {
      await act(async () => button("Play now")!.click());
    } else {
      await wait(UP_NEXT_SECONDS);
    }
    const video = () => host.querySelector("video")!;
    await act(async () => {
      video().dispatchEvent(new Event("ended"));
    });
    await flush();
    await act(async () => {
      video().dispatchEvent(new Event("loadedmetadata"));
      video().dispatchEvent(new Event("play"));
      video().dispatchEvent(new Event("playing"));
    });
    await flush();
  }

  it("counts the countdown as nobody, so a season left playing is asked about", async () => {
    await render([7, 8, 9, 10, 11]);
    for (let i = 0; i < 3; i++) await endViaCard(false);
    expect(pb.stillWatching).not.toBeNull();
  });

  it("counts Play now as somebody", async () => {
    await render([7, 8, 9, 10, 11]);
    for (let i = 0; i < 3; i++) await endViaCard(true);
    expect(pb.stillWatching).toBeNull();
  });
});
