/*
 * Home's photo shelves follow "Mark sensitive" and "Remove" (R13-3).
 *
 * "On this day" reads ["memories", 20] (fifteen-minute staleTime) and New this
 * week's photo row reads ["recent-photos", 20] (thirty seconds). Both are
 * siblings of ["items"], so invalidating ["items"] never reached them: a photo
 * just marked sensitive was drawn unblurred on Home, and a removed one stayed
 * there. These assert on the keys, after a real mutation.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useDeleteItem, useSetSensitive } from "./hooks";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let qc: QueryClient;
let mark: ReturnType<typeof useSetSensitive>;
let remove: ReturnType<typeof useDeleteItem>;

function Probe() {
  mark = useSetSensitive();
  remove = useDeleteItem(41);
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

// Home's two photo shelves, keyed as Home asks for them.
const homeShelves = [
  ["memories", 20],
  ["recent-photos", 20],
];

// Everything else that draws a thumbnail from a key of its own.
const thumbnails = [
  ...homeShelves,
  ["items", "infinite", "library_id=3"],
  ["children", 12, ""],
  ["collection-members", 7],
  ["playlist-entries", 5],
  ["search", "beach"],
  ["recently-added", "video", 20],
  ["duplicates", 3],
  ["photo-search", 3, "beach"],
];

function expectInvalidated(keys: unknown[][], what: string) {
  for (const key of keys) {
    expect(
      qc.getQueryState(key)?.isInvalidated,
      `${JSON.stringify(key)} kept the photo as it was before ${what}`,
    ).toBe(true);
  }
}

describe("marking a photo sensitive", () => {
  it("redraws Home's photo shelves and every list of thumbnails", async () => {
    for (const key of thumbnails) qc.setQueryData(key, { seeded: true });
    await act(async () => {
      await mark.mutateAsync({ id: 41, sensitive: true });
    });
    expectInvalidated(thumbnails, "the mark");
  });
});

describe("removing a photo", () => {
  it("takes it off Home's photo shelves", async () => {
    for (const key of thumbnails) qc.setQueryData(key, { seeded: true });
    await act(async () => {
      await remove.mutateAsync("ignore");
    });
    expectInvalidated(thumbnails, "the removal");
  });
});
