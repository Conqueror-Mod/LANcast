/*
 * Which button leads a detail page.
 *
 * Reported: a show nobody has started said "Continue", and a film half watched
 * said "Play". The button is a promise about what happens next: Continue
 * promises a place to carry on from, Play promises the start. So:
 *
 *   - a show: Play when untouched, Continue watching (and Play from start) when
 *     part watched, Watch again when finished;
 *   - a season: Continue only when part watched, Play all otherwise;
 *   - a film: Continue watching (and Play from start) when the player would
 *     resume, Play otherwise.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route, useLocation, useParams } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { PlaybackProvider } from "@/playback/PlaybackProvider";
import { Detail } from "./Detail";
import { entrySeconds } from "@/playback/resumePoint";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const show = { id: 900, title: "A Show", kind: "show", library_id: 1, child_count: 2, artwork: {} };
const season = { id: 950, title: "Season 1", kind: "season", library_id: 1, child_count: 2, artwork: {} };
const ep = (id: number, n: number) => ({
  id, title: `Episode ${n}`, kind: "episode", library_id: 1, season: 1, episode: n,
  duration_ms: 1_300_000, artwork: {},
});
const film = (progress?: { position_ms: number; watched: boolean }) => ({
  id: 700, title: "A Film", kind: "movie", library_id: 1, duration_ms: 6_000_000,
  container: "matroska", artwork: {}, progress,
});

let host: HTMLDivElement;
let root: Root;
let landed: { path: string; state: unknown } | null;
let seasonKids = [ep(101, 1), ep(102, 2)];

function stub(item: Record<string, unknown>, standing?: Record<string, unknown>) {
  landed = null;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.includes("/continue")) return json(standing ?? {});
      if (url.includes("/episodes")) return json({ episodes: [ep(101, 1), ep(102, 2)] });
      if (url.includes("parent_id=")) {
        const kids = item.kind === "season" ? seasonKids : [season];
        return json({ items: kids, total: kids.length });
      }
      if (/\/api\/items\/\d+$/.test(url.split("?")[0])) return json(item);
      if (url.includes("/api/auth/status")) {
        return json({ authenticated: true, configured: true, user: { id: "u1", role: "admin" } });
      }
      return json({ items: [], total: 0 });
    }),
  );
}

function FakePlayer() {
  const loc = useLocation();
  const params = useParams();
  landed = { path: params.id as string, state: loc.state };
  return <div>player</div>;
}

async function settle() {
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

async function render(id: number) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <PlaybackProvider>
            <MemoryRouter initialEntries={[`/item/${id}`]}>
              <Routes>
                <Route path="/item/:id" element={<Detail />} />
                <Route path="/watch/:id" element={<FakePlayer />} />
              </Routes>
            </MemoryRouter>
          </PlaybackProvider>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

const labels = () =>
  [...host.querySelectorAll("button")].map((b) => (b.textContent ?? "").trim().replace(/^▶/, ""));
const button = (label: string) =>
  [...host.querySelectorAll("button")].find(
    (b) => (b.textContent ?? "").trim().replace(/^▶/, "") === label,
  ) as HTMLButtonElement | undefined;

beforeEach(() => {
  seasonKids = [ep(101, 1), ep(102, 2)];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("a show's leading button", () => {
  it("is Play on a show nobody has started, with no Continue anywhere", async () => {
    stub(show, { episode: ep(101, 1), resume: false, exhausted: false, started: false });
    await render(900);
    expect(labels()).toContain("Play");
    expect(labels().some((l) => l.startsWith("Continue"))).toBe(false);
    expect(labels()).not.toContain("Play from start");
  });

  it("is Continue watching, with Play from start, on a show part watched", async () => {
    stub(show, { episode: ep(102, 2), resume: false, exhausted: false, started: true });
    await render(900);
    expect(labels()).toContain("Continue watching");
    expect(labels()).toContain("Play from start");
  });

  it("is Watch again on a finished show", async () => {
    stub(show, { resume: false, exhausted: true, started: true });
    await render(900);
    expect(labels()).toContain("Watch again");
    expect(labels().some((l) => l.startsWith("Continue"))).toBe(false);
  });
});

describe("a season's buttons", () => {
  it("offers Continue only part way through", async () => {
    stub(season, { episode: ep(102, 2), resume: false, exhausted: false, started: true });
    await render(950);
    expect(labels()).toContain("Continue");
  });

  it("leads with Play all when nothing in it has been watched", async () => {
    stub(season, { episode: ep(101, 1), resume: false, exhausted: false, started: false });
    await render(950);
    expect(labels()).not.toContain("Continue");
    expect(labels()).toContain("Play all");
  });
});

describe("a season of one episode", () => {
  it("says Play rather than Play all", async () => {
    seasonKids = [ep(101, 1)];
    stub(season, { episode: ep(101, 1), resume: false, exhausted: false, started: false });
    await render(950);
    expect(labels()).toContain("Play");
    expect(labels()).not.toContain("Play all");
  });
});

describe("a film's leading button", () => {
  it("is Play when it has not been started", async () => {
    stub(film());
    await render(700);
    expect(labels()).toContain("Play");
    expect(labels()).not.toContain("Continue watching");
  });

  it("is Continue watching when it is part watched", async () => {
    stub(film({ position_ms: 1_800_000, watched: false }));
    await render(700);
    expect(labels()).toContain("Continue watching");
    expect(labels()).not.toContain("Play");
  });

  it("is Play again once finished: a watched film starts from the top", async () => {
    stub(film({ position_ms: 5_990_000, watched: true }));
    await render(700);
    expect(labels()).toContain("Play");
    expect(labels()).not.toContain("Continue watching");
  });

  it("hands the player a from-start entry when asked", async () => {
    stub(film({ position_ms: 1_800_000, watched: false }));
    await render(700);
    await act(async () => button("Play from start")!.click());
    await settle();
    expect(landed?.path).toBe("700");
    expect((landed?.state as { fromStart?: boolean })?.fromStart).toBe(true);
  });

  /*
   * And tells the server. Reported after 0.9.80: Play from start played from
   * the top, but backing out to Home showed the old position on Continue
   * Watching, because the player writes nothing for its first stretch.
   */
  it("forgets the saved place on the server before starting over", async () => {
    stub(film({ position_ms: 1_800_000, watched: false }));
    const calls = vi.mocked(fetch).mock.calls;
    await render(700);
    await act(async () => button("Play from start")!.click());
    await settle();
    const put = calls.find(
      ([u, init]) => String(u) === "/api/items/700/progress" && (init as RequestInit)?.method === "PUT",
    );
    expect(put, "Play from start never told the server").toBeDefined();
    expect(JSON.parse(String((put![1] as RequestInit).body))).toEqual({ position_ms: 0, watched: false });
    expect(landed?.path).toBe("700");
  });
});

