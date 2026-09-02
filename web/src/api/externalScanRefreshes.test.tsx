/*
 * A scan nobody in this client started still has to refresh what it changed.
 *
 * scanRefreshesCounts covers the scan a person presses: the mutation claims the
 * work optimistically, so the poll finds an edge even when the scan is already
 * over. That claim is the whole mechanism, and nothing makes it when the scan
 * was started by the daily timer, by another client, or by files arriving from
 * somewhere else and a rescan following them.
 *
 * Reported from a real install: tracks written by another tool were scanned
 * correctly and did not appear in Recently Added "for some time". The server
 * was right, the request succeeded, and only the picture was stale — the shape
 * this project's most-repeated bug always takes.
 *
 * `completed_at` is what closes it. It is monotonic, so it does not matter
 * whether any poll saw the work running.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useActivity, useRecentlyAddedMusic } from "./hooks";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let recentReads: number;
let completedAt: number;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  recentReads = 0;
  completedAt = 1000;

  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/activity")) {
        // Never active. The scan begins and ends between two polls, which is
        // ordinary: an incremental music scan finishes in about four seconds
        // and the idle interval is eight.
        return json({ active: false, tasks: [], completed_at: completedAt });
      }
      if (url.includes("/api/items")) {
        recentReads++;
        return json({ items: [] });
      }
      return json({});
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

function Probe() {
  useActivity();
  useRecentlyAddedMusic();
  return null;
}

async function render() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <Probe />
      </QueryClientProvider>,
    );
  });
  await settle();
}

async function settle(ms = 20) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

describe("a scan this client did not start", () => {
  it("refreshes Recently Added when the completion stamp moves", async () => {
    await render();
    const before = recentReads;
    expect(before).toBeGreaterThan(0);

    // The scan happened entirely between two polls: never active, but the
    // server's stamp has moved on.
    completedAt = 2000;
    await act(async () => {
      await vi.waitFor(async () => {
        if (recentReads <= before) throw new Error("not yet");
      }, { timeout: 12_000, interval: 50 });
    });

    expect(recentReads).toBeGreaterThan(before);
  }, 20_000);

  it("does not refresh when nothing has finished", async () => {
    await render();
    const before = recentReads;

    // Several idle polls with an unchanged stamp. A poll that invalidated on
    // every tick would refetch the whole app every eight seconds for ever.
    await settle(300);

    expect(recentReads).toBe(before);
  });

  it("does not refresh on the very first look", async () => {
    // The first poll has no previous stamp. Treating "I have not looked
    // before" as "work just finished" would refetch everything on launch.
    await render();
    const afterFirst = recentReads;
    await settle(100);
    expect(recentReads).toBe(afterFirst);
  });
});
