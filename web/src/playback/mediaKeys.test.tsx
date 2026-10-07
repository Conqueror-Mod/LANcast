/*
 * The keyboard's media keys in the desktop app.
 *
 * The host hooks them (winkeys_windows.go) and answers them only while the page
 * says something is in the player; otherwise they pass to whatever else wants
 * them. So two things are asserted through the real provider: the page claims
 * the keys exactly while something is loaded, and the commands it is handed do
 * what the keys say.
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

type KeyWindow = Window & {
  lancastMediaKeys?: (on: boolean) => Promise<unknown>;
  __lancastMediaKey?: (cmd: string) => void;
};

let host: HTMLDivElement;
let root: Root;
let pb: ReturnType<typeof usePlayback>;
let claims: boolean[];

function Probe() {
  pb = usePlayback();
  return null;
}

beforeEach(() => {
  claims = [];
  (window as KeyWindow).lancastMediaKeys = vi.fn(async (on: boolean) => {
    claims.push(on);
  });
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
      if (url.includes("/playback")) {
        return json({ decision: { method: "direct", reason: "" } });
      }
      const item = url.match(/\/api\/items\/(\d+)(\?|$)/);
      if (item) {
        const id = Number(item[1]);
        return json({ id, title: `Episode ${id}`, kind: "episode", duration_ms: 1_300_000 });
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
  delete (window as KeyWindow).lancastMediaKeys;
});

async function settle(ms = 40) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

async function render() {
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
}

const press = async (cmd: string) => {
  await act(async () => {
    (window as KeyWindow).__lancastMediaKey!(cmd);
  });
  await settle();
};

describe("media keys", () => {
  it("are claimed only while something is in the player", async () => {
    await render();
    expect(claims.at(-1), "claimed the keys with nothing playing").toBe(false);

    await act(async () => {
      pb.play(1, [1, 2]);
    });
    await settle();
    expect(claims.at(-1), "did not claim the keys for a playing episode").toBe(true);

    await act(async () => {
      pb.stop();
    });
    await settle();
    expect(claims.at(-1), "kept the keys after playback stopped").toBe(false);
  });

  it("do what they say", async () => {
    await render();
    expect((window as KeyWindow).__lancastMediaKey).toBeTypeOf("function");
    await act(async () => {
      pb.play(1, [1, 2]);
    });
    await settle();

    await press("next");
    expect(pb.itemID, "Next did not move to the next episode").toBe(2);
    await press("previous");
    expect(pb.itemID, "Previous did not move back").toBe(1);

    const video = document.querySelector("video")!;
    Object.defineProperty(video, "paused", { configurable: true, value: false });
    await press("playpause");
    expect(video.pause, "Play/Pause did not pause a playing film").toHaveBeenCalled();

    await press("stop");
    expect(pb.itemID, "Stop left the player loaded").toBe(0);
  });
});
