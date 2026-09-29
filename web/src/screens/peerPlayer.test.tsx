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
  items?: { id: number; title: string; artwork?: { poster?: string } }[];
  total?: number;
  durationMs?: number;
  method?: string;
  subtitles?: unknown[];
  playbackFails?: boolean;
  /** How the peer refuses the items listing, when it does. */
  itemsFail?: { status: number; code: string };
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
      // What the far server says about one item: its name and its length.
      if (/\/item\/\d+$/.test(url.split("?")[0]))
        return json({ title: "Their Film", duration_ms: opts.durationMs ?? 7_200_000 });
      if (url.includes("/items")) {
        if (opts.itemsFail) {
          return json(
            { error: { code: opts.itemsFail.code, message: "no" } },
            opts.itemsFail.status,
          );
        }
        // Pages, because the client asks for one. A stub that ignored limit
        // and offset would make a paging bug invisible to every test here.
        const all = opts.items ?? [];
        const q = new URL(url, "https://x").searchParams;
        const offset = Number(q.get("offset") ?? 0);
        const limit = Number(q.get("limit") ?? 200);
        return json({
          items: all.slice(offset, offset + limit),
          total: opts.total ?? all.length,
        });
      }
      if (url.includes("/libraries")) {
        return json({ libraries: [{ id: 3, name: "Films", kind: "movie" }] });
      }
      return json({});
    }),
  );
}


/*
 * Move a range input the way a person does.
 *
 * Setting `.value` directly is not enough: React keeps a tracker of the last
 * value it saw on the node, and assigning to the property updates that tracker
 * too — so React concludes nothing changed and never calls onChange. Going
 * through the prototype's setter changes the DOM value while leaving the
 * tracker stale, which is what makes the dispatched event look like a real one.
 *
 * Two attempts here dispatched "change" and then set `.value` first; both
 * produced a test that reported the seek had not happened when in fact it had
 * never been asked for.
 */
