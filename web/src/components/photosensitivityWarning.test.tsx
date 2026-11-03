/*
 * The photosensitivity warning (PhotosensitivityWarning.tsx), beside the Calm
 * switch in This device › Player, where anybody who watches a visualizer can
 * find it, not only an administrator.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { PlayerSettings } from "./PlayerSettings";
import { MilkdropSettings } from "./MilkdropSettings";

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
    vi.fn(async () => {
      return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
    }),
  );
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
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

const warning = () => host.querySelector('[role="note"][aria-label="Photosensitivity warning"]');

describe("the photosensitivity warning", () => {
  it("is in the Player pane, beside the Calm switch, and says to keep to the cover", async () => {
    await render();
    const w = warning();
    expect(w).not.toBeNull();
    expect(w!.textContent).toContain("seizure");
    expect(w!.textContent).toContain("keep Now Playing on the cover");
    const calm = [...host.querySelectorAll("label")].find((l) => l.textContent?.includes("Calm visualizers"));
    expect(calm?.nextElementSibling).toBe(w);
  });

  it("makes no promise that the limits make it safe", async () => {
    await render();
    expect(warning()!.textContent).toContain("no setting can rule a reaction out");
  });

  it("is also in the Visualizers pane, where presets are uploaded", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    await act(async () => {
      root.render(
        <QueryClientProvider client={qc}>
          <MilkdropSettings />
        </QueryClientProvider>,
      );
    });
    expect(warning()).not.toBeNull();
  });
});
