/*
 * The Skip intro button, wired to a real playhead.
 *
 * The rule is pure and tested next door in lib/skip.test.ts. This is the half a
 * correct rule says nothing about: that it is connected to the item's markers
 * and to the element, that pressing it seeks where it said, and that it goes
 * away again.
 *
 * That seam is where this project's client bugs live. A settings shell whose
 * panes were not wired to its buttons, a context menu whose two items emptied
 * the same shelf — both had correct logic underneath.
 *
 * jsdom performs no layout, so nothing here is about where the button sits.
 * What it proves is when it exists and what it does.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback } from "@/playback/PlaybackProvider";
import { Player } from "./Player";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let seeked: number[];
let pb: ReturnType<typeof usePlayback>;

// The player takes its item from provider state, not the URL, so a harness has
// to ask it to play something the way the app does.
function Probe() {
  pb = usePlayback();
  return null;
}

let creditsSource = "blackdetect-gated";
// The account's switch (ADR 0083). Off here unless a test turns it on, so the
// button tests below describe an account that chose the button.
let skipIntros = false;

/**
 * An episode with a detected intro from 87s to 117s, and credits from 1,240s
 * of 1,320 — 93.9%.
 */
function episode() {
  return {
    id: 7,
    title: "The Gang Gets Analyzed",
    kind: "episode",
    duration_ms: 1_320_000,
    progress: { position_ms: 0, watched: false },
    media_streams: [
      { index: 0, kind: "video", codec: "h264" },
      { index: 1, kind: "audio", codec: "aac", language: "eng" },
    ],
    markers: [
      {
        kind: "intro",
        start_ms: 87_000,
        end_ms: 117_000,
        source: "fingerprint",
        confidence: 0.9,
        created_at: 0,
      },
      {
        kind: "credits",
        start_ms: 1_240_000,
        source: creditsSource,
        confidence: 0.9,
        created_at: 0,
      },
    ],
  };
}

beforeEach(() => {
  seeked = [];
  creditsSource = "blackdetect-gated";
  skipIntros = false;
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
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (String(url).includes("/api/profile/playback")) {
        return json({ skip_intros: skipIntros });
      }
      if (String(url).includes("/playback")) {
        return json({ decision: { method: "direct", reason: "" } });
      }
      if (/\/api\/items\/\d+(\?|$)/.test(String(url))) return json(episode());
      if (String(url).includes("/api/auth")) {
        return json({ user: { role: "admin" }, can_convert: true });
      }
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  host.remove();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 5));
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
  await act(async () => pb.play(7, []));
  await flush();
  /*
   * The source announcing itself, which every real player does before it
   * reports a position — and which the provider now waits for before trusting
   * the clock, so that the end of one item cannot be filed under the next.
   */
  await act(async () => {
    host.querySelector("video")?.dispatchEvent(new Event("loadedmetadata"));
  });
  await flush();
}

/** Move the playhead and let the element tell the player about it. */
async function at(seconds: number) {
  const el = host.querySelector("video");
  if (!el) return;
  (el as HTMLVideoElement).currentTime = seconds;
  seeked.pop(); // this move is the test's, not the button's
  await act(async () => {
    el.dispatchEvent(new Event("timeupdate"));
  });
  await flush();
}

const button = (label: string) => () =>
  [...host.querySelectorAll("button")].find(
    (b) => b.textContent?.trim() === label,
  );
const skipButton = button("Skip intro");
const skipCredits = button("Skip credits");