function drag(bar: HTMLInputElement, to: number) {
  const setter = Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    "value",
  )!.set!;
  setter.call(bar, String(to));
  bar.dispatchEvent(new Event("input", { bubbles: true }));
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
   * The beat that makes this visible as watching (ADR 0045 §10).
   *
   * **This test replaces one that certified the bug.** The old version asserted
   * that every request on this screen was a GET — true of progress, which a
   * peer item deliberately never writes, and it went on passing after a PUT
   * heartbeat was added in the release that was supposed to fix presence. It
   * passed *because* the beat never fired: the listeners were attached to a
   * ref that was null while the decision was loading, and the effect never ran
   * again. A green test reported the absence of the thing it was meant to allow.
   *
   * So the assertion is now the positive one. A beat that stops happening fails
   * here rather than being read as good news.
   */
  it("tells this server it is watching one of theirs", async () => {
    mockServer({ method: "direct" });
    await render(<PeerLibrary />, `/peers/${FP}/item/42`);

    const el = host.querySelector("video")!;
    // jsdom fires no media events by itself: nothing decodes, so nothing ever
    // reaches `playing`. Dispatching it is standing in for the browser, and it
    // is exactly the moment the real element emits.
    await act(async () => {
      el.dispatchEvent(new Event("playing"));
    });

    const beats = (fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls
      .filter(([url, init]) =>
        String(url).includes("/watching") &&
        (init as RequestInit | undefined)?.method === "PUT");

    expect(beats.length).toBeGreaterThan(0);
    expect(String(beats[0][0])).toBe(`/api/peers/${FP}/watching?item=42`);
  });

  // And it stops when the picture does. A beat that continued through a pause
  // would leave a false statement about the present standing, which is the one
  // thing ADR 0045 exists not to make.
  it("stops telling it once the picture stops", async () => {
    mockServer({ method: "direct" });
    await render(<PeerLibrary />, `/peers/${FP}/item/42`);

    const el = host.querySelector("video")!;
    await act(async () => {
      el.dispatchEvent(new Event("playing"));
    });
    const calls = () =>
      (fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls.filter(
        ([url]) => String(url).includes("/watching"),
      ).length;

    const afterPlaying = calls();
    await act(async () => {
      el.dispatchEvent(new Event("pause"));
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(calls()).toBe(afterPlaying);
  });

  /*
   * No progress is written, which is the §4 decision and separate from the beat
   * above: presence says *now* and leaves nothing behind, and where a friend's
   * progress should live is genuinely undecided.
   */
  it("writes no progress anywhere", async () => {
    mockServer({ method: "direct" });
    await render(<PeerLibrary />, `/peers/${FP}/item/42`);

    const calls = (fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls;
    for (const [url, init] of calls) {
      const method = (init as RequestInit | undefined)?.method ?? "GET";
      if (method !== "GET") {
        // The only write this screen may make is the presence beat.
        expect(String(url)).toContain("/watching");
      }
    }
  });
});

/*
 * A peer's library is bigger than one page (ADR 0071 §5).
 *
 * Shipped showing the first sixty items with the *total* printed beside them,
 * so a library of 1,395 films rendered sixty tiles ending in the A's under the
 * number 1,395. Reported by the person looking at it, who reasonably read it
 * as the list being broken rather than as one page of it.
 *
 * The count is the part worth asserting: truncating silently is a bug, and
 * truncating while displaying a number that contradicts the truncation is a
 * different and worse one.
 */
describe("a peer library with more than one page", () => {
  const many = Array.from({ length: 200 }, (_, i) => ({
    id: i + 1,
    title: `Film ${String(i + 1).padStart(4, "0")}`,
  }));

  it("says how many are here as well as how many there are", async () => {
    mockServer({ items: many, total: 1395 });
    await render(<PeerLibrary />, `/peers/${FP}/library/3`);

    expect(host.textContent).toContain("200 of 1,395");
    expect(host.querySelectorAll("button.poster-tile")).toHaveLength(200);
  });

  it("fetches the next page when asked, and keeps what it had", async () => {
    mockServer({ items: many, total: 1395 });
    await render(<PeerLibrary />, `/peers/${FP}/library/3`);

    const more = [...host.querySelectorAll("button")].find(
      (b) => b.textContent === "Show more",
    );
    expect(more, "no Show more button").toBeTruthy();

    await act(async () => {
      more!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    for (let i = 0; i < 15; i++) {
      await act(async () => {
        await new Promise((r) => setTimeout(r, 0));
      });
    }

    // The stub holds 200, so the second page is empty and the grid is unchanged
    // — what is asserted is that asking did not *replace* the first page, which
    // is the way an accumulating list is usually got wrong.
    expect(host.querySelectorAll("button.poster-tile").length).toBeGreaterThanOrEqual(200);
  });

  // Nothing to ask for when the page is the whole library.
  it("offers nothing more when everything is already here", async () => {
    mockServer({ items: many.slice(0, 5), total: 5 });
    await render(<PeerLibrary />, `/peers/${FP}/library/3`);

    expect(host.textContent).not.toContain("Show more");
    expect(host.textContent).not.toContain(" of ");
  });
});

/*
 * Seeking a converted film (reported: "plays on initial load but not after
 * seeking", on a 4K MKV, while MP4s were fine).
 *
 * The two paths are genuinely different and the difference is the bug. Direct
 * play is the file's own bytes over a range server and the element seeks them
 * itself. A converted stream has no bytes until the far server makes them, so
 * moving the scrubber is **a new session starting somewhere else** — and every
 * such session begins at zero, which is why the screen has to keep the offset.
 */
describe("seeking a peer's film", () => {
  async function playing(method: string) {
    mockServer({ method, durationMs: 7_200_000 });
    await render(<PeerLibrary />, `/peers/${FP}/item/42`);
    const el = host.querySelector("video")!;
    await act(async () => {
      el.dispatchEvent(new Event("playing"));
    });
    const bar = host.querySelector<HTMLInputElement>("input[type='range']")!;
    return { el, bar };
  }

  // The scale comes from whoever probed the file. Without it the bar has no
  // maximum and cannot be dragged anywhere.
  it("scales the bar by the length the far server reported", async () => {
    const { bar } = await playing("transcode");
    expect(bar.disabled).toBe(false);
    expect(bar.max).toBe("7200");
  });

  /*
   * The load-bearing one. A converted seek must re-request at an offset; if it
   * merely set currentTime the element would look for bytes that do not exist,
   * which is the reported failure.
   */
  it("asks the far server to start again somewhere else", async () => {
    const { el, bar } = await playing("transcode");

    await act(async () => {
      drag(bar, 2400);
    });

    const after = el.getAttribute("src")!;
    expect(after).toContain("t=2400");
    // jsdom reports no HLS support, so this is the progressive form. Which
    // route is taken is filePath's decision and is tested there; what matters
    // here is that the offset reached the far server at all.
    expect(after).toContain(`/api/peers/${FP}/transcode?item=42`);
  });

  /*
   * And the clock keeps telling the truth afterwards.
   *
   * The new session's own time is zero, so a screen reading the element would
   * say the film had jumped back to the start — which is exactly what native
   * controls did, and why this was reported as seeking being broken.
   */
  it("goes on showing where the film is, not where the session is", async () => {
    const { bar } = await playing("transcode");

    await act(async () => {
      drag(bar, 2400);
    });

    // 2400s = 40:00. The element's own clock is 0.
    expect(host.textContent).toContain("40:00");
    expect(host.textContent).toContain("2:00:00");
  });

  // Direct play is left alone: the element does this better than we would.
  it("leaves a direct file's seeking to the element", async () => {
    const { el, bar } = await playing("direct");
    const before = el.getAttribute("src");

    await act(async () => {
      drag(bar, 600);
    });

    // The claim is that nothing was re-requested. jsdom implements no media
    // clock, so what the element did with currentTime is not observable here —
    // and it is the browser's job anyway, which is the whole point.
    expect(el.getAttribute("src")).toBe(before);
    expect(before).not.toContain("t=");
  });
});

/*
 * A withdrawn share and a switched-off machine are different sentences.
 *
 * Found when a host unshared a library while somebody was looking at it: the
 * screen eventually caught up and then told them the server was not answering.
 * It was answering — it had said no. Somebody reading that goes and asks
 * whether a computer is switched on, and the answer is yes.
 *
 * It is the rule the People card had to learn twice, in its third costume:
 * never render a choice as an absence, and never render an absence as a choice.
 */
describe("a peer library that is no longer shared", () => {
  it("says it was a decision, not a machine being off", async () => {
    mockServer({ itemsFail: { status: 404, code: "peer_refused" } });
    await render(<PeerLibrary />, `/peers/${FP}/library/3`);

    expect(host.textContent).toContain("not shared with you any more");
    expect(host.textContent).not.toContain("not answering");
  });

  it("still says not answering when that is what happened", async () => {
    mockServer({ itemsFail: { status: 502, code: "peer_unreachable" } });
    await render(<PeerLibrary />, `/peers/${FP}/library/3`);

    expect(host.textContent).toContain("not answering");
    expect(host.textContent).not.toContain("not shared with you any more");
  });
});

/*
 * A peer's answer goes stale on its own.
 *
 * This is the project's most-repeated bug in the one shape where its usual fix
 * is unavailable: **a write that changes what a list holds must invalidate that
 * list**, and across a pairing there is nothing to invalidate with. The host
 * revokes a share on their machine; this one is never told.
 *
 * Reported exactly that way — a library was unshared while somebody was looking
 * at it and their sidebar kept it. The rail mounts once when the app starts and
 * never again, so a stale time alone changes nothing: without an interval the
 * query is fetched once per launch and then never.
 */
describe("what a peer said, a minute ago", () => {
  it("asks again without being told to", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      mockServer({ items: [{ id: 1, title: "A Film" }] });
      await render(<PeerLibrary />, `/peers/${FP}/library/3`);

      const asks = () =>
        (fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls.filter(
          ([url]) => String(url).includes("/libraries"),
        ).length;
      const before = asks();
      expect(before).toBeGreaterThan(0);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(61_000);
      });

      expect(asks()).toBeGreaterThan(before);
    } finally {
      vi.useRealTimers();
    }
  });
});

/*
 * A peer's library had no pictures.
 *
 * `artworkURL` builds `/api/artwork/{hash}` — **this** server's route — and a
 * hash on a peer's item names bytes on *their* disk. So every tile asked the
 * wrong machine and got nothing, and a friend's library rendered as lettered
 * placeholders. It was visible in a screenshot for two releases before anybody
 * said the word artwork.
 *
 * Content addressing made it harmless rather than wrong: identical bytes give
 * an identical hash, so a coincidental hit would have been the right image. Two
 * households fetch their own artwork, so it was a miss every time.
 */
describe("a peer library's artwork", () => {
  it("asks their server, never ours", async () => {
    mockServer({
      items: [{ id: 42, title: "Their Film", artwork: { poster: "abc123" } }],
    });
    await render(<PeerLibrary />, `/peers/${FP}/library/3`);

    // The image is a child of .poster-tile__art, not that element itself.
    const img = host.querySelector<HTMLImageElement>(".poster-tile__art img");
    expect(img, "no artwork image rendered").toBeTruthy();

    const src = img!.getAttribute("src") ?? "";
    expect(src).toContain(`/api/peers/${FP}/artwork/42/abc123`);
    // The local route holds a different household's image at that hash, or
    // nothing at all. Either way it is the wrong machine to ask.
    expect(src).not.toContain("/api/artwork/abc123");
  });

  // No hash, no request. A tile falls back to its lettered placeholder rather
  // than asking for an image that cannot exist.
  it("asks for nothing when there is no image", async () => {
    mockServer({ items: [{ id: 42, title: "Their Film" }] });
    await render(<PeerLibrary />, `/peers/${FP}/library/3`);

    const asked = (fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls
      .filter(([url]) => String(url).includes("/artwork/"));
    expect(asked).toHaveLength(0);
  });
});
