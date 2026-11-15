/*
 * What clearing watch history redraws (R13-2).
 *
 * Home's "On this day" draws its watched cards from the finished viewings the
 * reset deletes, under ["memories", 20] with a fifteen-minute staleTime. The
 * reset did not invalidate that key, so going Home after clearing history
 * showed the history back. These assert, by key, on every list a person could
 * be looking at that reads history, after a real mutation.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useResetHistory } from "./hooks";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let qc: QueryClient;
let reset: ReturnType<typeof useResetHistory>;

function Probe() {
  reset = useResetHistory();
  return null;
}

beforeEach(async () => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(JSON.stringify({ removed: 3, scope: "finished" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    ),
  );
  qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <Probe />
      </QueryClientProvider>,
    );
  });
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

// Keyed as the hooks that read them key them.
const readsHistory = [
  ["memories", 20], // Home's "On this day" — the finding
  ["year-in-review", "current"], // the year's totals
  ["to-rate"], // Rate what you've watched
  ["history-count", "finished"],
  ["profile", "viewings", 50, ""],
  ["continue", "movies", 20],
  ["item", 41], // a detail page's tick
  ["children", 40, ""], // a season's ticks
  ["collection-members", 7],
  ["search", "futurama"],
  ["items", "infinite", "library_id=1"],
  ["recently-added", "video", 20],
  ["review", 0],
];

describe("clearing watch history", () => {
  it("redraws every list that reads history", async () => {
    for (const key of readsHistory) qc.setQueryData(key, { seeded: true });

    await act(async () => {
      await reset.mutateAsync({ scope: "finished" });
    });

    for (const key of readsHistory) {
      expect(
        qc.getQueryState(key)?.isInvalidated,
        `${JSON.stringify(key)} was left showing cleared history`,
      ).toBe(true);
    }
  });

  it("leaves lists that do not read history alone", async () => {
    qc.setQueryData(["settings"], { seeded: true });
    await act(async () => {
      await reset.mutateAsync({ scope: "all" });
    });
    expect(qc.getQueryState(["settings"])?.isInvalidated).toBe(false);
  });
});
