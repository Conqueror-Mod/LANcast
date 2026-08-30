/*
 * One Play all on an artist page, not two.
 *
 * Reported on Ashnikko, whose library rows are an artist with a single track
 * parented straight to it — a file in the artist's folder with no album folder
 * around it.
 *
 * That shape rendered both buttons. The generic container Play all found a
 * playable child (the track) where an artist normally has only albums, and the
 * artist's own Play all was handed that track's id where an album id was
 * expected, asked the server for its children, and built an empty queue. So the
 * page offered two buttons and the one that means "play the discography" did
 * nothing at all.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { Detail } from "./Detail";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const artist = {
  id: 26621,
  library_id: 3,
  kind: "artist",
  title: "Ashnikko",
  year: null,
  missing: false,
  child_count: 1,
};

let host: HTMLDivElement;
let root: Root;

/** children is what the artist page finds under the artist. */
function mount(children: Record<string, unknown>[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (b: unknown) =>
        new Response(JSON.stringify(b), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      if (url.includes("parent_id=") || url.includes("/children")) {
        return json({ items: children, total: children.length });
      }
      if (/\/api\/items\/\d+$/.test(url.split("?")[0])) return json(artist);
      if (url.includes("/api/items")) return json({ items: [], total: 0 });
      if (url.includes("/api/libraries")) return json([]);
      return json({});
    }),
  );
}

beforeEach(() => {
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
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={[`/item/${artist.id}`]}>
              <Routes>
                <Route path="/item/:id" element={<Detail />} />
              </Routes>
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 3; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

function playAlls() {
  // includes rather than startsWith: one of the two carries a play glyph, and
  // matching on the prefix silently found neither.
  return [...host.querySelectorAll("button")].filter((b) =>
    b.textContent?.includes("Play all"),
  );
}

describe("an artist's Play all", () => {
  it("is one button when a track sits directly under the artist", async () => {
    mount([{ id: 9856, kind: "track", title: "Halloweenie VI", missing: false }]);
    await render();
    expect(playAlls().length).toBe(1);
  });

  it("is one button for an ordinary artist with albums", async () => {
    mount([{ id: 10, kind: "album", title: "Weedkiller", missing: false }]);
    await render();
    expect(playAlls().length).toBe(1);
  });

  it("is one button when the artist has both", async () => {
    // The shape that produced two: an album for the generic rule to ignore and
    // a track for it to find.
    mount([
      { id: 10, kind: "album", title: "Weedkiller", missing: false },
      { id: 9856, kind: "track", title: "Halloweenie VI", missing: false },
    ]);
    await render();
    expect(playAlls().length).toBe(1);
  });
});