/*
 * A collection continues film by film, the way a show does (settled
 * 2026-10-12): part way through, the next film leads, named.
 */
describe("a collection's leading button", () => {
  const collection = {
    id: 800, title: "A Franchise", kind: "collection", library_id: 1, child_count: 3, artwork: {},
  };
  const second = { ...film(), id: 702, title: "Film Two" };

  it("names the next film part way through, and plays it", async () => {
    stub(collection, { started: true, resume: false, exhausted: false, episode: second });
    await render(800);
    expect(labels()).toContain("Up next: Film Two");
    await act(async () => button("Up next: Film Two")!.click());
    await settle();
    expect(landed?.path).toBe("702");
  });

  it("says Continue for a film in progress", async () => {
    stub(collection, { started: true, resume: true, exhausted: false, episode: second });
    await render(800);
    expect(labels()).toContain("Continue: Film Two");
  });

  it("offers nothing new on a collection nobody has started", async () => {
    stub(collection, { started: false, resume: false, exhausted: false, episode: film() });
    await render(800);
    expect(labels().some((l) => l.startsWith("Up next") || l.startsWith("Continue"))).toBe(false);
  });

  it("offers nothing new once every film is watched", async () => {
    stub(collection, { started: true, resume: false, exhausted: true });
    await render(800);
    expect(labels().some((l) => l.startsWith("Up next") || l.startsWith("Continue"))).toBe(false);
  });
});

/*
 * Taking a title off Continue Watching from its own page. Reported after
 * 0.9.80: a collection's next film was put on the shelf unasked, and neither
 * its page nor the spotlight could take it off.
 */
describe("removing a title from Continue Watching on its page", () => {
  it("is offered when the title is on the shelf, and forgets the place", async () => {
    // The shelf answers with this film on it (the stub serves /continue).
    stub(film(), { items: [film()] });
    const calls = vi.mocked(fetch).mock.calls;
    await render(700);
    const remove = button("Remove from Continue Watching");
    expect(remove, "no way to take it off the shelf").toBeDefined();
    await act(async () => remove!.click());
    await settle();
    const put = calls.find(
      ([u, init]) => String(u) === "/api/items/700/progress" && (init as RequestInit)?.method === "PUT",
    );
    expect(JSON.parse(String((put![1] as RequestInit).body))).toEqual({ position_ms: 0, watched: false });
  });

  it("is not offered for a title that is not on the shelf", async () => {
    stub(film(), { items: [] });
    await render(700);
    expect(button("Remove from Continue Watching")).toBeUndefined();
  });
});

describe("where an entry starts", () => {
  const saved = { positionMs: 1_800_000, watched: false, durationMs: 6_000_000 };
  it("starts at the top when asked to, whatever was saved", () => {
    expect(entrySeconds({ fromStart: true, liveAt: 900, liveIsThisItem: true, ...saved })).toBe(0);
  });
  it("otherwise resumes the live position of the same item", () => {
    expect(entrySeconds({ fromStart: false, liveAt: 900, liveIsThisItem: true, ...saved })).toBe(900);
  });
  it("otherwise resumes the saved progress", () => {
    expect(entrySeconds({ fromStart: false, liveAt: 0, liveIsThisItem: false, ...saved })).toBe(1800);
  });
});