describe("skip intro", () => {
  it("offers nothing before the intro", async () => {
    await render();
    await at(40);
    expect(skipButton()).toBeUndefined();
  });

  it("offers the skip once the intro is playing", async () => {
    await render();
    await at(95);
    expect(skipButton()).toBeDefined();
  });

  /*
   * An account that turned automatic skipping off gets ADR 0054 exactly: a
   * button that appears, and reaching the intro moves nothing by itself.
   */
  it("does not skip on its own when the account says not to", async () => {
    await render();
    await at(95);
    expect(seeked).toHaveLength(0);
  });

  it("seeks to the end of the intro when pressed, with nothing added", async () => {
    await render();
    await at(95);
    await act(async () => skipButton()!.click());
    await flush();

    expect(seeked).toEqual([117]);
  });

  it("goes away once the intro is behind us", async () => {
    await render();
    await at(95);
    expect(skipButton()).toBeDefined();
    await at(130);
    expect(skipButton()).toBeUndefined();
  });

  // An item with no markers is the ordinary case and must be quiet: most of a
  // library has never been examined.
  it("offers nothing for an item with no markers", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        const json = (v: unknown) =>
          new Response(JSON.stringify(v), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        if (String(url).includes("/playback")) {
          return json({ decision: { method: "direct", reason: "" } });
        }
        if (/\/api\/items\/\d+(\?|$)/.test(String(url))) {
          const e = episode();
          delete (e as { markers?: unknown }).markers;
          return json(e);
        }
        if (String(url).includes("/api/auth")) {
          return json({ user: { role: "admin" }, can_convert: true });
        }
        return json({ items: [], total: 0 });
      }),
    );
    await render();
    await at(95);
    expect(skipButton()).toBeUndefined();
  });
});

describe("skip credits", () => {
  it("offers nothing before the credits", async () => {
    await render();
    await at(1200);
    expect(skipCredits()).toBeUndefined();
  });

  it("offers the skip once the credits begin, and does not take it alone", async () => {
    await render();
    await at(1250);
    expect(skipCredits()).toBeDefined();
    expect(skipButton()).toBeUndefined();
    expect(seeked).toHaveLength(0);
  });

  /*
   * To three seconds before the end, not the end: the last seconds are played,
   * so the film finishes the way every film does and the player's own ending —
   * watched, then the next item — takes over.
   */
  it("seeks to just before the end when pressed", async () => {
    await render();
    await at(1250);
    await act(async () => skipCredits()!.click());
    await flush();
    expect(seeked).toEqual([1317]);
  });

  // The fixture is an episode, and on an episode the ungated rule is trusted:
  // early on one in thirty-three. (On a film it is not; lib/skip.test.ts.)
  it("offers the ungated rule's marker on an episode", async () => {
    creditsSource = "blackdetect";
    await render();
    await at(1250);
    expect(skipCredits()).toBeDefined();
  });

  it("offers the shared ending's marker on an episode", async () => {
    creditsSource = "ending-audio";
    await render();
    await at(1250);
    expect(skipCredits()).toBeDefined();
  });

  it("ignores a credits marker from a source nobody vouched for", async () => {
    creditsSource = "guess";
    await render();
    await at(1250);
    expect(skipCredits()).toBeUndefined();
  });
});

describe("an intro that skips itself (ADR 0083)", () => {
  const notice = () => host.querySelector(".player__skipped");
  const watchIntro = button("Watch intro");

  /** The element saying it is playing, as a real one does once it starts. */
  async function playing() {
    await act(async () => {
      host.querySelector("video")?.dispatchEvent(new Event("play"));
    });
    await flush();
  }

  /** Play up to and into the intro a second at a time, as a playhead does. */
  async function playInto() {
    await playing();
    for (const t of [85, 86, 87, 88]) await at(t);
  }

  it("skips on reaching the intro, says so, and hides the button", async () => {
    skipIntros = true;
    await render();
    await playInto();
    expect(seeked).toEqual([117]);
    expect(notice()?.textContent).toContain("Intro skipped");
    expect(skipButton()).toBeUndefined();
  });

  it("goes back to the start of the intro on Watch intro, and does not skip again", async () => {
    skipIntros = true;
    await render();
    await playInto();
    await act(async () => watchIntro()!.click());
    await flush();
    expect(seeked).toEqual([117, 87]);
    expect(notice()).toBeNull();
    // Playing on through the theme: no second jump, and the button is back.
    for (const t of [88, 89, 90]) await at(t);
    expect(seeked).toEqual([117, 87]);
    expect(skipButton()).toBeDefined();
  });

  // The control for the test above: the same playhead with the switch off.
  it("leaves the intro alone, and offers the button, when the account turned it off", async () => {
    skipIntros = false;
    await render();
    await playInto();
    expect(seeked).toEqual([]);
    expect(notice()).toBeNull();
    expect(skipButton()).toBeDefined();
  });

  it("does not jump when somebody scrubs back into the intro", async () => {
    skipIntros = true;
    await render();
    await playing();
    await at(200);
    await at(201);
    await at(95);
    expect(seeked).toEqual([]);
    expect(skipButton()).toBeDefined();
  });
});
