/*
 * A click on the docked picture opens the player.
 *
 * Reported: the only way back to the full player was the title in the strip
 * below the picture; the picture itself did nothing. For the browser player the
 * picture is in the page. For native video it is a window of its own above the
 * page, so the client lays a click-catching window over it and calls
 * window.__lancastNativeClick — both routes land on the same handler.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
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
let path = "";

function Probe() {
  pb = usePlayback();
  path = useLocation().pathname;
  return null;
}

beforeEach(() => {
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
      const item = url.match(/\/api\/items\/(\d+)(\?|$)/);
      if (item) {
        return json({ id: Number(item[1]), title: "A Film", kind: "movie", duration_ms: 5_400_000 });
      }
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

async function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={["/library/1"]}>
          <FocusProvider>
            <PlaybackProvider>
              <Routes>
                <Route path="*" element={<Probe />} />
              </Routes>
            </PlaybackProvider>
          </FocusProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await act(async () => pb.play(7, [7]));
  await act(async () => {
    await new Promise((r) => setTimeout(r, 60));
  });
}

describe("the docked picture", () => {
  it("opens the player when clicked", async () => {
    await mount();
    expect(pb.surface).toBe("mini");
    await act(async () => host.querySelector<HTMLElement>(".playback--mini")!.click());
    expect(path).toBe("/watch/7");
  });

  it("opens the player when the desktop client reports a click on the native picture", async () => {
    await mount();
    expect(typeof window.__lancastNativeClick).toBe("function");
    await act(async () => window.__lancastNativeClick!());
    expect(path).toBe("/watch/7");
  });
});
