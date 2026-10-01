/*
 * Night mode and dialogue boost in the control bar, wired end to end.
 *
 * The real provider and the real Player, down the native path: a desktop
 * client with libmpv, asked by the provider whether it plays natively, and by
 * the page whether its player has the filters. What is asserted is when the
 * buttons exist, what pressing them stores, and that the client hears it:
 * the same two commands the settings panel sends.
 *
 * jsdom performs no layout, so nothing here says the buttons are on screen or
 * that the level letter sits in the icon's corner. That is the running app's
 * job.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider, usePlayback } from "@/playback/PlaybackProvider";
import { resetNativePlaybackAvailability, mpvBackend } from "@/playback/mpvBackend";
import { getPrefs, resetPrefs } from "@/playback/prefs";
import { Player } from "./Player";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

class NoLayoutResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}
(globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver =
  NoLayoutResizeObserver;

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
let commands: [string, number][];
/** Channels on the film's one audio track. */
let channels = 6;

function Probe() {
  pb = usePlayback();
  return null;
}

beforeEach(() => {
  commands = [];
  channels = 6;
  localStorage.clear();
  resetPrefs();
  resetNativePlaybackAvailability();

  window.lancastMpvAvailable = vi.fn(async () => true);
  window.lancastMpvFeatures = vi.fn(async () => ["audiofx"]);
  window.lancastMpvOpen = vi.fn(async () => {});
  window.lancastMpvCommand = vi.fn(async (n: string, v: number) => {
    commands.push([n, v]);
  });
  window.lancastMpvStop = vi.fn(async () => {});
  window.lancastMpvLayout = vi.fn(async () => {});

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
      if (url.includes("/playback")) return json({ decision: { method: "direct", reason: "" } });
      if (url.includes("/stream-ticket")) return json({ ticket: "tk", item_id: 9, expires_at: 0 });
      if (/\/api\/items\/9(\?|$)/.test(url))
        return json({
          id: 9,
          title: "Fast & Furious",
          kind: "movie",
          container: "mkv",
          duration_ms: 6_420_000,
          progress: { position_ms: 0, watched: false },
          streams: [
            { index: 0, kind: "video", codec: "h264" },
            { index: 1, kind: "audio", codec: "ac3", channels, default: true },
          ],
        });
      if (url.includes("/api/auth")) return json({ user: { role: "admin" }, can_convert: true });
      return json({ items: [], total: 0 });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  delete window.lancastMpvAvailable;
  delete window.lancastMpvFeatures;
  delete window.lancastMpvOpen;
  delete window.lancastMpvCommand;
  delete window.lancastMpvStop;
  delete window.lancastMpvLayout;
  resetNativePlaybackAvailability();
  resetPrefs();
  localStorage.clear();
});

async function settle(ms = 90) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

async function playing() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <FocusProvider>
          <MemoryRouter initialEntries={["/play/9"]}>
            <PlaybackProvider>
              <Probe />
              <Player />
            </PlaybackProvider>
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
  await act(async () => pb.play(9, [9]));
  await settle();
  // The file opening, as the client reports it; the backend sends the current
  // settings on it, and only a loaded backend hears changes afterwards.
  await act(async () =>
    mpvBackend().receive({
      events: ["loadedmetadata"],
      current_time: 0,
      duration: 6420,
      paused: false,
      ended: false,
    }),
  );
  await settle(30);
  commands = [];
}

const button = (name: RegExp) =>
  [...host.querySelectorAll("button")].find((b) => name.test(b.getAttribute("aria-label") ?? ""));
const press = async (b: HTMLButtonElement | undefined) => {
  await act(async () => b?.click());
  await settle(30);
};

describe("the sound toggles in the control bar", () => {
  it("are there on the desktop's own player with a client that has the filters", async () => {
    await playing();
    expect(pb.native).toBe(true);
    expect(button(/^Night mode$/)).toBeDefined();
    expect(button(/^Dialogue boost/)).toBeDefined();
  });

  it("are not there on a client too old to apply them", async () => {
    // The v0.9.44 shape: libmpv present, no features binding.
    delete window.lancastMpvFeatures;
    await playing();
    expect(pb.native).toBe(true);
    expect(button(/^Night mode$/)).toBeUndefined();
    expect(button(/^Dialogue boost/)).toBeUndefined();
  });

  it("night mode toggles, and the client hears it", async () => {
    await playing();
    const night = button(/^Night mode$/);
    expect(night?.getAttribute("aria-pressed")).toBe("false");

    await press(night);
    expect(getPrefs().nightVideo).toBe(true);
    expect(button(/^Night mode$/)?.getAttribute("aria-pressed")).toBe("true");
    expect(commands).toContainEqual(["night", 1]);

    await press(button(/^Night mode$/));
    expect(getPrefs().nightVideo).toBe(false);
    expect(commands).toContainEqual(["night", 0]);
  });

  it("dialogue boost steps Off, Low, High, Off, and says which", async () => {
    await playing();
    expect(button(/^Dialogue boost/)?.getAttribute("aria-label")).toBe("Dialogue boost: Off");

    await press(button(/^Dialogue boost/));
    expect(getPrefs().dialogueVideo).toBe(1);
    expect(button(/^Dialogue boost/)?.getAttribute("aria-label")).toBe("Dialogue boost: Low");
    expect(button(/^Dialogue boost/)?.textContent).toBe("L");
    expect(commands).toContainEqual(["dialogue", 1]);

    await press(button(/^Dialogue boost/));
    expect(button(/^Dialogue boost/)?.textContent).toBe("H");
    expect(commands).toContainEqual(["dialogue", 2]);

    await press(button(/^Dialogue boost/));
    expect(getPrefs().dialogueVideo).toBe(0);
    expect(button(/^Dialogue boost/)?.getAttribute("aria-pressed")).toBe("false");
    expect(button(/^Dialogue boost/)?.textContent).toBe("");
  });

  it("leaves dialogue boost off a mono track, and keeps night mode", async () => {
    channels = 1;
    await playing();
    expect(button(/^Night mode$/)).toBeDefined();
    expect(button(/^Dialogue boost/)).toBeUndefined();
  });

  it("never draws an engaged toggle in gold", async () => {
    await playing();
    await press(button(/^Night mode$/));
    await press(button(/^Dialogue boost/));
    for (const b of [button(/^Night mode$/), button(/^Dialogue boost/)]) {
      expect(b?.className).toContain("is-on");
      expect(b?.className).not.toMatch(/gold/);
    }
  });
});
