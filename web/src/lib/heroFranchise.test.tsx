/*
 * The spotlight's suggestion, when a franchise is under way.
 *
 * Reported: "Because you watched" suggestions were generic. They are a genre
 * match, and a genre match is the wrong answer when the next film of the
 * collection you just finished is right there. The server marks that row on
 * Continue Watching (`continues_from`), and the suggestion follows it, saying
 * which film it follows.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useHeroSpotlight } from "./useHero";
import { writeDevice } from "./device";
import { HERO_MODE_KEY } from "./heroMode";
import type { HeroPick } from "./heroMode";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let pick: HeroPick | null = null;

const art = { poster: "p", fanart: "f" };
const genreFilm = { id: 900, kind: "movie", title: "Same Genre", library_id: 1, artwork: art };
const inProgress = {
  id: 50, kind: "movie", title: "Something Else", library_id: 1, artwork: art,
  progress: { position_ms: 600_000, watched: false }, duration_ms: 6_000_000,
};

function Probe() {
  pick = useHeroSpotlight([]);
  return null;
}

function stub(shelf: unknown[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      const json = (b: unknown) =>
        new Response(JSON.stringify(b), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.startsWith("/api/continue")) return json({ items: shelf });
      if (/\/api\/items\/50(\?|$)/.test(url)) return json({ ...inProgress, genres: ["Drama"] });
      if (url.startsWith("/api/items?")) return json({ items: [genreFilm], total: 1 });
      return json({});
    }),
  );
}

beforeEach(() => {
  pick = null;
  writeDevice(HERO_MODE_KEY, "recommended");
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
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <Probe />
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

describe("a suggestion while a franchise is under way", () => {
  it("is the collection's next film, because of the one just finished", async () => {
    const next = {
      id: 702, kind: "movie", title: "Film Two", library_id: 1, artwork: art,
      continues_from: { collection_id: 800, collection_title: "A Franchise", after_id: 701, after_title: "Film One" },
    };
    stub([inProgress, next]);
    await render();
    expect(pick?.reason).toBe("recommended");
    expect(pick?.item.id).toBe(702);
    expect(pick && "seed" in pick ? pick.seed?.title : undefined).toBe("Film One");
  });

  it("falls back to a genre match when no franchise is under way", async () => {
    stub([inProgress]);
    await render();
    expect(pick?.reason).toBe("recommended");
    expect(pick?.item.id).toBe(900);
  });
});
