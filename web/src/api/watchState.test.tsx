/*
 * What a "mark as watched" redraws.
 *
 * The failure is always quiet: the request succeeds, the server is right, and
 * the tick does not appear until something else happens to redraw the page.
 * Marking an episode from its tile's menu refreshed the browse grid and not the
 * season page the tile was on, because that page reads ["children", …] and the
 * hook behind the menu never invalidated it. So these assert on the lists a
 * person could be looking at, by key, after a real mutation.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useSetWatchedByID } from "./hooks";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let qc: QueryClient;
let mark: ReturnType<typeof useSetWatchedByID>;

function Probe() {
  mark = useSetWatchedByID();
  return null;
}

beforeEach(async () => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(null, { status: 204 })),
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

// Lists as they are actually keyed by the hooks that read them.
const onScreen = [
  ["children", 40, ""], // a season's episodes
  ["children", 39, ""], // the show page's season cards
  ["item", 39], // the show's own count of episodes left
  ["collection-members", 7], // a collection's films
  ["search", "futurama"], // search results
  ["items", "infinite", "library_id=1"], // the browse grid
  ["continue", 20], // the Continue shelf
];

describe("marking something watched", () => {
  it("redraws every list that shows it, not only the grid", async () => {
    for (const key of onScreen) qc.setQueryData(key, { seeded: true });

    await act(async () => {
      await mark.mutateAsync({ itemID: 41, watched: true });
    });

    for (const key of onScreen) {
      expect(
        qc.getQueryState(key)?.isInvalidated,
        `${JSON.stringify(key)} was left stale`,
      ).toBe(true);
    }
  });

  it("leaves lists that cannot show watched state alone", async () => {
    qc.setQueryData(["settings"], { seeded: true });
    await act(async () => {
      await mark.mutateAsync({ itemID: 41, watched: false });
    });
    expect(qc.getQueryState(["settings"])?.isInvalidated).toBe(false);
  });
});
