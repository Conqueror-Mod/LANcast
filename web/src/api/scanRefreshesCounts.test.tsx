/*
 * Starting a scan has to refresh the nav's item counts when it finishes.
 *
 * Reported from a real install: the sidebar said "TV Shows 15" while the grid
 * header, on the same screen, said 12 — a count from before the library had
 * been tidied, and it stayed wrong indefinitely. Pressing Scan also appeared to
 * do nothing, so it got pressed several times.
 *
 * One cause behind both. The activity poll refreshes the counts only when it
 * *observes* work go from active to idle, and when idle it polls every eight
 * seconds. A small library finishes well inside that window, so the scan was
 * never seen running, the edge never happened, and nothing else invalidates the
 * library list.
 *
 * These tests therefore assert the *edge*, not the request: that a scan whose
 * work is already over by the first poll still refreshes the counts, and that
 * an unrelated idle poll does not.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useActivity, useLibraries, useStartScan } from "./hooks";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let libraryReads: number;
let startScan: (id: number) => void;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  libraryReads = 0;

  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if ((init?.method ?? "GET") !== "GET") return json({});
      if (url.includes("/api/activity")) {
        // The scan is already over by the time anything polls — the case the
        // eight-second idle interval makes ordinary on a small library.
        return json({ active: false, tasks: [] });
      }
      if (url.includes("/api/libraries")) {
        libraryReads++;
        return json([{ id: 1, name: "TV Shows", kind: "show", item_count: 12 }]);
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

// Probe mounts the three hooks the shell really has mounted: the activity
// indicator lives in AppShell, the nav reads the library list, and the settings
// pane owns the button.
function Probe() {
  useActivity();
  useLibraries();
  const scan = useStartScan();
  startScan = (id: number) => scan.mutate(id);
  return null;
}

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
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

describe("starting a scan", () => {
  it("refreshes the library counts even when the scan is over before the first poll", async () => {
    await render();
    const before = libraryReads;
    expect(before).toBeGreaterThan(0);

    await act(async () => {
      startScan(1);
    });
    await settle();

    // The count query was read again, which is what puts the right number in
    // the nav. Without the mutation claiming the work started, the poll sees
    // idle-then-idle, finds no edge, and the sidebar keeps its old number.
    expect(libraryReads).toBeGreaterThan(before);
  });

  it("does not refresh the counts when nothing was started", async () => {
    await render();
    const before = libraryReads;

    // Long enough for an idle poll or two, with no scan in between.
    await settle(60);

    expect(libraryReads).toBe(before);
  });
});
