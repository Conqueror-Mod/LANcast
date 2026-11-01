/*
 * This device › Player › Theater output (docs/theater-output-plan.md). Shown
 * only where the window's own player says it can match the display: a browser
 * has no such player, and an older client would refuse the setting.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { PlayerSettings } from "./PlayerSettings";
import { resetNativePlaybackAvailability } from "@/playback/mpvBackend";
import { getPrefs, resetPrefs } from "@/playback/prefs";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } })),
  );
  resetNativePlaybackAvailability();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  delete window.lancastMpvFeatures;
  resetNativePlaybackAvailability();
  resetPrefs();
});

async function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <PlayerSettings />
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

const matchBox = () =>
  [...host.querySelectorAll("label")]
    .find((l) => l.textContent?.includes("Match the display"))
    ?.querySelector("input");
const settleRow = () =>
  [...host.querySelectorAll("label")].find((l) => l.textContent?.includes("Wait for the screen"));

const fullscreenBox = () =>
  [...host.querySelectorAll("label")]
    .find((l) => l.textContent?.includes("Open films fullscreen"))
    ?.querySelector("input");

describe("Theater output", () => {
  it("opens films fullscreen by default once matching is on, and can be turned off", async () => {
    window.lancastMpvFeatures = vi.fn(async () => ["refresh"]);
    await render();
    expect(fullscreenBox()).toBeUndefined();
    await act(async () => matchBox()!.click());
    expect(fullscreenBox()?.checked).toBe(true);
    await act(async () => fullscreenBox()!.click());
    expect(getPrefs().fullscreenFilms).toBe(false);
  });

  it("is not drawn in a browser, which has no native player", async () => {
    await render();
    expect(host.textContent).not.toContain("Theater output");
  });

  it("is not drawn for a client player without the feature", async () => {
    window.lancastMpvFeatures = vi.fn(async () => ["audiofx"]);
    await render();
    expect(matchBox()).toBeUndefined();
  });

  it("offers matching where the player can do it, off by default", async () => {
    window.lancastMpvFeatures = vi.fn(async () => ["audiofx", "refresh"]);
    await render();
    expect(matchBox()?.checked).toBe(false);
    // The wait only means something once matching is on.
    expect(settleRow()).toBeUndefined();
  });

  it("turning it on saves it and offers the wait", async () => {
    window.lancastMpvFeatures = vi.fn(async () => ["refresh"]);
    await render();
    await act(async () => matchBox()!.click());
    expect(getPrefs().matchRate).toBe(true);
    const select = settleRow()?.querySelector("select");
    expect(select?.value).toBe("3");
    await act(async () => {
      select!.value = "5";
      select!.dispatchEvent(new Event("change", { bubbles: true }));
    });
    expect(getPrefs().matchSettle).toBe(5);
  });
});
