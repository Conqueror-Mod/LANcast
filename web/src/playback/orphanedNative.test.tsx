/*
 * A new page clears the native player its predecessor left behind.
 *
 * Reported after a port change (ADR 0089): the page moved to the new port, the
 * desktop app's native player kept playing the film in its mini-player, and
 * nothing could stop or control it -- the page that knew about it was gone.
 * A reload or the window coming back after an update restart is the same
 * shape. Anything native playing when a page starts is that orphan.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "./PlaybackProvider";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const w = window as unknown as Record<string, unknown>;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } })),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  delete w.lancastMpvStop;
  delete w.lancastMpvLayout;
  delete w.lancastRetroStop;
});

describe("a page starting in the desktop app", () => {
  it("stops and hides any native film or game left playing", async () => {
    const stop = vi.fn(async () => {});
    const layout = vi.fn(async () => {});
    const retroStop = vi.fn(async () => {});
    w.lancastMpvStop = stop;
    w.lancastMpvLayout = layout;
    w.lancastRetroStop = retroStop;

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    await act(async () => {
      root.render(
        <QueryClientProvider client={qc}>
          <MemoryRouter>
            <FocusProvider>
              <PlaybackProvider>
                <div />
              </PlaybackProvider>
            </FocusProvider>
          </MemoryRouter>
        </QueryClientProvider>,
      );
    });

    expect(stop).toHaveBeenCalled();
    expect(layout).toHaveBeenCalledWith("hidden", 0, 0, 0, 0);
    expect(retroStop).toHaveBeenCalled();
  });
});
