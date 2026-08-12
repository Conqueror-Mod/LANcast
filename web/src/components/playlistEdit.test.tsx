/*
 * A playlist's rows, and what pressing their controls actually sends.
 *
 * The reason this is a test and not a look at the screen: every failure mode
 * here is invisible. A reorder that drops the duplicate, a remove that deletes
 * the *other* copy of the same track, a move that sends the album's ids instead
 * of the playlist's — all of them render a plausible list. The only thing that
 * tells them apart is the request, so the request is what is asserted.
 *
 * The list under test holds the same track twice on purpose. That is the one
 * property a playlist has and no other listing does (ADR 0030), and it is the
 * one that quietly breaks: keyed on id, "a b a" becomes "a b".
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { TrackList } from "./TrackList";
import type { Item } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

function track(id: number, title: string): Item {
  return {
    id,
    library_id: 1,
    kind: "track",
    title,
    year: null,
    series: null,
    season: null,
    episode: null,
    artist: null,
  } as unknown as Item;
}

// a, b, a — the repeat is the point.
const tracks = [track(7, "Opener"), track(9, "Middle"), track(7, "Opener")];

let host: HTMLDivElement;
let root: Root;
let requests: { method: string; url: string; body: unknown }[];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  requests = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      requests.push({
        method: init?.method ?? "GET",
        url,
        body: init?.body ? JSON.parse(String(init.body)) : undefined,
      });
      return new Response(null, { status: 204 });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

function render(playlistID?: number) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  act(() => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <FocusProvider>
            <TrackList tracks={tracks} playlistID={playlistID} />
          </FocusProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
}

function buttons(label: RegExp): HTMLButtonElement[] {
  return [...host.querySelectorAll<HTMLButtonElement>("button")].filter((b) =>
    label.test(b.getAttribute("aria-label") ?? ""),
  );
}

// The component also reads auth status (for the admin-only file delete) and
// the odd item detail. Only the writes are the subject here.
function writes() {
  return requests.filter((r) => r.method !== "GET");
}

async function press(b: HTMLButtonElement) {
  await act(async () => {
    b.click();
  });
}

describe("playlist rows", () => {
  it("offers no edits when the list is not a playlist", () => {
    render(undefined);
    expect(buttons(/^Move /)).toHaveLength(0);
    expect(buttons(/from this playlist$/)).toHaveLength(0);
  });

  /*
   * Reachability, not decoration. "Add to playlist" also exists on an item's
   * detail page — where it is useless for music, because nothing in the client
   * navigates to a track's detail page: tracks are rows here and never poster
   * tiles. It shipped that way in v0.6.10 and no one with a music library could
   * reach it. The row is the only place a track can actually be pressed, so the
   * control has to be here, on every list and not only on playlists.
   */
  it("offers add-to-playlist on every row, playlist or not", () => {
    render(undefined);
    expect(buttons(/to a playlist$/)).toHaveLength(3);
    render(12);
    expect(buttons(/to a playlist$/)).toHaveLength(3);
  });

  it("renders every entry, including the repeat", () => {
    render(12);
    // Three rows, not two: keying on id would collapse the repeat and silently
    // shorten the list.
    expect(buttons(/^Play /)).toHaveLength(3);
  });

  it("numbers rows by position, not by the track's own number", () => {
    render(12);
    const nums = [...host.querySelectorAll(".track-row__num")].map(
      (n) => n.textContent,
    );
    expect(nums).toEqual(["1", "2", "3"]);
  });

  it("sends the whole sequence on a move, duplicates intact", async () => {
    render(12);
    // Move the middle track up: 7,9,7 → 9,7,7.
    await press(buttons(/^Move Middle up$/)[0]);
    expect(writes()).toHaveLength(1);
    expect(writes()[0].method).toBe("PUT");
    expect(writes()[0].url).toBe("/api/playlists/12/entries");
    expect(writes()[0].body).toEqual({ item_ids: [9, 7, 7] });
  });

  it("removes by position, so the pressed copy goes and the other stays", async () => {
    render(12);
    // The third row is the second copy of track 7. Addressed by position 2 —
    // by id there would be no way to say which one was meant.
    await press(buttons(/from this playlist$/)[2]);
    expect(writes()).toHaveLength(1);
    expect(writes()[0].method).toBe("DELETE");
    expect(writes()[0].url).toBe("/api/playlists/12/entries/2");
  });

  it("does not offer to move the ends off the list", () => {
    render(12);
    const up = buttons(/^Move .* up$/);
    const down = buttons(/^Move .* down$/);
    expect(up[0].disabled).toBe(true);
    expect(down[down.length - 1].disabled).toBe(true);
    expect(up[1].disabled).toBe(false);
  });
});
