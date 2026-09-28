import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route, useLocation } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PeerLibrary } from "./PeerLibrary";
import { PeerPlayer } from "./PeerPlayer";

/*
 * Playing something on somebody else's server (ADR 0071 §5).
 *
 * Every test here is about one failure, because it is the only one on this
 * path that does not announce itself: an id from another server names a
 * **different item here**, and both the tile and the player have a local route
 * one mistake away that would answer perfectly and show the wrong film.
 *
 * jsdom decodes nothing, so none of this proves a film plays. What it proves
 * is where the element was pointed, which is the part that can be wrong
 * silently.
 */

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const FP = "AAAABBBBCCCC";
let host: HTMLDivElement;
let root: Root;

function mockServer(opts: {
  items?: { id: number; title: string }[];
  method?: string;
  subtitles?: unknown[];
  playbackFails?: boolean;
}) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), {
          status,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/playback")) {
        if (opts.playbackFails) {
          return json({ error: { code: "peer_unreachable" } }, 502);
        }
        return json({
          decision: { method: opts.method ?? "direct", reason: "" },
        });
      }
      if (url.includes("/subtitles"))
        return json({ subtitles: opts.subtitles ?? [] });
      if (url.includes("/items")) {
        return json({
          items: opts.items ?? [],
          total: (opts.items ?? []).length,
        });
      }
      if (url.includes("/libraries")) {
        return json({ libraries: [{ id: 3, name: "Films", kind: "movie" }] });
      }
      return json({});
    }),
  );
}

// Where the router ended up, so a navigation can be asserted as a destination
// rather than as a spy on a mock.
let landed = "";
function Probe() {
  landed = useLocation().pathname;
  return null;
}

async function render(ui: React.ReactNode, at: string) {
  landed = "";
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchInterval: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <FocusProvider>
          <MemoryRouter initialEntries={[at]}>
            <Probe />
            <Routes>
              <Route path="/peers/:fingerprint/library/:library" element={ui} />
              <Route
                path="/peers/:fingerprint/item/:item"
                element={<PeerPlayer />}
              />
              <Route path="/item/:id" element={<div>LOCAL ITEM PAGE</div>} />
            </Routes>
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 25; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("a tile in somebody else's library", () => {
  /*
   * The failure the whole file exists for.
   *
   * PosterTile's default is `/item/{id}`, which on this server is a real page
   * holding a real and *different* film. Nothing would fail; the wrong thing
   * would simply appear, which is the merging §5 forbids arriving through the
   * one door nobody was watching.
   */
  it("opens the film on their server, never ours", async () => {
    mockServer({ items: [{ id: 42, title: "Their Film" }] });
    await render(<PeerLibrary />, `/peers/${FP}/library/3`);

    // The tile itself, not the header's back-link, which is also an anchor
    // and comes first in the document.
    const tile = host.querySelector<HTMLElement>("button.poster-tile");
    expect(tile, "no tile rendered").toBeTruthy();

    await act(async () => {
      tile!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    for (let i = 0; i < 10; i++) {
      await act(async () => {
        await new Promise((r) => setTimeout(r, 0));
      });
    }

    expect(landed).toBe(`/peers/${FP}/item/42`);
    expect(host.textContent).not.toContain("LOCAL ITEM PAGE");
  });
});

describe("the peer player", () => {
  // Their decision, their route. This household holds neither the file nor
  // the probe, so it has nothing to decide with.
  it("asks their server how to deliver it and points the element there", async () => {
    mockServer({ method: "direct" });
    await render(<PeerLibrary />, `/peers/${FP}/item/42`);

    const video = host.querySelector("video");
    expect(video, "no media element").toBeTruthy();
    expect(video!.getAttribute("src")).toBe(`/api/peers/${FP}/stream?item=42`);
  });

  /*
   * A converted film takes the playlist route where the engine has one, and
   * the item must be in the *path*: a playlist names its segments with a
   * prefix and cannot carry a query string.
   *
   * jsdom's canPlayType answers "" for everything, so this forces the branch
   * rather than pretending jsdom has HLS.
   */
  it("uses a route the element can be given, and never a local one", async () => {
    mockServer({ method: "transcode" });
    await render(<PeerLibrary />, `/peers/${FP}/item/42`);

    const src = host.querySelector("video")!.getAttribute("src")!;
    expect(src.startsWith(`/api/peers/${FP}/`)).toBe(true);
    expect(src).not.toMatch(/^\/api\/(stream|items)\//);
  });

  // Their tracks, fetched through the proxy like everything else.
  it("offers their subtitle tracks", async () => {
    mockServer({
      method: "direct",
      subtitles: [
        {
          key: "en",
          label: "English",
          language: "en",
          available: true,
          default: true,
        },
        {
          key: "pgs",
          label: "PGS",
          language: "en",
          available: false,
          default: false,
        },
      ],
    });
    await render(<PeerLibrary />, `/peers/${FP}/item/42`);

    const tracks = [...host.querySelectorAll("track")];
    expect(tracks).toHaveLength(1); // the image-based one is not offerable
    expect(tracks[0].getAttribute("src")).toBe(
      `/api/peers/${FP}/subtitles/42/en`,
    );
  });

  /*
   * A server that is off is said as a fact about them.
   *
   * It is the ordinary state of another household's machine, and a screen that
   * called it an error here would send somebody through their own settings
   * looking for it.
   */
  it("says plainly when their server is not answering", async () => {
    mockServer({ playbackFails: true });
    await render(<PeerLibrary />, `/peers/${FP}/item/42`);

    expect(host.textContent).toContain("not answering");
    expect(host.querySelector("video")).toBeNull();
  });

  /*
   * Nothing is recorded, which is ADR 0071 §4 left genuinely open rather than
   * answered by accident. A peer item must never reach this household's watch
   * history or Continue Watching.
   */
  it("writes no progress anywhere", async () => {
    mockServer({ method: "direct" });
    await render(<PeerLibrary />, `/peers/${FP}/item/42`);

    const calls = (fetch as unknown as { mock: { calls: unknown[][] } }).mock
      .calls;
    for (const [, init] of calls) {
      const method = (init as RequestInit | undefined)?.method ?? "GET";
      expect(method).toBe("GET");
    }
  });
});
