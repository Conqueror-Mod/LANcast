/*
 * Subtitle style follows the account (docs/settings-plan.md, decided
 * 2026-10-12). This device's preferences stay the working copy; the sync keeps
 * them and the account in step, in both directions.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { SubtitleStyleSync } from "./SubtitleStyleSync";
import { getPrefs, resetPrefs, setPrefs } from "@/playback/prefs";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let puts: unknown[];

function stub(style: unknown) {
  puts = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const json = (b: unknown) =>
        new Response(JSON.stringify(b), { status: 200, headers: { "Content-Type": "application/json" } });
      if (String(input) === "/api/profile/subtitle-style" && init?.method === "PUT") {
        const body = JSON.parse(String(init.body));
        puts.push(body);
        return json({ style: body });
      }
      return json({ style });
    }),
  );
}

beforeEach(() => {
  localStorage.clear();
  resetPrefs();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  resetPrefs();
});

async function settle(ms = 30) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

async function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <SubtitleStyleSync />
      </QueryClientProvider>,
    );
  });
  await settle();
}

describe("subtitle style and the account", () => {
  it("takes on the account's style when it arrives", async () => {
    stub({ color: "#ffe066", font: "serif", size: 1.8, position: 20 });
    await mount();
    const p = getPrefs();
    expect([p.subColor, p.subFont, p.subSize, p.subPosition]).toEqual(["#ffe066", "serif", 1.8, 20]);
    // Taking it on is not a change of the account's, so nothing goes back.
    await settle(700);
    expect(puts).toEqual([]);
  });

  it("sends a change made here to the account", async () => {
    stub({ color: "#ffffff", font: "sans", size: 1, position: 8 });
    await mount();
    await act(async () => setPrefs({ subSize: 1.35 }));
    await settle(700);
    expect(puts).toEqual([{ color: "#ffffff", font: "sans", size: 1.35, position: 8 }]);
  });

  it("does not push a new device's untouched style onto an account with none", async () => {
    setPrefs({ subColor: "#8fd3ff" });
    stub(null);
    await mount();
    await settle(700);
    expect(puts).toEqual([]);
    // And the device keeps its own.
    expect(getPrefs().subColor).toBe("#8fd3ff");
  });

  it("leaves the offset to this device", async () => {
    stub({ color: "#ffffff", font: "sans", size: 1, position: 8 });
    await mount();
    await act(async () => setPrefs({ subOffset: 1.5 }));
    await settle(700);
    expect(puts).toEqual([]);
  });
});
