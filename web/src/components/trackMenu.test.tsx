/*
 * The right-click menu on a track row.
 *
 * This row was the crowded one — a playlist entry already carries add, move up,
 * move down and remove — so a menu here is an obvious tidy, and the tidy is the
 * thing this file exists to prevent.
 *
 * The reordering arrows are two buttons rather than a drag handle *on purpose*,
 * because this list is driven by a remote as well as a mouse and a d-pad cannot
 * drag. There is no keyboard route into a context menu, so anything moved into
 * one leaves the remote entirely — and TrackList already carries two comments
 * about capabilities that shipped unreachable. A third would be a pattern.
 *
 * So the menu only ever adds, and the first test here asserts that: every
 * control the row had, it still has. It is written against aria-labels rather
 * than counting buttons, so it names what went missing rather than a number.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { TrackList } from "./TrackList";
import type { Item } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

function track(id: number, title: string, played = false): Item {
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
    progress: { position_ms: 0, watched: played },
  } as unknown as Item;
}

const tracks = [track(7, "Opener"), track(9, "Middle", true)];

let host: HTMLDivElement;
let root: Root;
let requests: { method: string; url: string; body: Record<string, unknown> }[];

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
        body: init?.body ? JSON.parse(String(init.body)) : {},
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
            <PlaybackProvider>
              <TrackList tracks={tracks} playlistID={playlistID} />
            </PlaybackProvider>
          </FocusProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
}

/** aria-labels of every button on the row, which is what a remote can reach. */
function rowLabels(): string[] {
  return [...host.querySelectorAll<HTMLButtonElement>("button")]
    .map((b) => b.getAttribute("aria-label") ?? "")
    .filter(Boolean);
}

function row(title: string): HTMLElement {
  const btn = [...host.querySelectorAll("button.track-row")].find((b) =>
    (b.getAttribute("aria-label") ?? "").includes(title),
  );
  if (!btn) throw new Error(`no row for ${title}`);
  return btn.closest(".track-line") as HTMLElement;
}

function rightClick(el: HTMLElement): void {
  act(() => {
    el.dispatchEvent(
      new MouseEvent("contextmenu", {
        bubbles: true,
        cancelable: true,
        clientX: 30,
        clientY: 30,
      }),
    );
  });
}

function menuLabels(): string[] {
  return [...host.querySelectorAll('[role="menuitem"]')].map((i) =>
    (i.textContent ?? "").trim(),
  );
}

async function pick(label: string): Promise<void> {
  const b = [...host.querySelectorAll('[role="menuitem"]')].find(
    (i) => (i.textContent ?? "").trim() === label,
  ) as HTMLButtonElement | undefined;
  if (!b) throw new Error(`no menu item "${label}" — have: ${menuLabels().join(", ")}`);
  // Awaited, or the mutation is still in flight when the assertion runs.
  await act(async () => {
    b.click();
  });
}

describe("the track row menu", () => {
  /*
   * The one that matters. A menu is a tempting place to put the arrows, and
   * putting them there would take reordering away from the remote this list is
   * explicitly built for.
   */
  it("takes nothing off the row", () => {
    render(4);
    const labels = rowLabels();
    expect(labels).toContain("Move Opener up");
    expect(labels).toContain("Move Opener down");
    expect(labels).toContain("Remove Opener from this playlist");
    expect(labels).toContain("Add Opener to a playlist");
    expect(labels).toContain("Play Opener");
  });

  it("opens on right-click anywhere on the row", () => {
    render();
    rightClick(row("Opener"));
    expect(menuLabels().length).toBeGreaterThan(0);
  });

  /*
   * The genuinely new capability. A track has no poster tile and nothing
   * navigates to its page, so until now only playback could set this flag.
   */
  it("marks a track played, and offers the undo on one already played", async () => {
    render();
    rightClick(row("Opener"));
    expect(menuLabels()).toContain("Mark as played");
    await pick("Mark as played");

    const w = requests.find((r) => r.url.includes("/items/7/progress"));
    expect(w, "nothing was written").toBeTruthy();
    expect(w?.body.watched).toBe(true);

    rightClick(row("Middle"));
    expect(menuLabels()).toContain("Mark as unplayed");
  });

  // A playlist entry can be removed from the list; an album track cannot, and
  // offering it there would be an action with nothing to act on.
  it("offers playlist removal only inside a playlist", () => {
    render(4);
    rightClick(row("Opener"));
    expect(menuLabels()).toContain("Remove from this playlist");

    act(() => root.unmount());
    root = createRoot(host);
    render();
    rightClick(row("Opener"));
    expect(menuLabels()).not.toContain("Remove from this playlist");
  });

  // Removing an entry addresses it by position, because a playlist may hold the
  // same track twice (ADR 0030) and an id cannot tell the copies apart.
  it("removes the entry through the playlist endpoint", async () => {
    render(4);
    rightClick(row("Opener"));
    await pick("Remove from this playlist");
    const del = requests.find(
      (r) => r.method === "DELETE" && r.url.includes("/playlists/4/entries"),
    );
    expect(del, "no entry removal was sent").toBeTruthy();
  });
});
