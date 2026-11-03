/*
 * An artist's page lists every track by the artist, under its albums, and
 * plays them: a row continues through the whole discography, Play all queues
 * it in order and Shuffle queues it shuffled.
 *
 * The queue goes in history state rather than ?queue=, because a prolific
 * artist is hundreds of ids — so these read location.state on the player
 * route rather than the URL.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useLocation, useParams } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { Detail } from "./Detail";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const artist = { id: 500, library_id: 3, kind: "artist", title: "Band", missing: false };
const albums = [
  { id: 10, kind: "album", title: "Alpha", missing: false },
  { id: 20, kind: "album", title: "Zebra", missing: false },
];
// Each record's own track numbers restart, which is why the list drops them.
const tracks = [
  { id: 101, kind: "track", title: "First", series: "Alpha", season: 1, episode: 1, missing: false },
  { id: 102, kind: "track", title: "Second", series: "Alpha", season: 2, episode: 1, missing: false },
  { id: 201, kind: "track", title: "Third", series: "Zebra", season: 1, episode: 1, missing: false },
];

let host: HTMLDivElement;
let root: Root;
let urls: string[];
let landed: { id?: string; search: string; state: unknown } | null;

function Watch() {
  const { id } = useParams();
  const loc = useLocation();
  landed = { id, search: loc.search, state: loc.state };
  return null;
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  urls = [];
  landed = null;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      urls.push(url);
      const json = (b: unknown) =>
        new Response(JSON.stringify(b), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/auth/status")) {
        return json({ authenticated: true, configured: true, user: { id: "u1", name: "dana", role: "admin" } });
      }
      if (url.includes(`/api/items/${artist.id}/tracks`)) return json({ items: tracks });
      if (url.includes("parent_id=")) return json({ items: albums, total: albums.length });
      if (/\/api\/items\/\d+$/.test(url.split("?")[0])) return json(artist);
      if (url.includes("/api/items")) return json({ items: [], total: 0 });
      if (url.includes("/api/libraries")) return json([]);
      return json({});
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={[`/item/${artist.id}`]}>
              <Routes>
                <Route path="/item/:id" element={<Detail />} />
                <Route path="/watch/:id" element={<Watch />} />
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

function button(text: string) {
  const b = [...host.querySelectorAll("button")].find(
    (el) => (el.textContent ?? "").replace("▶", "").trim() === text,
  );
  if (!b) throw new Error(`no button "${text}"`);
  return b;
}

async function press(el: HTMLElement) {
  await act(async () => {
    el.click();
  });
}

describe("an artist's track list", () => {
  it("lists every track under the albums, each naming its record", async () => {
    await render();
    expect(urls.some((u) => u.includes(`/api/items/${artist.id}/tracks`))).toBe(true);
    const rows = [...host.querySelectorAll(".track-row")];
    expect(rows.map((r) => r.querySelector(".track-row__title")?.firstChild?.textContent)).toEqual([
      "First",
      "Second",
      "Third",
    ]);
    // One sequence: no disc headings gathering every record's disc 1, and no
    // number column counting 1, 1, 1.
    expect(host.querySelector(".track-list__disc-head")).toBeNull();
    expect(host.querySelector(".track-row__num")).toBeNull();
    expect(rows[2].querySelector(".track-row__artist")?.textContent).toBe("Zebra");
    // Under the albums, not instead of them.
    const sections = [...host.querySelectorAll(".detail__children")];
    expect(sections.length).toBe(2);
    expect(sections[1].textContent).toContain("Tracks");
  });

  it("plays on through the discography from a row, in history state", async () => {
    await render();
    await press(host.querySelector<HTMLElement>('[aria-label="Play Second"]')!);
    expect(landed?.id).toBe("102");
    expect(landed?.search).toBe("");
    expect(landed?.state).toEqual({ queue: [101, 102, 201] });
  });

  it("Play all queues every track in order", async () => {
    await render();
    await press(button("Play all"));
    expect(landed?.id).toBe("101");
    expect(landed?.search).toBe("");
    expect(landed?.state).toEqual({ queue: [101, 102, 201], shuffle: false });
  });

  it("Shuffle queues every track shuffled, from a random start", async () => {
    vi.spyOn(Math, "random").mockReturnValue(0.99);
    await render();
    await press(button("Shuffle"));
    expect(landed?.id).toBe("201");
    expect(landed?.state).toEqual({ queue: [101, 102, 201], shuffle: true });
  });
});
