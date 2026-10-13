/*
 * The Skip intros switch in This device › Player (ADR 0083). It is per
 * account, so it reads and writes the server rather than this device's prefs.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { PlayerSettings } from "./PlayerSettings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let puts: string[];

beforeEach(() => {
  puts = [];
  let stored = true;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (String(url).includes("/api/profile/playback")) {
        if (init?.method === "PUT") {
          puts.push(String(init.body));
          stored = JSON.parse(String(init.body)).skip_intros;
        }
        return new Response(JSON.stringify({ skip_intros: stored }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
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

const box = () =>
  [...host.querySelectorAll("label")]
    .find((l) => l.textContent?.includes("Skip intros automatically"))
    ?.querySelector("input");

describe("the Skip intros switch", () => {
  it("shows the account's answer and sends a change to the account", async () => {
    await render();
    expect(box()?.checked).toBe(true);
    await act(async () => box()!.click());
    expect(puts).toEqual([JSON.stringify({ skip_intros: false })]);
  });
});
